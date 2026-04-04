package main

import (
	"encoding/binary"
	"errors"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
)

type dcConn struct {
	conn net.Conn
	ch   chan []byte
	once sync.Once
}

func (c *dcConn) closeWriter() {
	c.once.Do(func() { close(c.ch) })
}

type dcUDPConn struct {
	conn net.Conn
	mu   sync.Mutex
}

const udpIdleTimeout = 30 * time.Second

func isClosedConnError(err error) bool {
	return errors.Is(err, net.ErrClosed)
}

func closeWriteConn(conn net.Conn) {
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.CloseWrite()
		return
	}
	_ = conn.Close()
}

type TunnelRelay struct {
	pc          *webrtc.PeerConnection
	remoteSet   bool
	pending     []webrtc.ICECandidateInit
	externalICE func(*webrtc.ICECandidate)
	externalCSC func(webrtc.PeerConnectionState)

	dc    *webrtc.DataChannel
	dcMu  sync.Mutex
	conns sync.Map
	udps  sync.Map

	sampleTrack *webrtc.TrackLocalStaticSample
	tunnel      *VP8DataTunnel
	OnConnected func(*VP8DataTunnel)

	readBufSize int
	maxDCBuf    uint64

	mode     string
	modeOnce sync.Once
}

func NewTunnelRelay() *TunnelRelay {
	return &TunnelRelay{mode: "unknown"}
}

func (u *TunnelRelay) Init(iceServers []webrtc.ICEServer) error {
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{ICEServers: iceServers})
	if err != nil {
		return err
	}
	u.pc = pc

	negotiated := true
	dcID := uint16(2)
	dc, err := pc.CreateDataChannel("tunnel", &webrtc.DataChannelInit{
		Negotiated: &negotiated,
		ID:         &dcID,
	})
	if err != nil {
		log.Printf("[relay] warning: could not create tunnel DC: %v", err)
	} else {
		u.dc = dc
		dc.OnOpen(func() {
			log.Printf("[relay] tunnel DC open (readyState=%v)", dc.ReadyState())
		})
		dc.OnClose(func() {
			log.Println("[relay] tunnel DC closed")
			if u.mode == "dc" {
				u.closeAllConns()
			}
		})
		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			u.modeOnce.Do(func() {
				u.mode = "dc"
				log.Println("[relay] === MODE: DC ===")
			})
			u.handleDCMessage(msg.Data)
		})
	}

	sampleTrack, _ := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8},
		"video", "tunnel-video",
	)
	u.sampleTrack = sampleTrack

	audioTrack, _ := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio", "tunnel-audio",
	)
	pc.AddTrack(audioTrack)
	pc.AddTrack(sampleTrack)

	ordered := true
	dcNotif, err := pc.CreateDataChannel("producerNotification", &webrtc.DataChannelInit{Ordered: &ordered})
	if err == nil {
		dcNotif.OnOpen(func() { log.Println("[relay] producerNotification DC opened") })
		dcNotif.OnMessage(func(msg webrtc.DataChannelMessage) {
			log.Printf("[relay] producerNotification msg len=%d", len(msg.Data))
		})
	}
	dcCmd, err := pc.CreateDataChannel("producerCommand", &webrtc.DataChannelInit{Ordered: &ordered})
	if err == nil {
		dcCmd.OnOpen(func() { log.Println("[relay] producerCommand DC opened") })
		dcCmd.OnMessage(func(msg webrtc.DataChannelMessage) {
			log.Printf("[relay] producerCommand msg len=%d", len(msg.Data))
		})
	}
	pc.CreateDataChannel("producerScreenShare", &webrtc.DataChannelInit{Ordered: &ordered})
	pc.CreateDataChannel("consumerScreenShare", &webrtc.DataChannelInit{Ordered: &ordered})

	pc.OnICECandidate(func(cand *webrtc.ICECandidate) {
		if cand == nil {
			return
		}
		if u.externalICE != nil {
			u.externalICE(cand)
		}
	})

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("[relay] connection state: %s (mode=%s)", state.String(), u.mode)
		if u.externalCSC != nil {
			u.externalCSC(state)
		}
	})

	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		log.Printf("[relay] remote track: %s", track.Codec().MimeType)
		u.modeOnce.Do(func() {
			u.mode = "video"
			log.Println("[relay] === MODE: VIDEO ===")
			u.tunnel = NewVP8DataTunnel(sampleTrack, log.Printf)
			u.tunnel.Start(25)
			if u.OnConnected != nil {
				u.OnConnected(u.tunnel)
			}
		})
		go u.readTrack(track)
	})

	log.Printf("[relay] PC created (%d ICE servers)", len(iceServers))
	return nil
}

func (u *TunnelRelay) CreateOffer() (webrtc.SessionDescription, error) {
	offer, err := u.pc.CreateOffer(nil)
	if err != nil {
		return offer, err
	}
	u.pc.SetLocalDescription(offer)
	return offer, nil
}

