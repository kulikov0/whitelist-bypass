package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
	"headless-creator/tunnel"
)

type SFURelay struct {
	pubPC        *webrtc.PeerConnection
	subPC        *webrtc.PeerConnection
	pubRemoteSet bool
	subRemoteSet bool
	pubPending   []webrtc.ICECandidateInit
	subPending   []webrtc.ICECandidateInit
	mu           sync.Mutex

	sampleTrack  *webrtc.TrackLocalStaticSample
	tun          *tunnel.VP8DataTunnel
	dc           *webrtc.DataChannel
	inDC         *webrtc.DataChannel
	OnConnected  func(*tunnel.VP8DataTunnel)
	OnPubICE     func(*webrtc.ICECandidate)
	OnSubICE     func(*webrtc.ICECandidate)

	readBufSize int
	maxDCBuf    uint64

	// DC relay
	conns    sync.Map
	dcMu     sync.Mutex
	sendMsgId uint32
	recvBufs  sync.Map
}

func NewSFURelay() *SFURelay {
	return &SFURelay{}
}

func (r *SFURelay) Init(iceServers []webrtc.ICEServer) error {
	config := webrtc.Configuration{ICEServers: iceServers}

	pubPC, err := webrtc.NewPeerConnection(config)
	if err != nil {
		return err
	}
	r.pubPC = pubPC

	sampleTrack, _ := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8},
		"video", "tunnel-video",
	)
	r.sampleTrack = sampleTrack

	audioTrack, _ := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio", "tunnel-audio",
	)
	pubPC.AddTransceiverFromTrack(audioTrack, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly})
	pubPC.AddTransceiverFromTrack(sampleTrack, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly})
	r.createSharingDC()

	pubPC.OnICECandidate(func(cand *webrtc.ICECandidate) {
		if cand == nil || r.OnPubICE == nil {
			return
		}
		r.OnPubICE(cand)
	})

	pubPC.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("[pub] connection state: %s", state.String())
	})

	subPC, err := webrtc.NewPeerConnection(config)
	if err != nil {
		pubPC.Close()
		return err
	}
	r.subPC = subPC

	subPC.OnICECandidate(func(cand *webrtc.ICECandidate) {
		if cand == nil || r.OnSubICE == nil {
			return
		}
		r.OnSubICE(cand)
	})

	subPC.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("[sub] connection state: %s", state.String())
	})

	subPC.OnDataChannel(func(inDC *webrtc.DataChannel) {
		log.Printf("[sub] Incoming DC: label=%s id=%d state=%s", inDC.Label(), inDC.ID(), inDC.ReadyState().String())
		if inDC.Label() == "sharing" {
			log.Println("[sub] Joiner sharing DC found")
			r.inDC = inDC
			inDC.OnMessage(func(msg webrtc.DataChannelMessage) {
				if msg.IsString {
					s := string(msg.Data)
					if s == "tunnel:ping" {
						if r.dc != nil && r.dc.ReadyState() == webrtc.DataChannelStateOpen {
							r.dc.SendText("tunnel:pong")
							log.Println("[relay] === MODE: DC ===")
							fmt.Println("\n  TUNNEL CONNECTED\n")
						} else {
							log.Println("[sub] tunnel:ping received but pub DC not ready")
						}
						return
					}
				}
				if !msg.IsString && len(msg.Data) >= 6 {
					r.handleChunk(msg.Data)
				}
			})
			inDC.OnOpen(func() {
				log.Println("[sub] Joiner sharing DC open")
			})
		}
	})

	subPC.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		log.Printf("[sub] remote track: %s", track.Codec().MimeType)
		go r.readTrack(track)
	})

	log.Printf("[relay] pub+sub PCs created (%d ICE servers)", len(iceServers))
	return nil
}

func (r *SFURelay) CreatePubOffer() (webrtc.SessionDescription, error) {
	offer, err := r.pubPC.CreateOffer(nil)
	if err != nil {
		return offer, err
	}
	r.pubPC.SetLocalDescription(offer)
	return offer, nil
}

func (r *SFURelay) SetPubAnswer(sdp string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	err := r.pubPC.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer, SDP: sdp,
	})
	if err != nil {
		return err
	}
	r.pubRemoteSet = true
	for _, cand := range r.pubPending {
		r.pubPC.AddICECandidate(cand)
	}
	r.pubPending = nil
	return nil
}

