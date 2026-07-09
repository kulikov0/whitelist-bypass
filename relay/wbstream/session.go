package wbstream

import (
	"context"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
	"whitelist-bypass/relay/common"
	"whitelist-bypass/relay/livekit"
	"whitelist-bypass/relay/tunnel"
)

type peerEntry struct {
	sid       string
	identity  string
	firstSeen time.Time
	state     int32
	promoted  bool
}

const (
	TunnelModeVideo = "video"
	TunnelModeDC    = "dc"
)

type SessionConfig struct {
	RoomToken      string
	ServerURL      string
	DisplayName    string
	TunnelMode     string
	Obfuscator     *tunnel.TunnelObfuscator
	LogFn          func(string, ...any)
	SettingEngine  *webrtc.SettingEngine
	NetDialContext func(ctx context.Context, network, addr string) (net.Conn, error)
	ResolveICEHost func(host string) (string, error)
	VP8FPS         int
	VP8Batch       int
	RoomID         string
	AccessToken    string
	ReadBuf        int
	ScreenShare    bool // when true, publish a second VP8 track as ScreenShare and shard outbound across both
	IsJoiner       bool // when true, run the configPingPong loop; only the joiner sends VP8 config to the peer
}


type Session struct {
	cfg SessionConfig

	lk                 *livekit.Client
	sampleTracks       []*webrtc.TrackLocalStaticSample
	sampleTransceivers []*webrtc.RTPTransceiver

	pubReliableDC      *webrtc.DataChannel
	pubReliableDCReady bool
	subReliableDC      *webrtc.DataChannel

	vp8tun       *tunnel.MultiTrackTunnel
	kcptun       *tunnel.KCPTunnel
	dctun        *tunnel.DCTunnel
	mu           sync.Mutex
	tunFired     bool
	remoteTracks int
	done         chan struct{}

	peersBySID map[string]peerEntry // SID -> first-seen time + state
	kickedSIDs map[string]bool      // SIDs we kicked; SFU may still echo them as Active until it processes the kick

	configAcked     chan struct{}
	configAckedOnce sync.Once

	OnConnected   func(tunnel.DataTunnel)
	OnPeerRestart func()
	// OnRemoteCandidate is forwarded from the underlying LiveKit client.
	// It fires for every trickle ICE candidate sent by the SFU, plus
	// once with target=-1 for every SDP description (carrying inline
	// candidates) before the description is applied to Pion.
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
	s.lk = livekit.NewClient(livekit.Config{
		ServerURL:      s.cfg.ServerURL,
		Token:          s.cfg.RoomToken,
		Origin:         Origin,
		UserAgent:      common.UserAgent,
		LogFn:          s.cfg.LogFn,
		SettingEngine:  s.cfg.SettingEngine,
		NetDialContext: s.cfg.NetDialContext,
		ResolveICEHost: s.cfg.ResolveICEHost,
	})
	s.lk.OnReady = s.onLKReady
	s.lk.OnTrack = s.onRemoteTrack
	s.lk.OnDataChannel = s.onRemoteDataChannel
	s.lk.OnPubConnected = s.startTunnel
	if s.cfg.AccessToken != "" && s.cfg.RoomID != "" {
		s.lk.OnParticipantUpdate = s.onParticipantUpdate
	}
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

	camID := "videochannel-" + uuid.New().String()
	trackCam, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000},
		camID, "tunnel-video-"+uuid.New().String(),
	)
	if err != nil {
		s.cfg.LogFn("[lk] create local cam track: %v", err)
		return
	}
	tracks := []*webrtc.TrackLocalStaticSample{trackCam}

	if s.cfg.ScreenShare {
		screenID := "screenchannel-" + uuid.New().String()
		trackScreen, err := webrtc.NewTrackLocalStaticSample(
			webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000},
			screenID, "tunnel-screen-"+uuid.New().String(),
		)
		if err != nil {
			s.cfg.LogFn("[lk] create local screen track: %v", err)
			return
		}
		tracks = append(tracks, trackScreen)
	}

	transceivers := make([]*webrtc.RTPTransceiver, 0, len(tracks))
	for _, t := range tracks {
		trx, err := pubPC.AddTransceiverFromTrack(t,
			webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly})
		if err != nil {
			s.cfg.LogFn("[lk] add transceiver: %v", err)
			return
		}
		transceivers = append(transceivers, trx)
	}

	s.mu.Lock()
	s.sampleTracks = tracks
	s.sampleTransceivers = transceivers
	s.mu.Unlock()

	ordered := true
	dc, err := pubPC.CreateDataChannel("_reliable", &webrtc.DataChannelInit{
		Ordered: &ordered,
	})
	if err != nil {
		s.cfg.LogFn("[lk] create reliable DC: %v", err)
		return
	}
	s.mu.Lock()
	s.pubReliableDC = dc
	s.mu.Unlock()
	dc.OnOpen(func() {
		s.cfg.LogFn("[lk] reliable DC open")
		s.mu.Lock()
		s.pubReliableDCReady = true
		s.mu.Unlock()
		s.maybeStartDCTunnel()
	})

	for i, t := range tracks {
		source := livekit.TrackSourceCamera
		if i > 0 {
			source = livekit.TrackSourceScreenShare
		}
		if err := s.lk.SendAddTrack(t.ID(), "videochannel",
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
	s.cfg.LogFn("[lk] sent publisher offer (%d bytes)", len(offer.SDP))
}

func (s *Session) startTunnel() {
	s.mu.Lock()
	if s.vp8tun != nil || len(s.sampleTracks) == 0 {
		s.mu.Unlock()
		return
	}
	subs := make([]*tunnel.VP8DataTunnel, 0, len(s.sampleTracks))
	for _, t := range s.sampleTracks {
		subs = append(subs, tunnel.NewVP8DataTunnel(t, s.cfg.Obfuscator, s.cfg.LogFn))
	}
	s.vp8tun = tunnel.NewMultiTrackTunnel(subs)
	s.vp8tun.Start(s.cfg.VP8FPS, s.cfg.VP8Batch)
	tun := s.vp8tun
	s.mu.Unlock()
	s.cfg.LogFn("[lk] vp8 tunnel writer started tracks=%d", len(subs))
	var active tunnel.DataTunnel = tun
	if s.cfg.TunnelMode == TunnelModeVideo {
		active = s.maybeWrapReliable(tun)
	}
	if s.cfg.IsJoiner && s.cfg.TunnelMode != TunnelModeDC {
		go s.configPingPong(active, len(subs))
	}

	if s.cfg.TunnelMode == TunnelModeVideo {
		s.fireOnConnected(active)
		return
	}
	if s.cfg.TunnelMode == "" {
		tun.SetOnData(func(payload []byte) { s.activate(tun, payload) })
	}
}

func (s *Session) configPingPong(tun tunnel.DataTunnel, trackCount int) {
	frame := tunnel.EncodeVP8Config(s.cfg.VP8FPS, s.cfg.VP8Batch, trackCount)
	tun.SendData(frame)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.configAcked:
			return
		case <-s.done:
			return
		case <-ticker.C:
			s.cfg.LogFn("[lk] resending vp8 config (no ack yet)")
			tun.SendData(tunnel.EncodeVP8Config(s.cfg.VP8FPS, s.cfg.VP8Batch, trackCount))
		}
	}
}

