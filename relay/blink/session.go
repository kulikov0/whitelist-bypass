package blink

import (
	"context"
	"log"
	"net"
	"sync"

	"whitelist-bypass/relay/common"
	"whitelist-bypass/relay/livekit"
	"whitelist-bypass/relay/tunnel"
	"whitelist-bypass/relay/tunnel/rtc"

	headless "github.com/kulikov0/headless-client"
	"github.com/kulikov0/headless-client/webrtc"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
)

const (
	TunnelModeVideo = "video"
	TunnelModeDC    = "dc"
)

type SessionConfig struct {
	RoomToken              string
	ServerURL              string
	TunnelMode             string
	Obfuscator             *tunnel.TunnelObfuscator
	LogFn                  func(string, ...any)
	ConfigureSettingEngine func(*webrtc.SettingEngine)
	NetDialContext         func(ctx context.Context, network, addr string) (net.Conn, error)
	ResolveICEHost         func(host string) (string, error)
	VP8FPS                 int
	VP8Batch               int
	ReadBuf                int
	ScreenShare            bool
	IsJoiner               bool
	Reliable               bool
}

// TODO: Session almost entirely copies wbstream.Session, minus the kick and promote logic.
// Extract the shared part into one place once the WB Stream service works again as before,
// so that its e2e can guard the refactor.
type Session struct {
	cfg SessionConfig

	lk                 *livekit.Client
	sampleTracks       []*webrtc.TrackLocalStaticSample
	sampleTransceivers []*webrtc.RTPTransceiver

	pubReliableDC      *webrtc.DataChannel
	pubReliableDCReady bool
	subReliableDC      *webrtc.DataChannel

	vp8tun    *rtc.MultiTrackTunnel
	kcptun    *rtc.MultiTrackKCPTunnel
	dctun     *rtc.DCTunnel
	dcStarted bool
	mu        sync.Mutex
	tunFired  bool
	done      chan struct{}

	configAcked     chan struct{}
	configAckedOnce sync.Once

	OnConnected       func(tunnel.DataTunnel)
	OnPeerRestart     func()
	OnRemoteCandidate func(target int, candidateOrSDP string)
}

func NewSession(cfg SessionConfig) *Session {
	if cfg.LogFn == nil {
		cfg.LogFn = log.Printf
	}
	return &Session{
		cfg:         cfg,
		done:        make(chan struct{}),
		configAcked: make(chan struct{}),
	}
}

func (s *Session) MarkConfigAcked() {
	s.configAckedOnce.Do(func() {
		s.cfg.LogFn("[lk] peer acked vp8 config")
		close(s.configAcked)
	})
}

func (s *Session) Done() <-chan struct{} { return s.done }

func (s *Session) Start() error {
	lk, err := livekit.NewClient(livekit.Config{
		ServerURL:              s.cfg.ServerURL,
		Token:                  s.cfg.RoomToken,
		Origin:                 Origin,
		UserAgent:              headless.ChromeWindows.UserAgent(),
		Codec:                  livekit.ProtoCodec{},
		LogFn:                  s.cfg.LogFn,
		ConfigureSettingEngine: s.cfg.ConfigureSettingEngine,
		NetDialContext:         s.cfg.NetDialContext,
		ResolveICEHost:         s.cfg.ResolveICEHost,
	})
	if err != nil {
		return err
	}
	s.lk = lk
	s.lk.OnReady = s.onLKReady
	s.lk.OnTrack = s.onRemoteTrack
	s.lk.OnDataChannel = s.onRemoteDataChannel
	s.lk.OnPubConnected = s.startTunnel
	s.lk.OnRemoteCandidate = func(target int, ic webrtc.ICECandidateInit) {
		if s.OnRemoteCandidate != nil {
			s.OnRemoteCandidate(target, ic.Candidate)
		}
	}
	s.lk.OnRemoteSDP = func(target int, _, sdp string) {
		if s.OnRemoteCandidate != nil {
			s.OnRemoteCandidate(-1, sdp)
		}
	}

	if err := s.lk.Connect(); err != nil {
		return err
	}
	go s.lk.PingLoop()
	go func() {
		if err := s.lk.ReadLoop(); err != nil {
			s.cfg.LogFn("[lk] read loop ended: %v", err)
		}
		s.stopTunnels()
		close(s.done)
	}()
	return nil
}