func (r *SFURelay) SetSubOffer(sdp string) (webrtc.SessionDescription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	err := r.subPC.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer, SDP: sdp,
	})
	if err != nil {
		return webrtc.SessionDescription{}, err
	}
	r.subRemoteSet = true
	for _, cand := range r.subPending {
		r.subPC.AddICECandidate(cand)
	}
	r.subPending = nil

	answer, err := r.subPC.CreateAnswer(nil)
	if err != nil {
		return answer, err
	}
	r.subPC.SetLocalDescription(answer)
	return answer, nil
}

func (r *SFURelay) AddPubICECandidate(cand webrtc.ICECandidateInit) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.pubRemoteSet {
		r.pubPending = append(r.pubPending, cand)
		return
	}
	r.pubPC.AddICECandidate(cand)
}

func (r *SFURelay) AddSubICECandidate(cand webrtc.ICECandidateInit) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.subRemoteSet {
		r.subPending = append(r.subPending, cand)
		return
	}
	r.subPC.AddICECandidate(cand)
}

func (r *SFURelay) createSharingDC() {
	log.Println("[pub] Creating sharing DataChannel")
	ordered := true
	dc, err := r.pubPC.CreateDataChannel("sharing", &webrtc.DataChannelInit{Ordered: &ordered})
	if err != nil {
		log.Printf("[pub] Failed to create sharing DC: %v", err)
		return
	}
	r.dc = dc
	dc.OnOpen(func() {
		log.Println("[pub] Sharing DC open")
	})
	dc.OnClose(func() {
		log.Println("[pub] Sharing DC closed")
	})
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		if msg.IsString {
			s := string(msg.Data)
			if s == "tunnel:ping" {
				dc.SendText("tunnel:pong")
				return
			}
			if s == "tunnel:pong" {
				log.Println("[pub] Received tunnel:pong on outbound DC")
				return
			}
		}
	})
}

func (r *SFURelay) CreatePubRenegotiate() (webrtc.SessionDescription, error) {
	offer, err := r.pubPC.CreateOffer(&webrtc.OfferOptions{ICERestart: false})
	if err != nil {
		return offer, err
	}
	err = r.pubPC.SetLocalDescription(offer)
	if err != nil {
		return offer, err
	}
	r.mu.Lock()
	r.pubRemoteSet = false
	r.pubPending = nil
	r.mu.Unlock()
	return offer, nil
}

const chunkSize = 994

type chunkBuf struct {
	chunks [][]byte
	count  int
	size   int
}

func (r *SFURelay) handleChunk(data []byte) {
	id := uint16(data[0])<<8 | uint16(data[1])
	idx := int(uint16(data[2])<<8 | uint16(data[3]))
	total := int(uint16(data[4])<<8 | uint16(data[5]))
	payload := data[6:]

	if total == 1 {
		cp := make([]byte, len(payload))
		copy(cp, payload)
		r.handleDCMessage(cp)
		return
	}

	key := id
	val, _ := r.recvBufs.LoadOrStore(key, &chunkBuf{chunks: make([][]byte, total)})
	buf := val.(*chunkBuf)
	if idx < len(buf.chunks) && buf.chunks[idx] == nil {
		cp := make([]byte, len(payload))
		copy(cp, payload)
		buf.chunks[idx] = cp
		buf.count++
		buf.size += len(cp)
	}
	if buf.count == total {
		r.recvBufs.Delete(key)
		out := make([]byte, 0, buf.size)
		for _, c := range buf.chunks {
			out = append(out, c...)
		}
		r.handleDCMessage(out)
	}
}

func (r *SFURelay) sendChunked(data []byte) {
	r.dcMu.Lock()
	dc := r.dc
	r.dcMu.Unlock()
	if dc == nil || dc.ReadyState() != webrtc.DataChannelStateOpen {
		return
	}
	if r.maxDCBuf > 0 {
		for dc.BufferedAmount() > r.maxDCBuf {
			time.Sleep(5 * time.Millisecond)
		}
	}
	total := int(math.Ceil(float64(len(data)) / float64(chunkSize)))
	if total == 0 {
		total = 1
	}
	id := uint16(atomic.AddUint32(&r.sendMsgId, 1)) & 0xFFFF
	for i := 0; i < total; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > len(data) {
			end = len(data)
		}
		p := data[start:end]
		f := make([]byte, 6+len(p))
		f[0] = byte(id >> 8)
		f[1] = byte(id & 0xFF)
		f[2] = byte(i >> 8)
		f[3] = byte(i & 0xFF)
		f[4] = byte(total >> 8)
		f[5] = byte(total & 0xFF)
		copy(f[6:], p)
		dc.Send(f)
	}
}