func (s *Session) maybeStartDCTunnel() {
	s.mu.Lock()
	if s.dctun != nil {
		s.mu.Unlock()
		return
	}
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
	readWrapped := newDataPacketWrapper(subRaw, livekit.DataPacketKindReliable)
	writeWrapped := newDataPacketWrapper(pubRaw, livekit.DataPacketKindReliable)
	readBuf := s.cfg.ReadBuf
	if readBuf == 0 {
		readBuf = common.DCBufSize
	}
	dctun := tunnel.NewChunkedDCTunnelFromRaw(readWrapped, writeWrapped, s.cfg.Obfuscator, readBuf, s.cfg.LogFn)
	if dctun == nil {
		return
	}
	s.mu.Lock()
	s.dctun = dctun
	s.mu.Unlock()
	s.cfg.LogFn("[lk] dc tunnel ready (pub+sub _reliable)")

	if s.cfg.TunnelMode == TunnelModeDC {
		s.fireOnConnected(dctun)
		return
	}
	if s.cfg.TunnelMode == "" {
		dctun.SetOnData(func(payload []byte) { s.activate(dctun, payload) })
	}
}

func (s *Session) fireOnConnected(tun tunnel.DataTunnel) {
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

func (s *Session) activate(tun tunnel.DataTunnel, payload []byte) {
	s.mu.Lock()
	if s.tunFired {
		s.mu.Unlock()
		return
	}
	s.tunFired = true
	s.mu.Unlock()
	delivered := s.maybeWrapReliable(tun)
	s.cfg.LogFn("[lk] auto-detected active tunnel: %T", delivered)
	if s.OnConnected != nil {
		s.OnConnected(delivered)
	}
	var fwd func([]byte)
	switch v := tun.(type) {
	case *tunnel.DCTunnel:
		fwd = v.OnData()
	case *tunnel.MultiTrackTunnel:
		// MultiTrackTunnel does not expose a readable OnData hook; the trigger
		// payload is dropped here. Next frame will arrive via the SetOnData
		// callback OnConnected wires up.
	}
	if fwd != nil {
		fwd(payload)
	}
}

func (s *Session) maybeWrapReliable(tun tunnel.DataTunnel) tunnel.DataTunnel {
	vp8, ok := tun.(*tunnel.MultiTrackTunnel)
	if !ok {
		return tun
	}
	wrapped := tunnel.NewKCPTunnel(vp8, s.cfg.LogFn)
	s.mu.Lock()
	s.kcptun = wrapped
	s.mu.Unlock()
	s.cfg.LogFn("[lk] kcp reliability wrapper active over video tunnel")
	return wrapped
}

func (s *Session) currentVP8Tun() *tunnel.MultiTrackTunnel {
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
		for i := current; i < peerCount; i++ {
			if !s.addPublisherTrack(pubPC, i) {
				return
			}
		}
	} else {
		s.cfg.LogFn("[lk] adapt-track-count: shrinking publisher tracks %d -> %d", current, peerCount)
		for i := current; i > peerCount; i-- {
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
	s.cfg.LogFn("[lk] adapt-track-count: renegotiation offer sent (%d bytes)", len(offer.SDP))
}

// removePublisherTrack stops the trailing transceiver, drops its sub-tunnel
// from the multi-track wrapper, and trims the bookkeeping slices. The SFU
// sees the transceiver go inactive on the next renegotiation and stops
// forwarding the track to subscribers. Slot 0 is preserved.
func (s *Session) removePublisherTrack() bool {
	s.mu.Lock()
	if len(s.sampleTransceivers) <= 1 || len(s.sampleTracks) <= 1 {
		s.mu.Unlock()
		s.cfg.LogFn("[lk] adapt-track-count: refusing to remove cam slot")
		return false
	}
	last := len(s.sampleTransceivers) - 1
	trx := s.sampleTransceivers[last]
	s.sampleTransceivers = s.sampleTransceivers[:last]
	s.sampleTracks = s.sampleTracks[:last]
	vp8 := s.vp8tun
	s.mu.Unlock()
	if vp8 != nil {
		vp8.RemoveLastSubTunnel()
	}
	if err := trx.Stop(); err != nil {
		s.cfg.LogFn("[lk] adapt-track-count: stop transceiver: %v", err)
		return false
	}
	return true
}

func (s *Session) addPublisherTrack(pubPC *webrtc.PeerConnection, slot int) bool {
	labelPrefix := "screenchannel-"
	streamPrefix := "tunnel-screen-"
	source := livekit.TrackSourceScreenShare
	if slot == 0 {
		labelPrefix = "videochannel-"
		streamPrefix = "tunnel-video-"
		source = livekit.TrackSourceCamera
	}
	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000},
		labelPrefix+uuid.New().String(), streamPrefix+uuid.New().String(),
	)
	if err != nil {
		s.cfg.LogFn("[lk] adapt-track-count: new track slot=%d: %v", slot, err)
		return false
	}
	trx, err := pubPC.AddTransceiverFromTrack(track,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly})
	if err != nil {
		s.cfg.LogFn("[lk] adapt-track-count: add transceiver slot=%d: %v", slot, err)
		return false
	}
	if err := s.lk.SendAddTrack(track.ID(), "videochannel",
		livekit.TrackTypeVideo, source, 1280, 720); err != nil {
		s.cfg.LogFn("[lk] adapt-track-count: send add-track slot=%d: %v", slot, err)
		return false
	}
	s.mu.Lock()
	s.sampleTracks = append(s.sampleTracks, track)
	s.sampleTransceivers = append(s.sampleTransceivers, trx)
	vp8 := s.vp8tun
	s.mu.Unlock()
	if vp8 != nil {
		vp8.AddSubTunnel(tunnel.NewVP8DataTunnel(track, s.cfg.Obfuscator, s.cfg.LogFn))
	}
	return true
}

