package bitrix

import (
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
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
	Signal    *Signal
	Alias     string
	Mode      string
	FPS       int
	Batch     int
	Reliable  bool
	DualTrack bool
	LogFn     func(string, ...any)
}

type MediaSession struct {
	p   MediaParams
	obf *tunnel.TunnelObfuscator

	mu           sync.Mutex
	sendTracks   []*webrtc.TrackLocalStaticSample
	transceivers []*webrtc.RTPTransceiver
	vp8tun       *tunnel.MultiTrackTunnel
	kcptun       *tunnel.MultiTrackKCPTunnel
	dctun        *tunnel.DCTunnel

	subReliableDC *webrtc.DataChannel
	pubDCHooked   bool
	dcStarted     bool
	tunFired      bool

	configAcked     chan struct{}
	configAckedOnce sync.Once
	stopCh          chan struct{}

	OnConnected   func(tunnel.DataTunnel)
	OnPeerRestart func()
}

func NewMediaSession(p MediaParams) (*MediaSession, error) {
	if p.LogFn == nil {
		p.LogFn = log.Printf
	}
	obf, err := tunnel.NewTunnelObfuscator(tunnel.DeriveSecretFromJoinLink(p.Alias))
	if err != nil {
		return nil, err
	}
	s := &MediaSession{
		p:           p,
		obf:         obf,
		configAcked: make(chan struct{}),
		stopCh:      make(chan struct{}),
	}

	count := 1
	if p.DualTrack {
		count = 2
	}
	tracks := make([]*webrtc.TrackLocalStaticSample, 0, count)
	subs := make([]*tunnel.VP8DataTunnel, 0, count)
	for i := 0; i < count; i++ {
		track, err := s.newVP8Track(i)
		if err != nil {
			return nil, err
		}
		tracks = append(tracks, track)
		subs = append(subs, tunnel.NewVP8DataTunnelWithQueue(track, obf, p.LogFn, tunnel.KCPCarrierQueueDepth))
	}
	s.sendTracks = tracks
	s.vp8tun = tunnel.NewMultiTrackTunnel(subs)
	s.vp8tun.SetOnPeerRestart(func() {
		s.p.LogFn("[bx] peer epoch changed, re-arming auto-detect")
		s.rearmAutoDetect()
		if s.OnPeerRestart != nil {
			s.OnPeerRestart()
		}
	})

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

func (s *MediaSession) newVP8Track(slot int) (*webrtc.TrackLocalStaticSample, error) {
	labelPrefix, streamPrefix := "videochannel-", "tunnel-video-"
	if slot > 0 {
		labelPrefix, streamPrefix = "screenchannel-", "tunnel-screen-"
	}
	return webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000},
		labelPrefix+uuid.New().String(), streamPrefix+uuid.New().String())
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
	s.mu.Lock()
	vp8 := s.vp8tun
	kcptun := s.kcptun
	s.mu.Unlock()
	if kcptun != nil {
		kcptun.Stop()
	}
	if vp8 != nil {
		vp8.Stop()
	}
}

