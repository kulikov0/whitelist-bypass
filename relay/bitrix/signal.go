package bitrix

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
	"whitelist-bypass/relay/livekit"
)

type SignalConfig struct {
	SignalURL         string
	Origin            string
	UserAgent         string
	LogFn             func(string, ...any)
	SettingEngine     *webrtc.SettingEngine
	NetDialContext    func(ctx context.Context, network, addr string) (net.Conn, error)
	OnConnected       func()
	OnDataChannel     func(*webrtc.DataChannel)
	OnTrack           func(*webrtc.TrackRemote, *webrtc.RTPReceiver)
	OnRemoteCandidate func(target int, candidateOrSDP string)
}

type Signal struct {
	lk    *livekit.Client
	logFn func(string, ...any)

	mu             sync.Mutex
	onTrack        func(*webrtc.TrackRemote, *webrtc.RTPReceiver)
	onDC           func(*webrtc.DataChannel)
	pendingTracks  []remoteTrack
	pendingDCs     []*webrtc.DataChannel
	pubReliable    *webrtc.DataChannel
	dataChannelsUp bool
}

type remoteTrack struct {
	track    *webrtc.TrackRemote
	receiver *webrtc.RTPReceiver
}

func ConnectSignal(cfg SignalConfig) (*Signal, error) {
	logFn := cfg.LogFn
	if logFn == nil {
		logFn = func(string, ...any) {}
	}
	s := &Signal{
		logFn:   logFn,
		onTrack: cfg.OnTrack,
		onDC:    cfg.OnDataChannel,
	}
	lk, err := livekit.NewClient(livekit.Config{
		ServerURL:      cfg.SignalURL,
		Origin:         cfg.Origin,
		UserAgent:      cfg.UserAgent,
		Codec:          livekit.JSONCodec{},
		LogFn:          logFn,
		SettingEngine:  cfg.SettingEngine,
		NetDialContext: cfg.NetDialContext,
	})
	if err != nil {
		return nil, err
	}
	s.lk = lk
	s.lk.OnTrack = s.dispatchTrack
	s.lk.OnDataChannel = s.dispatchDataChannel
	s.lk.OnSubConnected = cfg.OnConnected
	if cfg.OnRemoteCandidate != nil {
		s.lk.OnRemoteCandidate = func(target int, ic webrtc.ICECandidateInit) {
			cfg.OnRemoteCandidate(target, ic.Candidate)
		}
		s.lk.OnRemoteSDP = func(target int, _, sdp string) {
			cfg.OnRemoteCandidate(-1, sdp)
		}
	}
	if err := s.lk.Connect(); err != nil {
		return nil, err
	}
	go s.lk.PingLoop()
	return s, nil
}

func (s *Signal) Run() error { return s.lk.ReadLoop() }

func (s *Signal) Close() { s.lk.Close() }

func (s *Signal) SetOnTrack(fn func(*webrtc.TrackRemote, *webrtc.RTPReceiver)) {
	s.mu.Lock()
	s.onTrack = fn
	pending := s.pendingTracks
	s.pendingTracks = nil
	s.mu.Unlock()
	if fn == nil {
		return
	}
	for _, t := range pending {
		fn(t.track, t.receiver)
	}
}

func (s *Signal) SetOnDataChannel(fn func(*webrtc.DataChannel)) {
	s.mu.Lock()
	s.onDC = fn
	pending := s.pendingDCs
	s.pendingDCs = nil
	s.mu.Unlock()
	if fn == nil {
		return
	}
	for _, dc := range pending {
		fn(dc)
	}
}

func (s *Signal) LocalUserID() string { return s.lk.Join().LocalUserID }

func (s *Signal) PubReliableDC() *webrtc.DataChannel {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pubReliable
}

func (s *Signal) PubPC() *webrtc.PeerConnection { return s.lk.PubPC() }

func (s *Signal) Joined() <-chan struct{} { return s.lk.Joined() }

func (s *Signal) WaitJoined(timeout time.Duration) error {
	select {
	case <-s.lk.Joined():
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("join did not arrive")
	}
}

func (s *Signal) AddPublisherTrack(track *webrtc.TrackLocalStaticSample, source int) (*webrtc.RTPTransceiver, error) {
	pubPC := s.lk.PubPC()
	if pubPC == nil {
		return nil, fmt.Errorf("pub pc not ready")
	}
	if err := s.ensureDataChannels(pubPC); err != nil {
		return nil, err
	}
	transceiver, err := pubPC.AddTransceiverFromTrack(track, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionSendonly,
	})
	if err != nil {
		return nil, err
	}
	if err := s.lk.SendAddTrack(track.ID(), "tunnel", livekit.TrackTypeVideo, source, 1280, 720); err != nil {
		return nil, err
	}
	s.logFn("[bx] published vp8 track cid=%s source=%d", track.ID(), source)
	return transceiver, nil
}

func (s *Signal) Renegotiate() error {
	pubPC := s.lk.PubPC()
	if pubPC == nil {
		return fmt.Errorf("pub pc not ready")
	}
	offer, err := pubPC.CreateOffer(nil)
	if err != nil {
		return err
	}
	if err := pubPC.SetLocalDescription(offer); err != nil {
		return err
	}
	return s.lk.SendOffer(offer.SDP)
}

func (s *Signal) dispatchTrack(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
	s.mu.Lock()
	fn := s.onTrack
	if fn == nil {
		s.pendingTracks = append(s.pendingTracks, remoteTrack{track: track, receiver: receiver})
	}
	s.mu.Unlock()
	if fn != nil {
		fn(track, receiver)
	}
}

func (s *Signal) dispatchDataChannel(dc *webrtc.DataChannel) {
	s.mu.Lock()
	fn := s.onDC
	if fn == nil {
		s.pendingDCs = append(s.pendingDCs, dc)
	}
	s.mu.Unlock()
	if fn != nil {
		fn(dc)
	}
}

func (s *Signal) ensureDataChannels(pubPC *webrtc.PeerConnection) error {
	s.mu.Lock()
	if s.dataChannelsUp {
		s.mu.Unlock()
		return nil
	}
	s.dataChannelsUp = true
	s.mu.Unlock()

	ordered := true
	reliable, err := pubPC.CreateDataChannel("_reliable", &webrtc.DataChannelInit{Ordered: &ordered})
	if err != nil {
		return fmt.Errorf("create _reliable: %w", err)
	}
	unordered := false
	var zero uint16
	lossy, err := pubPC.CreateDataChannel("_lossy", &webrtc.DataChannelInit{Ordered: &unordered, MaxRetransmits: &zero})
	if err != nil {
		return fmt.Errorf("create _lossy: %w", err)
	}
	reliable.OnOpen(func() { s.logFn("[bx] pub dc _reliable open") })
	lossy.OnOpen(func() { s.logFn("[bx] pub dc _lossy open") })

	s.mu.Lock()
	s.pubReliable = reliable
	s.mu.Unlock()
	return nil
}