func (s *Session) stopTunnels() {
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

func (s *Session) onLKReady() {
	pubPC := s.lk.PubPC()
	if pubPC == nil {
		return
	}

	trackCam, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000},
	)
	if err != nil {
		s.cfg.LogFn("[lk] create local cam track: %v", err)
		return
	}
	tracks := []*webrtc.TrackLocalStaticSample{trackCam}

	if s.cfg.ScreenShare {
		trackScreen, err := webrtc.NewTrackLocalStaticSample(
			webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000},
		)
		if err != nil {
			s.cfg.LogFn("[lk] create local screen track: %v", err)
			return
		}
		tracks = append(tracks, trackScreen)
	}

	transceivers := make([]*webrtc.RTPTransceiver, 0, len(tracks))
	for _, track := range tracks {
		transceiver, err := pubPC.AddTransceiverFromTrack(track,
			webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly})
		if err != nil {
			s.cfg.LogFn("[lk] add transceiver: %v", err)
			return
		}
		transceivers = append(transceivers, transceiver)
		go rtc.DrainSenderRTCP(transceiver.Sender())
	}

	s.mu.Lock()
	s.sampleTracks = tracks
	s.sampleTransceivers = transceivers
	s.mu.Unlock()

	ordered := true
	reliableChannel, err := pubPC.CreateDataChannel("_reliable", &webrtc.DataChannelInit{
		Ordered: &ordered,
	})
	if err != nil {
		s.cfg.LogFn("[lk] create reliable DC: %v", err)
		return
	}
	s.mu.Lock()
	s.pubReliableDC = reliableChannel
	s.mu.Unlock()
	reliableChannel.OnOpen(func() {
		s.cfg.LogFn("[lk] reliable DC open")
		s.mu.Lock()
		s.pubReliableDCReady = true
		s.mu.Unlock()
		s.maybeStartDCTunnel()
	})

	for index, track := range tracks {
		source := livekit.TrackSourceCamera
		if index > 0 {
			source = livekit.TrackSourceScreenShare
		}
		if err := s.lk.SendAddTrack(track.ID(), "videochannel",
			livekit.TrackTypeVideo, source, 1280, 720); err != nil {
			s.cfg.LogFn("[lk] send add-track: %v", err)
			return
		}
	}

	offer, err := pubPC.CreateOffer(nil)
	if err != nil {
		s.cfg.LogFn("[lk] create offer: %v", err)
		return
	}
	if err := pubPC.SetLocalDescription(offer); err != nil {
		s.cfg.LogFn("[lk] set local offer: %v", err)
		return
	}
	if err := s.lk.SendOffer(offer.SDP); err != nil {
		s.cfg.LogFn("[lk] send offer: %v", err)
		return
	}
	s.cfg.LogFn("[lk] sent publisher offer, %d bytes", len(offer.SDP))
}

func (s *Session) startTunnel() {
	s.mu.Lock()
	if s.vp8tun != nil || len(s.sampleTracks) == 0 {
		s.mu.Unlock()
		return
	}
	subTunnels := make([]*rtc.VP8DataTunnel, 0, len(s.sampleTracks))
	for _, track := range s.sampleTracks {
		subTunnels = append(subTunnels, rtc.NewVP8DataTunnelWithQueue(track, s.cfg.Obfuscator, s.cfg.LogFn, rtc.KCPCarrierQueueDepth))
	}
	s.vp8tun = rtc.NewMultiTrackTunnel(subTunnels)
	s.vp8tun.SetOnPeerRestart(func() {
		s.cfg.LogFn("[blink] peer epoch changed, signalling peer-restart")
		s.rearmAutoDetect()
		if s.OnPeerRestart != nil {
			s.OnPeerRestart()
		}
	})
	s.vp8tun.Start(s.cfg.VP8FPS, s.cfg.VP8Batch)
	videoTunnel := s.vp8tun
	s.mu.Unlock()
	s.cfg.LogFn("[lk] vp8 tunnel writer started tracks=%d", len(subTunnels))
	var active tunnel.DataTunnel = videoTunnel
	if s.cfg.TunnelMode == TunnelModeVideo && s.cfg.Reliable {
		active = s.maybeWrapReliable(videoTunnel)
	}
	if s.cfg.IsJoiner && s.cfg.TunnelMode != TunnelModeDC {
		go s.configPingPong(active, len(subTunnels))
	}

	if s.cfg.TunnelMode == TunnelModeVideo {
		s.fireOnConnected(active)
		return
	}
	if s.cfg.TunnelMode == "" {
		videoTunnel.SetOnData(func(payload []byte) { s.activate(videoTunnel, payload) })
	}
}