func (r *SFURelay) handleDCMessage(data []byte) {
	if len(data) < 5 {
		return
	}
	connID := binary.BigEndian.Uint32(data[0:4])
	mt := data[4]
	payload := data[5:]

	switch mt {
	case tunnel.MsgConnect:
		go r.connectTCP(connID, string(payload))
	case tunnel.MsgUDP:
		go r.handleUDP(connID, payload)
	case tunnel.MsgData:
		val, ok := r.conns.Load(connID)
		if ok {
			val.(net.Conn).Write(payload)
		}
	case tunnel.MsgClose:
		val, ok := r.conns.LoadAndDelete(connID)
		if ok {
			val.(net.Conn).Close()
		}
	}
}

func (r *SFURelay) sendDCFrame(connID uint32, mt byte, payload []byte) {
	buf := make([]byte, 5+len(payload))
	binary.BigEndian.PutUint32(buf[0:4], connID)
	buf[4] = mt
	copy(buf[5:], payload)
	r.sendChunked(buf)
}

func (r *SFURelay) connectTCP(connID uint32, addr string) {
	log.Printf("[dc] CONNECT %d -> %s", connID, tunnel.MaskAddr(addr))
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		log.Printf("[dc] CONNECT %d failed: %v", connID, err)
		r.sendDCFrame(connID, tunnel.MsgConnectErr, []byte(err.Error()))
		return
	}
	r.conns.Store(connID, conn)
	r.sendDCFrame(connID, tunnel.MsgConnectOK, nil)
	log.Printf("[dc] CONNECTED %d -> %s", connID, tunnel.MaskAddr(addr))

	buf := make([]byte, chunkSize-5)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			r.sendDCFrame(connID, tunnel.MsgData, buf[:n])
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("[dc] conn %d read error: %v", connID, err)
			}
			break
		}
	}
	r.sendDCFrame(connID, tunnel.MsgClose, nil)
	r.conns.Delete(connID)
}

func (r *SFURelay) handleUDP(connID uint32, payload []byte) {
	if len(payload) < 2 {
		return
	}
	addrLen := int(payload[0])
	if addrLen == 0 || len(payload) < 1+addrLen {
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
	resp := make([]byte, tunnel.UDPBufSize)
	n, err := conn.Read(resp)
	if err != nil {
		return
	}
	r.sendDCFrame(connID, tunnel.MsgUDPReply, resp[:n])
}

func (r *SFURelay) closeAllConns() {
	r.conns.Range(func(key, val any) bool {
		if c, ok := val.(net.Conn); ok {
			c.Close()
		}
		r.conns.Delete(key)
		return true
	})
}

func (r *SFURelay) Close() {
	r.closeAllConns()
	if r.tun != nil {
		r.tun.Stop()
		r.tun = nil
	}
	if r.pubPC != nil {
		r.pubPC.Close()
		r.pubPC = nil
	}
	if r.subPC != nil {
		r.subPC.Close()
		r.subPC = nil
	}
}

func (r *SFURelay) readTrack(track *webrtc.TrackRemote) {
	if track.Codec().MimeType != webrtc.MimeTypeVP8 {
		buf := make([]byte, tunnel.UDPBufSize)
		for {
			if _, _, err := track.Read(buf); err != nil {
				return
			}
		}
	}

	var vp8Pkt codecs.VP8Packet
	var frameBuf []byte
	var dataCount, recvCount int
	bufSz := r.readBufSize
	if bufSz <= 0 {
		bufSz = tunnel.RTPBufSize
	}
	buf := make([]byte, bufSz)
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
			data := tunnel.ExtractDataFromPayload(frameBuf)
			if data != nil {
				if r.tun == nil {
					log.Println("[relay] === MODE: VIDEO ===")
					r.tun = tunnel.NewVP8DataTunnel(r.sampleTrack, log.Printf)
					r.tun.Start(25)
					if r.OnConnected != nil {
						r.OnConnected(r.tun)
					}
				}
				dataCount++
				if dataCount <= 5 || dataCount%100 == 0 {
					log.Printf("[video] TUNNEL DATA #%d: %d bytes", dataCount, len(data))
				}
				if r.tun.OnData != nil {
					r.tun.OnData(data)
				}
			}
		}
	}
}