func (s *Session) rearmAutoDetect() {
	if s.cfg.TunnelMode != "" {
		return
	}
	s.mu.Lock()
	s.tunFired = false
	vp8 := s.vp8tun
	dc := s.dctun
	s.mu.Unlock()
	if vp8 != nil {
		vp8.SetOnData(func(payload []byte) { s.activate(vp8, payload) })
	}
	if dc != nil {
		dc.SetOnData(func(payload []byte) { s.activate(dc, payload) })
	}
}

func (s *Session) onRemoteTrack(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
	if track.Codec().MimeType != webrtc.MimeTypeVP8 {
		go func() {
			buf := make([]byte, common.UDPBufSize)
			for {
				if _, _, err := track.Read(buf); err != nil {
					return
				}
			}
		}()
		return
	}
	s.mu.Lock()
	s.remoteTracks++
	count := s.remoteTracks
	s.mu.Unlock()
	if count > 1 {
		s.cfg.LogFn("[wb] new peer track #%d, signalling peer-restart", count)
		s.rearmAutoDetect()
		if s.OnPeerRestart != nil {
			s.OnPeerRestart()
		}
	}
	go s.readVP8Track(track)
}

func (s *Session) readVP8Track(track *webrtc.TrackRemote) {
	var vp8Pkt codecs.VP8Packet
	var frameBuf []byte
	var lastSeq uint16
	var haveLastSeq bool
	frameValid := false
	var recvCount int
	buf := make([]byte, common.RTPBufSize)
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
		if recvCount <= 3 || recvCount%200 == 0 {
			s.cfg.LogFn("[lk-video] recv vp8 frame #%d %d bytes", recvCount, len(frameBuf))
		}

		tun := s.currentVP8Tun()
		if tun != nil {
			tun.HandleFrame(frameBuf)
		}
		frameBuf = frameBuf[:0]
		frameValid = false
	}
}

