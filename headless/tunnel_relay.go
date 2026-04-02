package main

import (
	"encoding/binary"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
)

const (
	dcBufSize = 16384
	dcMaxBufs = 8
)

var dcBufPool = func() chan []byte {
	ch := make(chan []byte, dcMaxBufs)
	for i := 0; i < dcMaxBufs; i++ {
		ch <- make([]byte, 5+dcBufSize)
	}
	return ch
}()

type TunnelRelay struct {
	pc          *webrtc.PeerConnection
	remoteSet   bool
	pending     []webrtc.ICECandidateInit
	externalICE func(*webrtc.ICECandidate)
	externalCSC func(webrtc.PeerConnectionState)

	dc    *webrtc.DataChannel
	dcMu  sync.Mutex
	conns sync.Map

	sampleTrack *webrtc.TrackLocalStaticSample
	tunnel      *VP8DataTunnel
	OnConnected func(*VP8DataTunnel)

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
			log.Println("[relay] tunnel DC open")
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
		log.Printf("[relay] connection state: %s", state.String())
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
		return
	}
	connID := binary.BigEndian.Uint32(data[0:4])
	mt := data[4]
	payload := data[5:]

	switch mt {
	case msgConnect:
		go u.connectTCP(connID, string(payload))
	case msgUDP:
		go u.handleUDP(connID, payload)
	case msgData:
		val, ok := u.conns.Load(connID)
		if ok {
			val.(net.Conn).Write(payload)
		}
	case msgClose:
		val, ok := u.conns.LoadAndDelete(connID)
		if ok {
			val.(net.Conn).Close()
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
	u.dc.Send(buf)
}

func (u *TunnelRelay) connectTCP(connID uint32, addr string) {
	log.Printf("[dc] CONNECT %d -> %s", connID, maskAddr(addr))
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		log.Printf("[dc] CONNECT %d failed: %v", connID, err)
		u.sendDCFrame(connID, msgConnectErr, []byte(err.Error()))
		return
	}
	u.conns.Store(connID, conn)
	u.sendDCFrame(connID, msgConnectOK, nil)
	log.Printf("[dc] CONNECTED %d -> %s", connID, maskAddr(addr))

	buf := <-dcBufPool
	defer func() { dcBufPool <- buf }()

	readBuf := buf[5:]
	for {
		n, err := conn.Read(readBuf)
		if n > 0 {
			binary.BigEndian.PutUint32(buf[0:4], connID)
			buf[4] = msgData
			u.dcMu.Lock()
			if u.dc != nil {
				u.dc.Send(buf[:5+n])
			}
			u.dcMu.Unlock()
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("[dc] conn %d read error: %v", connID, err)
			}
			break
		}
	}
	u.sendDCFrame(connID, msgClose, nil)
	u.conns.Delete(connID)
}

func (u *TunnelRelay) handleUDP(connID uint32, payload []byte) {
	if len(payload) < 2 {
		return
	}
	addrLen := int(payload[0])
	if len(payload) < 1+addrLen {
		return
	}
	addr := string(payload[1 : 1+addrLen])
	data := payload[1+addrLen:]
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return
	}
	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	conn.Write(data)
	resp := make([]byte, udpBufSize)
	n, err := conn.Read(resp)
	if err != nil {
		return
	}
	u.sendDCFrame(connID, msgUDPReply, resp[:n])
}

func (u *TunnelRelay) closeAllConns() {
	u.conns.Range(func(key, val any) bool {
		val.(net.Conn).Close()
		u.conns.Delete(key)
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
