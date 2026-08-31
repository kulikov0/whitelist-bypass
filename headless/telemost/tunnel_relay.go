package main

import (
	"fmt"
	"log"
	"sync"

	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/kulikov0/headless-client/webrtc"
	"whitelist-bypass/relay/common"
	tmapi "whitelist-bypass/relay/telemost"
	"whitelist-bypass/relay/tunnel"
)

const (
	sharingRTPMTU     = 1200
	sharingVP8PT      = 96
	sharingClockRate  = 90000
	sharingStreamSSRC = 0x1a2b3c4d
	defaultSharingFPS = 24
)

type SFURelay struct {
	pubPC        *webrtc.PeerConnection
	subPC        *webrtc.PeerConnection
	pubRemoteSet bool
	subRemoteSet bool
	pubPending   []webrtc.ICECandidateInit
	subPending   []webrtc.ICECandidateInit
	mu           sync.Mutex

	sampleTrack   *webrtc.TrackLocalStaticSample
	tun           *tunnel.VP8DataTunnel
	mt            *tunnel.MultiTrackTunnel
	obf           *tunnel.TunnelObfuscator
	OnConnected   func(tunnel.DataTunnel)
	OnPubReady    func()
	OnPeerRestart func()
	OnPubICE      func(*webrtc.ICECandidate)
	OnSubICE      func(*webrtc.ICECandidate)

	sharingDC *webrtc.DataChannel

	readBufSize int
	tunFired    bool
}

type sharingDCSink struct {
	dc         *webrtc.DataChannel
	mu         sync.Mutex
	packetizer rtp.Packetizer
	samples    uint32
}

func (r *SFURelay) SetObfuscator(o *tunnel.TunnelObfuscator) { r.obf = o }

func NewSFURelay() *SFURelay {
	return &SFURelay{}
}

func newSharingDCSink(dc *webrtc.DataChannel, fps int) *sharingDCSink {
	if fps <= 0 {
		fps = defaultSharingFPS
	}
	return &sharingDCSink{
		dc: dc,
		packetizer: rtp.NewPacketizer(sharingRTPMTU, sharingVP8PT, sharingStreamSSRC,
			&codecs.VP8Payloader{EnablePictureID: true}, rtp.NewRandomSequencer(), sharingClockRate),
		samples: uint32(sharingClockRate / fps),
	}
}

func (s *sharingDCSink) sendFrame(frame []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, pkt := range s.packetizer.Packetize(frame, s.samples) {
		raw, err := pkt.Marshal()
		if err != nil {
			continue
		}
		if err := s.dc.Send(raw); err != nil {
			return err
		}
	}
	return nil
}

func (r *SFURelay) AddSharingDataChannel() error {
	if r.pubPC == nil {
		return fmt.Errorf("pub PC nil")
	}
	unordered := false
	dc, err := r.pubPC.CreateDataChannel("sharing", &webrtc.DataChannelInit{Ordered: &unordered})
	if err != nil {
		return err
	}
	r.sharingDC = dc
	sink := newSharingDCSink(dc, defaultSharingFPS)
	keyframe := func() []byte {
		return r.obf.EncodeKeepalive(0)
	}
	dc.OnOpen(func() {
		log.Printf("[ss] creator 'sharing' DC open, registering screenshare sub-tunnel")
		sink.sendFrame(keyframe())
		sub := tunnel.NewVP8DataTunnel(nil, r.obf, log.Printf)
		sub.WriteFrame = sink.sendFrame
		if r.mt == nil {
			return
		}
		r.mt.AddSubTunnel(sub)
		log.Printf("[ss] screenshare sub-tunnel live, tracks=%d", r.mt.SubTunnelCount())
	})
	dc.OnClose(func() { log.Printf("[ss] creator 'sharing' DC closed") })
	dc.OnError(func(e error) { log.Printf("[ss] creator 'sharing' DC error: %v", e) })
	dc.OnMessage(func(m webrtc.DataChannelMessage) {
		if len(m.Data) >= 2 && m.Data[0]&0xC0 == 0x80 && m.Data[1] >= 200 && m.Data[1] <= 206 {
			if m.Data[1] == 206 {
				sink.sendFrame(keyframe())
			}
		}
	})
	return nil
}

func (r *SFURelay) RemoveSharingDataChannel() {
	if r.mt != nil {
		r.mt.RemoveLastSubTunnel()
	}
	if r.sharingDC != nil {
		r.sharingDC.Close()
		r.sharingDC = nil
	}
}