func (u *TunnelRelay) CreateAnswer() (webrtc.SessionDescription, error) {
	answer, err := u.pc.CreateAnswer(nil)
	if err != nil {
		return answer, err
	}
	u.pc.SetLocalDescription(answer)
	return answer, nil
}

func (u *TunnelRelay) SetRemoteDescription(sdpType webrtc.SDPType, sdp string) error {
	err := u.pc.SetRemoteDescription(webrtc.SessionDescription{Type: sdpType, SDP: sdp})
	if err != nil {
		return err
	}
	u.remoteSet = true
	for _, cand := range u.pending {
		u.pc.AddICECandidate(cand)
	}
	u.pending = nil
	return nil
}

func (u *TunnelRelay) AddICECandidate(candidate webrtc.ICECandidateInit) error {
	if !u.remoteSet {
		u.pending = append(u.pending, candidate)
		return nil
	}
	return u.pc.AddICECandidate(candidate)
}

func (u *TunnelRelay) OnICECandidate(fn func(*webrtc.ICECandidate)) {
	u.externalICE = fn
}

func (u *TunnelRelay) OnConnectionStateChange(fn func(webrtc.PeerConnectionState)) {
	u.externalCSC = fn
}

func (u *TunnelRelay) Close() {
	u.closeAllConns()
	if u.tunnel != nil {
		u.tunnel.Stop()
		u.tunnel = nil
	}
	u.dcMu.Lock()
	u.dc = nil
	u.dcMu.Unlock()
	if u.pc != nil {
		u.pc.OnConnectionStateChange(nil)
		u.pc.OnICECandidate(nil)
		u.pc.OnTrack(nil)
		oldPC := u.pc
		u.pc = nil
		go oldPC.Close()
	}
	u.remoteSet = false
	u.pending = nil
	u.sampleTrack = nil
}

func (u *TunnelRelay) handleDCMessage(data []byte) {
	if len(data) < 5 {
		log.Printf("[dc] short frame: %d bytes", len(data))
		return
	}
	connID := binary.BigEndian.Uint32(data[0:4])
	mt := data[4]
	payload := data[5:]

	switch mt {
	case msgConnect:
		go u.connectTCP(connID, string(payload))

	case msgUDP:
		u.handleUDP(connID, payload)

	case msgData:
		val, ok := u.conns.Load(connID)
		if !ok {
			log.Printf("[dc] DATA for unknown conn %d (%d bytes)", connID, len(payload))
			return
		}
		dc := val.(*dcConn)
		cp := make([]byte, len(payload))
		copy(cp, payload)
		select {
		case dc.ch <- cp:
		default:
			log.Printf("[dc] conn %d write queue full, dropping %d bytes", connID, len(payload))
		}

	case msgClose:
		val, ok := u.conns.LoadAndDelete(connID)
		if ok {
			dc := val.(*dcConn)
			dc.closeWriter()
		}
	}
}

func (u *TunnelRelay) sendDCFrame(connID uint32, mt byte, payload []byte) {
	u.dcMu.Lock()
	defer u.dcMu.Unlock()
	if u.dc == nil {
		return
	}
	buf := make([]byte, 5+len(payload))
	binary.BigEndian.PutUint32(buf[0:4], connID)
	buf[4] = mt
	copy(buf[5:], payload)
	if err := u.dc.Send(buf); err != nil {
		log.Printf("[dc] send frame conn=%d type=%d failed: %v", connID, mt, err)
	}
}

func safeBufferedAmount(dc *webrtc.DataChannel) (n uint64) {
	defer func() { recover() }()
	return dc.BufferedAmount()
}

func (u *TunnelRelay) connectTCP(connID uint32, addr string) {
	log.Printf("[dc] CONNECT %d -> %s", connID, maskAddr(addr))
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		log.Printf("[dc] CONNECT %d failed: %v", connID, err)
		u.sendDCFrame(connID, msgConnectErr, []byte(err.Error()))
		return
	}
	dc := &dcConn{conn: conn, ch: make(chan []byte, 256)}
	u.conns.Store(connID, dc)
	u.sendDCFrame(connID, msgConnectOK, nil)
	log.Printf("[dc] CONNECTED %d -> %s", connID, maskAddr(addr))

	go func() {
		for data := range dc.ch {
			if _, err := conn.Write(data); err != nil {
				if !isClosedConnError(err) {
					log.Printf("[dc] conn %d write error: %v", connID, err)
				}
				break
			}
		}
		closeWriteConn(conn)
	}()

	bufSz := u.readBufSize
	if bufSz <= 0 {
		bufSz = rtpBufSize
	}
	buf := make([]byte, bufSz)
	sent := 0
	for {
		if u.maxDCBuf > 0 {
			u.dcMu.Lock()
			localDC := u.dc
			u.dcMu.Unlock()
			if localDC != nil {
				for safeBufferedAmount(localDC) > u.maxDCBuf {
					time.Sleep(5 * time.Millisecond)
				}
			}
		}

		n, err := conn.Read(buf)
		if n > 0 {
			u.sendDCFrame(connID, msgData, buf[:n])
			sent += n
		}
		if err != nil {
			if err != io.EOF && !isClosedConnError(err) {
				log.Printf("[dc] conn %d read error: %v", connID, err)
			}
			break
		}
	}
	log.Printf("[dc] conn %d closed, sent %d bytes", connID, sent)
	u.sendDCFrame(connID, msgClose, nil)
	u.conns.Delete(connID)
}