func (s *Session) onRemoteDataChannel(dc *webrtc.DataChannel) {
	s.cfg.LogFn("[lk] remote DC label=%s id=%v", dc.Label(), dc.ID())
	if dc.Label() != "_reliable" {
		return
	}
	s.mu.Lock()
	s.subReliableDC = dc
	s.mu.Unlock()
	dc.OnOpen(func() {
		s.cfg.LogFn("[lk] remote _reliable DC open")
		s.maybeStartDCTunnel()
	})
}

func (s *Session) onParticipantUpdate(updates []livekit.ParticipantInfo) {
	selfSID := s.lk.Join().ParticipantSID

	s.mu.Lock()
	if s.peersBySID == nil {
		s.peersBySID = make(map[string]peerEntry)
	}
	newcomerSIDs := make(map[string]bool)
	canPromote := s.cfg.AccessToken != "" && s.cfg.RoomID != ""
	for _, p := range updates {
		if p.SID == "" || p.SID == selfSID {
			continue
		}
		if p.State == livekit.ParticipantStateDisconnected {
			delete(s.peersBySID, p.SID)
			delete(s.kickedSIDs, p.SID)
			continue
		}
		if s.kickedSIDs[p.SID] {
			continue
		}
		entry, ok := s.peersBySID[p.SID]
		if !ok {
			entry = peerEntry{sid: p.SID, identity: p.Identity, firstSeen: time.Now()}
			newcomerSIDs[p.SID] = true
		}
		if p.Identity != "" {
			entry.identity = p.Identity
		}
		entry.state = p.State
		s.peersBySID[p.SID] = entry
	}

	var stale []peerEntry
	staleSIDs := make(map[string]bool)
	if len(newcomerSIDs) > 0 {
		for _, e := range s.peersBySID {
			if e.state == livekit.ParticipantStateActive && !newcomerSIDs[e.sid] {
				stale = append(stale, e)
				staleSIDs[e.sid] = true
			}
		}
	}

	var toPromote []peerEntry
	if canPromote {
		for sid, entry := range s.peersBySID {
			if staleSIDs[sid] {
				continue
			}
			if !entry.promoted && entry.state == livekit.ParticipantStateActive && entry.identity != "" {
				entry.promoted = true
				s.peersBySID[sid] = entry
				toPromote = append(toPromote, entry)
			}
		}
	}
	s.mu.Unlock()

	for _, e := range toPromote {
		go s.promotePeer(e.sid, e.identity)
	}

	if len(stale) == 0 {
		return
	}

	for _, e := range stale {
		if e.identity == "" {
			continue
		}
		if err := KickParticipant(http.DefaultClient, s.cfg.AccessToken, s.cfg.RoomID, e.identity); err != nil {
			s.cfg.LogFn("[wb] kick failed identity=%s: %v", e.identity, err)
			continue
		}
		s.cfg.LogFn("[wb] kicked stale peer identity=%s sid=%s", e.identity, e.sid)
		s.mu.Lock()
		delete(s.peersBySID, e.sid)
		if s.kickedSIDs == nil {
			s.kickedSIDs = make(map[string]bool)
		}
		s.kickedSIDs[e.sid] = true
		s.mu.Unlock()
	}
}

func (s *Session) promotePeer(sid, identity string) {
	if err := SetParticipantPermissions(http.DefaultClient, s.cfg.AccessToken, s.cfg.RoomID, identity, ModeratorPermissions); err != nil {
		s.cfg.LogFn("[wb] promote failed identity=%s: %v", identity, err)
		s.mu.Lock()
		if entry, ok := s.peersBySID[sid]; ok {
			entry.promoted = false
			s.peersBySID[sid] = entry
		}
		s.mu.Unlock()
		return
	}
	s.cfg.LogFn("[wb] promoted to moderator identity=%s sid=%s", identity, sid)
}

func (s *Session) Close() {
	if s.lk != nil {
		s.lk.Close()
	}
}