func (s *Session) configPingPong(dataTunnel tunnel.DataTunnel, trackCount int) {
	tunnel.SendVP8ConfigUntilAcked(s.configAcked, nil, s.done, dataTunnel,
		s.cfg.VP8FPS, s.cfg.VP8Batch, trackCount, s.cfg.LogFn, "[lk]")
}

func (s *Session) maybeStartDCTunnel() {
	s.mu.Lock()
	pubDC := s.pubReliableDC
	subDC := s.subReliableDC
	pubReady := s.pubReliableDCReady
	s.mu.Unlock()
	if pubDC == nil || subDC == nil || !pubReady {
		return
	}
	if subDC.ReadyState() != webrtc.DataChannelStateOpen {
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
		s.cfg.LogFn("[lk] detach sub DC: %v", err)
		return
	}
	pubRaw, err := pubDC.Detach()
	if err != nil {
		s.cfg.LogFn("[lk] detach pub DC: %v", err)
		return
	}
	readWrapped := livekit.NewDataPacketWrapper(subRaw, livekit.DataPacketKindReliable)
	writeWrapped := livekit.NewDataPacketWrapper(pubRaw, livekit.DataPacketKindReliable)
	readBuf := s.cfg.ReadBuf
	if readBuf == 0 {
		readBuf = common.DCBufSize
	}
	dcTunnel := rtc.NewChunkedDCTunnelFromRaw(readWrapped, writeWrapped, s.cfg.Obfuscator, readBuf, s.cfg.LogFn)
	s.mu.Lock()
	s.dctun = dcTunnel
	s.mu.Unlock()
	s.cfg.LogFn("[lk] dc tunnel ready, pub and sub _reliable")

	if s.cfg.TunnelMode == TunnelModeDC {
		s.fireOnConnected(dcTunnel)
		return
	}
	if s.cfg.TunnelMode == "" {
		dcTunnel.SetOnData(func(payload []byte) { s.activate(dcTunnel, payload) })
	}
}

func (s *Session) fireOnConnected(dataTunnel tunnel.DataTunnel) {
	s.mu.Lock()
	if s.tunFired {
		s.mu.Unlock()
		return
	}
	s.tunFired = true
	s.mu.Unlock()
	if s.OnConnected != nil {
		s.OnConnected(dataTunnel)
	}
}

func (s *Session) activate(dataTunnel tunnel.DataTunnel, payload []byte) {
	s.mu.Lock()
	if s.tunFired {
		s.mu.Unlock()
		return
	}
	s.tunFired = true
	s.mu.Unlock()
	var delivered tunnel.DataTunnel = dataTunnel
	useKCP := false
	if _, ok := dataTunnel.(*rtc.MultiTrackTunnel); ok && !tunnel.LooksLikeRelayFrame(payload) {
		delivered = s.maybeWrapReliable(dataTunnel)
		useKCP = true
	}
	s.cfg.LogFn("[lk] auto-detected active tunnel: %T", delivered)
	if s.OnConnected != nil {
		s.OnConnected(delivered)
	}
	switch typed := dataTunnel.(type) {
	case *rtc.DCTunnel:
		if forward := typed.OnData(); forward != nil {
			forward(payload)
		}
	case *rtc.MultiTrackTunnel:
		if useKCP {
			if kcpTunnel, ok := delivered.(*rtc.MultiTrackKCPTunnel); ok {
				kcpTunnel.InjectSegment(payload)
			}
		} else {
			typed.DeliverData(payload)
		}
	}
}