func (s *MediaSession) Start() error {
	if err := s.p.Signal.WaitJoined(30 * time.Second); err != nil {
		return err
	}
	s.mu.Lock()
	tracks := s.sendTracks
	s.mu.Unlock()

	transceivers := make([]*webrtc.RTPTransceiver, 0, len(tracks))
	for i, t := range tracks {
		source := livekit.TrackSourceCamera
		if i > 0 {
			source = livekit.TrackSourceScreenShare
		}
		trx, err := s.p.Signal.AddPublisherTrack(t, source)
		if err != nil {
			return err
		}
		transceivers = append(transceivers, trx)
		go tunnel.DrainSenderRTCP(trx.Sender())
	}
	if err := s.p.Signal.Renegotiate(); err != nil {
		return err
	}
	s.mu.Lock()
	s.transceivers = transceivers
	s.mu.Unlock()

	s.vp8tun.Start(s.p.FPS, s.p.Batch)
	s.p.LogFn("[bx] vp8 tunnel started fps=%d batch=%d tracks=%d", s.p.FPS, s.p.Batch, len(tracks))
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
	tun := s.currentVP8Tun()
	var active tunnel.DataTunnel = tun
	if s.p.Mode == TunnelModeVideo && s.p.Reliable {
		active = s.wrapReliable(tun)
	}
	switch s.p.Mode {
	case TunnelModeVideo:
		go s.configPingPong(active, tun.SubTunnelCount())
		s.fireOnConnected(active)
	case TunnelModeAuto:
		tun.SetOnData(func(payload []byte) { s.activate(tun, payload) })
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
	s.mu.Lock()
	s.dctun = dctun
	s.mu.Unlock()
	s.p.LogFn("[bx] dc tunnel ready pub+sub _reliable")

	switch s.p.Mode {
	case TunnelModeDC:
		s.fireOnConnected(dctun)
	case TunnelModeAuto:
		dctun.SetOnData(func(payload []byte) { s.activate(dctun, payload) })
	}
}

func (s *MediaSession) configPingPong(tun tunnel.DataTunnel, trackCount int) {
	tun.SendData(tunnel.EncodeVP8Config(s.p.FPS, s.p.Batch, trackCount))
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.configAcked:
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.p.LogFn("[bx] resending vp8 config no ack yet")
			tun.SendData(tunnel.EncodeVP8Config(s.p.FPS, s.p.Batch, trackCount))
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
	if _, ok := tun.(*tunnel.MultiTrackTunnel); ok && !tunnel.LooksLikeRelayFrame(payload) {
		delivered = s.wrapReliable(tun)
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
	case *tunnel.MultiTrackTunnel:
		if useKCP {
			if k, ok := delivered.(*tunnel.MultiTrackKCPTunnel); ok {
				k.InjectSegment(payload)
			}
		} else {
			v.DeliverData(payload)
		}
	}
}

func (s *MediaSession) wrapReliable(tun tunnel.DataTunnel) tunnel.DataTunnel {
	mt, ok := tun.(*tunnel.MultiTrackTunnel)
	if !ok {
		return tun
	}
	wrapped := tunnel.NewMultiTrackKCPTunnel(mt, s.p.LogFn)
	s.mu.Lock()
	s.kcptun = wrapped
	s.mu.Unlock()
	s.p.LogFn("[bx] per-track kcp reliability active over video tunnel")
	return wrapped
}

func (s *MediaSession) currentVP8Tun() *tunnel.MultiTrackTunnel {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.vp8tun
}

func (s *MediaSession) rearmAutoDetect() {
	if s.p.Mode != TunnelModeAuto {
		return
	}
	s.mu.Lock()
	s.tunFired = false
	orphanKCP := s.kcptun
	s.kcptun = nil
	vp8 := s.vp8tun
	dc := s.dctun
	s.mu.Unlock()
	if orphanKCP != nil {
		orphanKCP.StopLayer()
	}
	if vp8 != nil {
		vp8.SetOnData(func(payload []byte) { s.activate(vp8, payload) })
	}
	if dc != nil {
		dc.SetOnData(func(payload []byte) { s.activate(dc, payload) })
	}
}

func (s *MediaSession) AdaptTrackCount(peerCount int) {
	if peerCount < 1 {
		return
	}
	s.mu.Lock()
	current := len(s.sendTracks)
	s.mu.Unlock()
	if peerCount == current {
		s.p.LogFn("[bx] adapt-track-count: peer=%d current=%d, no change", peerCount, current)
		return
	}
	if peerCount > current {
		s.p.LogFn("[bx] adapt-track-count: scaling publisher tracks %d -> %d", current, peerCount)
		for i := current; i < peerCount; i++ {
			if !s.addPublisherTrack(i) {
				return
			}
		}
	} else {
		s.p.LogFn("[bx] adapt-track-count: shrinking publisher tracks %d -> %d", current, peerCount)
		for i := current; i > peerCount; i-- {
			if !s.removePublisherTrack() {
				return
			}
		}
	}
	if err := s.p.Signal.Renegotiate(); err != nil {
		s.p.LogFn("[bx] adapt-track-count: renegotiate: %v", err)
		return
	}
	s.p.LogFn("[bx] adapt-track-count: renegotiation offer sent")
}

func (s *MediaSession) addPublisherTrack(slot int) bool {
	source := livekit.TrackSourceScreenShare
	if slot == 0 {
		source = livekit.TrackSourceCamera
	}
	track, err := s.newVP8Track(slot)
	if err != nil {
		s.p.LogFn("[bx] adapt-track-count: new track slot=%d: %v", slot, err)
		return false
	}
	trx, err := s.p.Signal.AddPublisherTrack(track, source)
	if err != nil {
		s.p.LogFn("[bx] adapt-track-count: add track slot=%d: %v", slot, err)
		return false
	}
	go tunnel.DrainSenderRTCP(trx.Sender())
	s.mu.Lock()
	s.sendTracks = append(s.sendTracks, track)
	s.transceivers = append(s.transceivers, trx)
	vp8 := s.vp8tun
	kcptun := s.kcptun
	s.mu.Unlock()
	if vp8 != nil {
		newSub := tunnel.NewVP8DataTunnelWithQueue(track, s.obf, s.p.LogFn, tunnel.KCPCarrierQueueDepth)
		vp8.AddSubTunnel(newSub)
		if kcptun != nil {
			kcptun.AddSession(newSub)
		}
	}
	return true
}

func (s *MediaSession) removePublisherTrack() bool {
	s.mu.Lock()
	if len(s.transceivers) <= 1 || len(s.sendTracks) <= 1 {
		s.mu.Unlock()
		s.p.LogFn("[bx] adapt-track-count: refusing to remove cam slot")
		return false
	}
	last := len(s.transceivers) - 1
	trx := s.transceivers[last]
	s.transceivers = s.transceivers[:last]
	s.sendTracks = s.sendTracks[:last]
	vp8 := s.vp8tun
	kcptun := s.kcptun
	s.mu.Unlock()
	if kcptun != nil {
		kcptun.RemoveLastSession()
	}
	if vp8 != nil {
		vp8.RemoveLastSubTunnel()
	}
	if err := trx.Stop(); err != nil {
		s.p.LogFn("[bx] adapt-track-count: stop transceiver: %v", err)
		return false
	}
	return true
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
		if tun := s.currentVP8Tun(); tun != nil {
			tun.HandleFrame(frameBuf)
		}
		frameBuf = frameBuf[:0]
		frameValid = false
	}
}
