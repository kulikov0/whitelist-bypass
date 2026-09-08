package bitrix

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/kulikov0/headless-client/webrtc"
	"whitelist-bypass/relay/livekit"
)

type SignalConfig struct {
	SignalURL              string
	Origin                 string
	UserAgent              string
	LogFn                  func(string, ...any)
	ConfigureSettingEngine func(*webrtc.SettingEngine)
	NetDialContext         func(ctx context.Context, network, addr string) (net.Conn, error)
	OnConnected            func()
	OnDataChannel          func(*webrtc.DataChannel)
	OnTrack                func(*webrtc.TrackRemote, *webrtc.RTPReceiver)
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
		ServerURL:              cfg.SignalURL,
		Origin:                 cfg.Origin,
		UserAgent:              cfg.UserAgent,
		Codec:                  livekit.JSONCodec{},
		LogFn:                  logFn,
		ConfigureSettingEngine: cfg.ConfigureSettingEngine,
		NetDialContext:         cfg.NetDialContext,
	})
	if err != nil {
		return nil, err
	}
	s.lk = lk
	s.lk.OnTrack = s.dispatchTrack
	s.lk.OnDataChannel = s.dispatchDataChannel
	s.lk.OnSubConnected = cfg.OnConnected
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

func (s *Signal) PubReliableDC() *webrtc.DataChannel {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pubReliable
}

func (s *Signal) PublishVP8(track *webrtc.TrackLocalStaticSample, name string) (*webrtc.RTPSender, error) {
	select {
	case <-s.lk.Joined():
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("join did not arrive")
	}
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
	if err := s.lk.SendAddTrack(track.ID(), name, livekit.TrackTypeVideo, livekit.TrackSourceCamera, 1280, 720); err != nil {
		return nil, err
	}
	s.logFn("[bx] published vp8 track cid=%s name=%s", track.ID(), name)
	offer, err := pubPC.CreateOffer(nil)
	if err != nil {
		return nil, err
	}
	if err := pubPC.SetLocalDescription(offer); err != nil {
		return nil, err
	}
	if err := s.lk.SendOffer(offer.SDP); err != nil {
		return nil, err
	}
	return transceiver.Sender(), nil
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