func (s *Session) maybeWrapReliable(dataTunnel tunnel.DataTunnel) tunnel.DataTunnel {
	videoTunnel, ok := dataTunnel.(*rtc.MultiTrackTunnel)
	if !ok {
		return dataTunnel
	}
	wrapped := rtc.NewMultiTrackKCPTunnel(videoTunnel, s.cfg.LogFn)
	s.mu.Lock()
	if s.kcptun != nil {
		s.kcptun.StopLayer()
	}
	s.kcptun = wrapped
	s.mu.Unlock()
	s.cfg.LogFn("[lk] per-track kcp reliability active over video tunnel")
	return wrapped
}

func (s *Session) currentVP8Tun() *rtc.MultiTrackTunnel {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.vp8tun
}

func (s *Session) AdaptTrackCount(peerCount int) {
	if peerCount < 1 {
		return
	}
	pubPC := s.lk.PubPC()
	if pubPC == nil {
		return
	}
	s.mu.Lock()
	current := len(s.sampleTracks)
	s.mu.Unlock()
	if peerCount == current {
		s.cfg.LogFn("[lk] adapt-track-count: peer=%d current=%d, no change", peerCount, current)
		return
	}
	if peerCount > current {
		s.cfg.LogFn("[lk] adapt-track-count: scaling publisher tracks %d -> %d", current, peerCount)
		for slot := current; slot < peerCount; slot++ {
			if !s.addPublisherTrack(pubPC, slot) {
				return
			}
		}
	} else {
		s.cfg.LogFn("[lk] adapt-track-count: shrinking publisher tracks %d -> %d", current, peerCount)
		for remaining := current; remaining > peerCount; remaining-- {
			if !s.removePublisherTrack() {
				return
			}
		}
	}
	offer, err := pubPC.CreateOffer(nil)
	if err != nil {
		s.cfg.LogFn("[lk] adapt-track-count: create offer: %v", err)
		return
	}
	if err := pubPC.SetLocalDescription(offer); err != nil {
		s.cfg.LogFn("[lk] adapt-track-count: set local offer: %v", err)
		return
	}
	if err := s.lk.SendOffer(offer.SDP); err != nil {
		s.cfg.LogFn("[lk] adapt-track-count: send offer: %v", err)
		return
	}
	s.cfg.LogFn("[lk] adapt-track-count: renegotiation offer sent, %d bytes", len(offer.SDP))
}

func (s *Session) removePublisherTrack() bool {
	s.mu.Lock()
	if len(s.sampleTransceivers) <= 1 || len(s.sampleTracks) <= 1 {
		s.mu.Unlock()
		s.cfg.LogFn("[lk] adapt-track-count: refusing to remove cam slot")
		return false
	}
	last := len(s.sampleTransceivers) - 1
	transceiver := s.sampleTransceivers[last]
	s.sampleTransceivers = s.sampleTransceivers[:last]
	s.sampleTracks = s.sampleTracks[:last]
	videoTunnel := s.vp8tun
	kcpTunnel := s.kcptun
	s.mu.Unlock()
	if kcpTunnel != nil {
		kcpTunnel.RemoveLastSession()
	}
	if videoTunnel != nil {
		videoTunnel.RemoveLastSubTunnel()
	}
	if err := transceiver.Stop(); err != nil {
		s.cfg.LogFn("[lk] adapt-track-count: stop transceiver: %v", err)
		return false
	}
	return true
}