func (u *TunnelRelay) handleUDP(connID uint32, payload []byte) {
	if len(payload) < 2 {
		log.Printf("[dc] UDP frame too short conn=%d len=%d", connID, len(payload))
		return
	}
	addrLen := int(payload[0])
	if addrLen == 0 || len(payload) < 1+addrLen {
		return
	}
	addr := string(payload[1 : 1+addrLen])
	data := payload[1+addrLen:]

	val, ok := u.udps.Load(connID)
	if !ok {
		udpAddr, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			log.Printf("[dc] UDP resolve failed conn=%d addr=%s: %v", connID, maskAddr(addr), err)
			return
		}
		udpConn, err := net.DialUDP("udp", nil, udpAddr)
		if err != nil {
			log.Printf("[dc] UDP dial failed conn=%d addr=%s: %v", connID, maskAddr(addr), err)
			return
		}
		sess := &dcUDPConn{conn: udpConn}
		actual, loaded := u.udps.LoadOrStore(connID, sess)
		if loaded {
			udpConn.Close()
			val = actual
		} else {
			val = sess
			go func() {
				buf := make([]byte, udpBufSize)
				for {
					_ = udpConn.SetReadDeadline(time.Now().Add(udpIdleTimeout))
					n, err := udpConn.Read(buf)
					if n > 0 {
						resp := make([]byte, n)
						copy(resp, buf[:n])
						u.sendDCFrame(connID, msgUDPReply, resp)
					}
					if err != nil {
						if ne, ok := err.(net.Error); ok && ne.Timeout() {
							log.Printf("[dc] UDP conn %d idle timeout", connID)
						} else if !isClosedConnError(err) {
							log.Printf("[dc] UDP read failed conn=%d: %v", connID, err)
						}
						break
					}
				}
				u.udps.Delete(connID)
				_ = udpConn.Close()
			}()
		}
	}
	sess := val.(*dcUDPConn)
	sess.mu.Lock()
	_, err := sess.conn.Write(data)
	sess.mu.Unlock()
	if err != nil {
		if !isClosedConnError(err) {
			log.Printf("[dc] UDP write failed conn=%d: %v", connID, err)
		}
		u.udps.Delete(connID)
		_ = sess.conn.Close()
	}
}

func (u *TunnelRelay) closeAllConns() {
	u.conns.Range(func(key, val any) bool {
		dc := val.(*dcConn)
		dc.closeWriter()
		dc.conn.Close()
		u.conns.Delete(key)
		return true
	})
	u.udps.Range(func(key, val any) bool {
		_ = val.(*dcUDPConn).conn.Close()
		u.udps.Delete(key)
		return true
	})
}

func (u *TunnelRelay) readTrack(track *webrtc.TrackRemote) {
	if track.Codec().MimeType != webrtc.MimeTypeVP8 {
		buf := make([]byte, udpBufSize)
		for {
			if _, _, err := track.Read(buf); err != nil {
				return
			}
		}
	}

	var vp8Pkt codecs.VP8Packet
	var frameBuf []byte
	var dataCount, recvCount int
	buf := make([]byte, rtpBufSize)
	for {
		n, _, err := track.Read(buf)
		if err != nil {
			return
		}
		pkt := &rtp.Packet{}
		if pkt.Unmarshal(buf[:n]) != nil {
			continue
		}
		vp8Payload, err := vp8Pkt.Unmarshal(pkt.Payload)
		if err != nil {
			continue
		}
		if vp8Pkt.S == 1 {
			frameBuf = frameBuf[:0]
		}
		frameBuf = append(frameBuf, vp8Payload...)
		if pkt.Marker {
			recvCount++
			if recvCount <= 3 || recvCount%25 == 0 {
				if len(frameBuf) > 0 {
					log.Printf("[video] recv frame #%d %d bytes, first=0x%02x", recvCount, len(frameBuf), frameBuf[0])
				}
			}
			data := ExtractDataFromPayload(frameBuf)
			if data != nil {
				dataCount++
				if dataCount <= 5 || dataCount%100 == 0 {
					log.Printf("[video] TUNNEL DATA #%d: %d bytes", dataCount, len(data))
				}
				if u.tunnel != nil && u.tunnel.onData != nil {
					u.tunnel.onData(data)
				}
			}
		}
	}
}