func (r *SFURelay) Init(iceServers []webrtc.ICEServer) error {
	config := webrtc.Configuration{ICEServers: iceServers}

	pubPC, err := tmapi.NewPeerConnection(config)
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
	videoTr, _ := pubPC.AddTransceiverFromTrack(sampleTrack, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly})
	if videoTr != nil {
		go tunnel.DrainSenderRTCP(videoTr.Sender())
	}

	pubPC.OnICECandidate(func(cand *webrtc.ICECandidate) {
		if cand == nil || r.OnPubICE == nil {
			return
		}
		r.OnPubICE(cand)
	})

	pubPC.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("[pub] connection state: %s", state.String())
		if state == webrtc.PeerConnectionStateConnected {
			if r.tun == nil {
				log.Println("[relay] starting VP8 publish tunnel on pub PC connected")
				r.tun = tunnel.NewVP8DataTunnel(r.sampleTrack, r.obf, log.Printf)
				r.tun.Start(0, 0)
				r.tunFired = false
				r.mt = tunnel.NewMultiTrackTunnel([]*tunnel.VP8DataTunnel{r.tun})
				r.mt.SetOnData(func(payload []byte) { r.activate(r.mt, payload) })
			}
			if r.OnPubReady != nil {
				r.OnPubReady()
			}
		}
	})

	subPC, err := tmapi.NewPeerConnection(config)
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

	subPC.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		log.Printf("[sub] remote track: %s", track.Codec().MimeType)
		go r.readTrack(track)
	})

	log.Printf("[relay] pub+sub PCs created (%d ICE servers)", len(iceServers))
	return nil
}

func (r *SFURelay) activate(mt *tunnel.MultiTrackTunnel, payload []byte) {
	r.mu.Lock()
	if r.tunFired {
		r.mu.Unlock()
		return
	}
	r.tunFired = true
	r.mu.Unlock()

	var delivered tunnel.DataTunnel = mt
	useKCP := false
	if !tunnel.LooksLikeRelayFrame(payload) {
		delivered = tunnel.NewMultiTrackKCPTunnel(mt, log.Printf)
		useKCP = true
		log.Println("[relay] per-track kcp reliability active over video tunnel")
	}
	log.Printf("[relay] auto-detected active tunnel: %T", delivered)
	if r.OnConnected != nil {
		r.OnConnected(delivered)
	}
	if useKCP {
		if kcptun, ok := delivered.(*tunnel.MultiTrackKCPTunnel); ok {
			kcptun.InjectSegment(payload)
		}
	} else {
		mt.DeliverData(payload)
	}
}

func (r *SFURelay) CreatePubOffer() (webrtc.SessionDescription, error) {
	offer, err := r.pubPC.CreateOffer(nil)
	if err != nil {
		return offer, err
	}
	if err := r.pubPC.SetLocalDescription(offer); err != nil {
		return offer, err
	}
	offer.SDP = tmapi.MungeSDPAddVideoContent(offer.SDP)
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

func (r *SFURelay) CreatePubRenegotiate() (webrtc.SessionDescription, error) {
	offer, err := r.pubPC.CreateOffer(&webrtc.OfferOptions{ICERestart: false})
	if err != nil {
		return offer, err
	}
	err = r.pubPC.SetLocalDescription(offer)
	if err != nil {
		return offer, err
	}
	offer.SDP = tmapi.MungeSDPAddVideoContent(offer.SDP)
	r.mu.Lock()
	r.pubRemoteSet = false
	r.pubPending = nil
	r.mu.Unlock()
	return offer, nil
}

func (r *SFURelay) Close() {
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
		buf := make([]byte, common.UDPBufSize)
		for {
			if _, _, err := track.Read(buf); err != nil {
				return
			}
		}
	}

	var vp8Pkt codecs.VP8Packet
	var frameBuf []byte
	var lastSeq uint16
	var haveLastSeq bool
	frameValid := false
	var recvCount int
	bufSz := r.readBufSize
	if bufSz <= 0 {
		bufSz = common.RTPBufSize
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
		if haveLastSeq && pkt.SequenceNumber != lastSeq+1 {
			frameValid = false
			frameBuf = frameBuf[:0]
		}
		lastSeq = pkt.SequenceNumber
		haveLastSeq = true

		vp8Payload, err := vp8Pkt.Unmarshal(pkt.Payload)
		if err != nil {
			frameValid = false
			frameBuf = frameBuf[:0]
			continue
		}
		if vp8Pkt.S == 1 {
			frameBuf = frameBuf[:0]
			frameValid = true
		}
		if !frameValid {
			continue
		}
		frameBuf = append(frameBuf, vp8Payload...)
		if !pkt.Marker {
			continue
		}
		recvCount++
		if common.Debug && (recvCount <= 3 || recvCount%200 == 0) {
			log.Printf("[video] recv vp8 frame #%d %d bytes", recvCount, len(frameBuf))
		}

		res := r.obf.Decode(frameBuf)
		frameBuf = frameBuf[:0]
		frameValid = false

		if !res.HasFrame || res.SelfEcho {
			continue
		}
		if res.PeerRestart {
			log.Printf("[video] peer restart detected, new epoch=0x%08x", res.PeerEpoch)
			if r.OnPeerRestart != nil {
				r.OnPeerRestart()
			}
		}
		if res.Keepalive || len(res.Payload) == 0 {
			continue
		}
		if r.tun != nil && r.tun.OnData != nil {
			r.tun.OnData(res.Payload)
		}
	}
}