func (s *Session) addPublisherTrack(pubPC *webrtc.PeerConnection, slot int) bool {
	source := livekit.TrackSourceScreenShare
	if slot == 0 {
		source = livekit.TrackSourceCamera
	}
	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000},
	)
	if err != nil {
		s.cfg.LogFn("[lk] adapt-track-count: new track slot=%d: %v", slot, err)
		return false
	}
	transceiver, err := pubPC.AddTransceiverFromTrack(track,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly})
	if err != nil {
		s.cfg.LogFn("[lk] adapt-track-count: add transceiver slot=%d: %v", slot, err)
		return false
	}
	go rtc.DrainSenderRTCP(transceiver.Sender())
	if err := s.lk.SendAddTrack(track.ID(), "videochannel",
		livekit.TrackTypeVideo, source, 1280, 720); err != nil {
		s.cfg.LogFn("[lk] adapt-track-count: send add-track slot=%d: %v", slot, err)
		return false
	}
	s.mu.Lock()
	s.sampleTracks = append(s.sampleTracks, track)
	s.sampleTransceivers = append(s.sampleTransceivers, transceiver)
	videoTunnel := s.vp8tun
	kcpTunnel := s.kcptun
	s.mu.Unlock()
	if videoTunnel != nil {
		newSubTunnel := rtc.NewVP8DataTunnelWithQueue(track, s.cfg.Obfuscator, s.cfg.LogFn, rtc.KCPCarrierQueueDepth)
		videoTunnel.AddSubTunnel(newSubTunnel)
		if kcpTunnel != nil {
			kcpTunnel.AddSession(newSubTunnel)
		}
	}
	return true
}

func (s *Session) rearmAutoDetect() {
	if s.cfg.TunnelMode != "" {
		return
	}
	s.mu.Lock()
	s.tunFired = false
	orphanKCP := s.kcptun
	s.kcptun = nil
	videoTunnel := s.vp8tun
	dcTunnel := s.dctun
	s.mu.Unlock()
	if orphanKCP != nil {
		orphanKCP.StopLayer()
	}
	if videoTunnel != nil {
		videoTunnel.SetOnData(func(payload []byte) { s.activate(videoTunnel, payload) })
	}
	if dcTunnel != nil {
		dcTunnel.SetOnData(func(payload []byte) { s.activate(dcTunnel, payload) })
	}
}

func (s *Session) onRemoteTrack(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
	if track.Codec().MimeType != webrtc.MimeTypeVP8 {
		go rtc.DrainTrack(track)
		return
	}
	go s.readVP8Track(track)
}

func (s *Session) readVP8Track(track *webrtc.TrackRemote) {
	var vp8Packet codecs.VP8Packet
	var frameBuf []byte
	var lastSequence uint16
	var haveLastSequence bool
	frameValid := false
	var receivedFrames int
	readBuffer := make([]byte, common.RTPBufSize)
	for {
		readBytes, _, err := track.Read(readBuffer)
		if err != nil {
			return
		}
		packet := &rtp.Packet{}
		if packet.Unmarshal(readBuffer[:readBytes]) != nil {
			continue
		}
		if haveLastSequence && packet.SequenceNumber != lastSequence+1 {
			frameValid = false
			frameBuf = frameBuf[:0]
		}
		lastSequence = packet.SequenceNumber
		haveLastSequence = true

		vp8Payload, err := vp8Packet.Unmarshal(packet.Payload)
		if err != nil {
			frameValid = false
			frameBuf = frameBuf[:0]
			continue
		}
		if vp8Packet.S == 1 {
			frameBuf = frameBuf[:0]
			frameValid = true
		}
		if !frameValid {
			continue
		}
		frameBuf = append(frameBuf, vp8Payload...)
		if !packet.Marker {
			continue
		}
		receivedFrames++
		if common.Debug && (receivedFrames <= 3 || receivedFrames%200 == 0) {
			s.cfg.LogFn("[lk-video] recv vp8 frame #%d %d bytes", receivedFrames, len(frameBuf))
		}

		videoTunnel := s.currentVP8Tun()
		if videoTunnel != nil {
			videoTunnel.HandleFrame(frameBuf)
		}
		frameBuf = frameBuf[:0]
		frameValid = false
	}
}

func (s *Session) onRemoteDataChannel(channel *webrtc.DataChannel) {
	s.cfg.LogFn("[lk] remote DC label=%s id=%v", channel.Label(), channel.ID())
	if channel.Label() != "_reliable" {
		return
	}
	s.mu.Lock()
	s.subReliableDC = channel
	s.mu.Unlock()
	channel.OnOpen(func() {
		s.cfg.LogFn("[lk] remote _reliable DC open")
		s.maybeStartDCTunnel()
	})
}

func (s *Session) Close() {
	if s.lk != nil {
		s.lk.Close()
	}
}
