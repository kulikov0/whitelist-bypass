package bitrix

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
	"whitelist-bypass/relay/common"
	"whitelist-bypass/relay/livekit"
	"whitelist-bypass/relay/tunnel"
)

const (
	TunnelModeAuto  = ""
	TunnelModeVideo = "video"
	TunnelModeDC    = "dc"
)

type MediaParams struct {
	Signal   *Signal
	Alias    string
	Mode     string
	FPS      int
	Batch    int
	Reliable bool
	LogFn    func(string, ...any)
}

type MediaSession struct {
	p         MediaParams
	obf       *tunnel.TunnelObfuscator
	sendTrack *webrtc.TrackLocalStaticSample
	vp8tun    *tunnel.VP8DataTunnel

	mu            sync.Mutex
	subReliableDC *webrtc.DataChannel
	pubDCHooked   bool
	dcStarted     bool
	tunFired      bool

	configAcked     chan struct{}
	configAckedOnce sync.Once
	stopCh          chan struct{}

	OnConnected func(tunnel.DataTunnel)
}

func NewMediaSession(p MediaParams) (*MediaSession, error) {
	if p.LogFn == nil {
		p.LogFn = log.Printf
	}
	obf, err := tunnel.NewTunnelObfuscator(tunnel.DeriveSecretFromJoinLink(p.Alias))
	if err != nil {
		return nil, err
	}
	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000},
		"tunnel", fmt.Sprintf("bitrix-tunnel-%08x", obf.LocalEpoch()))
	if err != nil {
		return nil, err
	}
	s := &MediaSession{
		p:           p,
		obf:         obf,
		sendTrack:   track,
		vp8tun:      tunnel.NewVP8DataTunnel(track, obf, p.LogFn),
		configAcked: make(chan struct{}),
		stopCh:      make(chan struct{}),
	}
	p.Signal.SetOnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		if remote.Codec().MimeType == webrtc.MimeTypeVP8 {
			go s.readVP8Track(remote)
		} else {
			go tunnel.DrainTrack(remote)
		}
	})
	p.Signal.SetOnDataChannel(func(dc *webrtc.DataChannel) {
		if dc.Label() != "_reliable" {
			return
		}
		s.mu.Lock()
		s.subReliableDC = dc
		s.mu.Unlock()
		dc.OnOpen(func() {
			s.p.LogFn("[bx] sub _reliable DC open")
			s.maybeStartDCTunnel()
		})
	})
	return s, nil
}

func (s *MediaSession) MarkConfigAcked() {
	s.configAckedOnce.Do(func() { close(s.configAcked) })
}

func (s *MediaSession) Stop() {
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
	s.vp8tun.Stop()
}

func (s *MediaSession) Start() error {
	sender, err := s.p.Signal.PublishVP8(s.sendTrack, "tunnel")
	if err != nil {
		return err
	}
	go tunnel.DrainSenderRTCP(sender)
	s.vp8tun.Start(s.p.FPS, s.p.Batch)
	s.p.LogFn("[bx] vp8 tunnel started fps=%d batch=%d", s.p.FPS, s.p.Batch)
	s.hookPubDC()
	s.startVP8Role()
	return nil
}

func (s *MediaSession) hookPubDC() {
	dc := s.p.Signal.PubReliableDC()
	if dc == nil {
		return
	}
	s.mu.Lock()
	if s.pubDCHooked {
		s.mu.Unlock()
		return
	}
	s.pubDCHooked = true
	s.mu.Unlock()
	dc.OnOpen(func() {
		s.p.LogFn("[bx] pub _reliable DC open")
		s.maybeStartDCTunnel()
	})
	if dc.ReadyState() == webrtc.DataChannelStateOpen {
		s.maybeStartDCTunnel()
	}
}

func (s *MediaSession) startVP8Role() {
	var active tunnel.DataTunnel = s.vp8tun
	if s.p.Mode == TunnelModeVideo && s.p.Reliable {
		active = s.wrapReliable(s.vp8tun)
	}
	switch s.p.Mode {
	case TunnelModeVideo:
		go s.configPingPong(active)
		s.fireOnConnected(active)
	case TunnelModeAuto:
		s.vp8tun.SetOnData(func(payload []byte) { s.activate(s.vp8tun, payload) })
	}
}

