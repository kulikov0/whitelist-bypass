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
}

const (
	TunnelModeVideo = "video"
	TunnelModeDC    = "dc"
)

type SessionConfig struct {
	RoomToken      string
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
}

type Session struct {
	cfg SessionConfig

	lk          *livekit.Client
	sampleTrack *webrtc.TrackLocalStaticSample

	pubReliableDC      *webrtc.DataChannel
	pubReliableDCReady bool
	subReliableDC      *webrtc.DataChannel

	vp8tun       *tunnel.VP8DataTunnel
	dctun        *tunnel.DCTunnel
	mu           sync.Mutex
	tunFired     bool
	remoteTracks int
	done         chan struct{}

	peersBySID map[string]peerEntry // SID -> first-seen time + state

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
	return &Session{cfg: cfg, done: make(chan struct{})}
}

func (s *Session) Done() <-chan struct{} { return s.done }

func (s *Session) Start() error {
	s.lk = livekit.NewClient(livekit.Config{
		ServerURL:      WSURL,
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
		close(s.done)
	}()
	return nil
}

func (s *Session) onLKReady() {
	pubPC := s.lk.PubPC()
	if pubPC == nil {
		return
	}

	trackID := "videochannel-" + uuid.New().String()
	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000},
		trackID, "tunnel-video-"+uuid.New().String(),
	)
	if err != nil {
		s.cfg.LogFn("[lk] create local track: %v", err)
		return
	}
	s.mu.Lock()
	s.sampleTrack = track
	s.mu.Unlock()

	if _, err := pubPC.AddTransceiverFromTrack(track,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly}); err != nil {
		s.cfg.LogFn("[lk] add transceiver: %v", err)
		return
	}

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

	if err := s.lk.SendAddTrack(track.ID(), "videochannel",
		livekit.TrackTypeVideo, livekit.TrackSourceCamera, 1280, 720); err != nil {
		s.cfg.LogFn("[lk] send add-track: %v", err)
		return
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
	if s.vp8tun != nil || s.sampleTrack == nil {
		s.mu.Unlock()
		return
	}
	s.vp8tun = tunnel.NewVP8DataTunnel(s.sampleTrack, s.cfg.Obfuscator, s.cfg.LogFn)
	s.vp8tun.Start(s.cfg.VP8FPS, s.cfg.VP8Batch)
	s.mu.Unlock()
	s.cfg.LogFn("[lk] vp8 tunnel writer started")

	if s.cfg.TunnelMode == TunnelModeVideo {
		s.fireOnConnected(s.vp8tun)
		return
	}
	if s.cfg.TunnelMode == "" {
		s.vp8tun.SetOnData(func(payload []byte) { s.activate(s.vp8tun, payload) })
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
	s.cfg.LogFn("[lk] auto-detected active tunnel: %T", tun)
	if s.OnConnected != nil {
		s.OnConnected(tun)
	}
	var fwd func([]byte)
	switch v := tun.(type) {
	case *tunnel.VP8DataTunnel:
		fwd = v.OnData
	case *tunnel.DCTunnel:
		fwd = v.OnData()
	}
	if fwd != nil {
		fwd(payload)
	}
}

func (s *Session) currentVP8Tun() *tunnel.VP8DataTunnel {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.vp8tun
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
	for _, p := range updates {
		if p.SID == "" || p.SID == selfSID {
			continue
		}
		if p.State == livekit.ParticipantStateDisconnected {
			delete(s.peersBySID, p.SID)
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
	if len(newcomerSIDs) > 0 {
		for _, e := range s.peersBySID {
			if e.state == livekit.ParticipantStateActive && !newcomerSIDs[e.sid] {
				stale = append(stale, e)
			}
		}
	}
	s.mu.Unlock()
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
		s.mu.Unlock()
	}
}

func (s *Session) Close() {
	if s.lk != nil {
		s.lk.Close()
	}
}