func (s *MediaSession) maybeStartDCTunnel() {
	if s.p.Mode == TunnelModeVideo {
		return
	}
	s.mu.Lock()
	subDC := s.subReliableDC
	s.mu.Unlock()
	pubDC := s.p.Signal.PubReliableDC()
	if pubDC == nil || subDC == nil {
		return
	}
	if pubDC.ReadyState() != webrtc.DataChannelStateOpen || subDC.ReadyState() != webrtc.DataChannelStateOpen {
		return
	}
	s.mu.Lock()
	if s.dcStarted {
		s.mu.Unlock()
		return
	}
	s.dcStarted = true
	s.mu.Unlock()

	subRaw, err := subDC.Detach()
	if err != nil {
		s.p.LogFn("[bx] detach sub _reliable: %v", err)
		return
	}
	pubRaw, err := pubDC.Detach()
	if err != nil {
		s.p.LogFn("[bx] detach pub _reliable: %v", err)
		return
	}
	readWrapped := livekit.NewDataPacketWrapper(subRaw, livekit.DataPacketKindReliable)
	writeWrapped := livekit.NewDataPacketWrapper(pubRaw, livekit.DataPacketKindReliable)
	dctun := tunnel.NewChunkedDCTunnelFromRaw(readWrapped, writeWrapped, s.obf, common.DCBufSize, s.p.LogFn)
	s.p.LogFn("[bx] dc tunnel ready (pub+sub _reliable)")

	switch s.p.Mode {
	case TunnelModeDC:
		s.fireOnConnected(dctun)
	case TunnelModeAuto:
		dctun.SetOnData(func(payload []byte) { s.activate(dctun, payload) })
	}
}

func (s *MediaSession) configPingPong(tun tunnel.DataTunnel) {
	tun.SendData(tunnel.EncodeVP8Config(s.vp8tun.FPS(), s.vp8tun.Batch(), 1))
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.configAcked:
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.p.LogFn("[bx] resending vp8 config (no ack yet)")
			tun.SendData(tunnel.EncodeVP8Config(s.vp8tun.FPS(), s.vp8tun.Batch(), 1))
		}
	}
}

func (s *MediaSession) fireOnConnected(tun tunnel.DataTunnel) {
	s.mu.Lock()
	if s.tunFired {
		s.mu.Unlock()
		return
	}
	s.tunFired = true
	s.mu.Unlock()
	if s.OnConnected != nil {
		s.OnConnected(tun)
	}
}

func (s *MediaSession) activate(tun tunnel.DataTunnel, payload []byte) {
	s.mu.Lock()
	if s.tunFired {
		s.mu.Unlock()
		return
	}
	s.tunFired = true
	s.mu.Unlock()

	var delivered tunnel.DataTunnel = tun
	useKCP := false
	if v, ok := tun.(*tunnel.VP8DataTunnel); ok && !tunnel.LooksLikeRelayFrame(payload) {
		delivered = s.wrapReliable(v)
		useKCP = true
	}
	s.p.LogFn("[bx] auto-detected active tunnel: %T", delivered)
	if s.OnConnected != nil {
		s.OnConnected(delivered)
	}
	switch v := tun.(type) {
	case *tunnel.DCTunnel:
		if fwd := v.OnData(); fwd != nil {
			fwd(payload)
		}
	case *tunnel.VP8DataTunnel:
		if useKCP {
			if k, ok := delivered.(*tunnel.MultiTrackKCPTunnel); ok {
				k.InjectSegment(payload)
			}
		} else if v.OnData != nil {
			v.OnData(payload)
		}
	}
}

func (s *MediaSession) wrapReliable(vp8 *tunnel.VP8DataTunnel) tunnel.DataTunnel {
	mt := tunnel.NewMultiTrackTunnel([]*tunnel.VP8DataTunnel{vp8})
	wrapped := tunnel.NewMultiTrackKCPTunnel(mt, s.p.LogFn)
	s.p.LogFn("[bx] per-track kcp reliability active over video tunnel")
	return wrapped
}

func (s *MediaSession) readVP8Track(track *webrtc.TrackRemote) {
	var vp8Pkt codecs.VP8Packet
	var frameBuf []byte
	var lastSeq uint16
	var haveLastSeq bool
	frameValid := false
	for {
		pkt, _, err := track.ReadRTP()
		if err != nil {
			return
		}
		if pkt == nil {
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
		s.vp8tun.HandleFrame(frameBuf)
		frameBuf = frameBuf[:0]
		frameValid = false
	}
}
