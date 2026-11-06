package livekit

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	headless "github.com/kulikov0/headless-client"
	"github.com/kulikov0/headless-client/webrtc"
	"github.com/kulikov0/headless-client/websocket"
	"whitelist-bypass/relay/common"
	"whitelist-bypass/relay/headlessapi"
)

const (
	ProtocolVersion = "15"
	SDKName         = "js"
	SDKVersion      = "2.7.0"
	PingPeriod      = 5 * time.Second

	SinglePCProtocolVersion = 17
	SinglePCSDKVersion      = "2.22.0"
	SinglePCClientProtocol  = 2

	TargetPublisher  = signalTargetPublisher
	TargetSubscriber = signalTargetSubscriber

	TrackTypeAudio         = trackTypeAudio
	TrackTypeVideo         = trackTypeVideo
	TrackTypeData          = trackTypeData
	TrackSourceCamera      = trackSourceCamera
	TrackSourceScreenShare = trackSourceScreenShare

	initialAudioSections  = 3
	initialVideoSections  = 3
	resumeResponseTimeout = 5 * time.Second
)

var (
	errResumeRefused = errors.New("livekit: server refused resume")
	resumeDelays     = []time.Duration{
		0,
		300 * time.Millisecond,
		1200 * time.Millisecond,
		2700 * time.Millisecond,
		4800 * time.Millisecond,
		7 * time.Second,
		7 * time.Second,
		7 * time.Second,
		7 * time.Second,
		7 * time.Second,
	}
)

type ICEServer = iceServer
type JoinResponse = joinResponse
type MediaSections = mediaSections

type Config struct {
	ServerURL              string
	Token                  string
	Origin                 string
	UserAgent              string
	Codec                  Codec
	LogFn                  func(string, ...any)
	ConfigureSettingEngine func(*webrtc.SettingEngine)
	NetDialContext         func(ctx context.Context, network, addr string) (net.Conn, error)
	ResolveICEHost         func(host string) (string, error)
	SinglePeerConnection   bool
}

type Client struct {
	logFn func(string, ...any)

	wsURL  string
	token  string
	origin string
	ua     string
	codec  Codec

	configureSettingEngine func(*webrtc.SettingEngine)
	netDialContext         func(ctx context.Context, network, addr string) (net.Conn, error)
	resolveICEHost         func(host string) (string, error)

	ws   *websocket.Conn
	wsMu sync.Mutex

	join JoinResponse

	pubPC        *webrtc.PeerConnection
	subPC        *webrtc.PeerConnection
	pubMu        sync.Mutex
	subMu        sync.Mutex
	pubRemoteSet bool
	subRemoteSet bool
	pubPending   []webrtc.ICECandidateInit
	subPending   []webrtc.ICECandidateInit

	singlePC            bool
	reliableDC          *webrtc.DataChannel
	pendingInitialOffer *webrtc.SessionDescription
	renegotiate         bool
	offerID             atomic.Uint32
	leaveReceived       atomic.Bool

	joined     chan struct{}
	joinedOnce sync.Once
	closed     atomic.Bool

	OnReady             func()
	OnTrack             func(*webrtc.TrackRemote, *webrtc.RTPReceiver)
	OnDataChannel       func(*webrtc.DataChannel)
	OnPubConnected      func()
	OnSubConnected      func()
	OnParticipantUpdate func([]ParticipantInfo)
	// OnRemoteCandidate fires once for every trickle ICE candidate
	// arriving from the SFU. Used by the Windows joiner to install
	// /32 bypass routes so the joiner's own media flows stay on the
	// physical NIC instead of recursing through the tunnel.
	OnRemoteCandidate func(target int, candidate webrtc.ICECandidateInit)
	// OnRemoteSDP fires when the SFU sends an SDP offer or answer.
	// The SDP itself can carry candidates inline; the Windows joiner
	// uses this to bypass them before any media starts flowing.
	OnRemoteSDP func(target int, sdpType, sdp string)
}

func NewClient(cfg Config) (*Client, error) {
	if cfg.Codec == nil {
		return nil, fmt.Errorf("livekit: no codec")
	}
	logFn := cfg.LogFn
	if logFn == nil {
		logFn = func(string, ...any) {}
	}
	return &Client{
		logFn:                  logFn,
		wsURL:                  cfg.ServerURL,
		token:                  cfg.Token,
		origin:                 cfg.Origin,
		ua:                     cfg.UserAgent,
		codec:                  cfg.Codec,
		configureSettingEngine: cfg.ConfigureSettingEngine,
		netDialContext:         cfg.NetDialContext,
		resolveICEHost:         cfg.ResolveICEHost,
		singlePC:               cfg.SinglePeerConnection,
		joined:                 make(chan struct{}),
	}, nil
}

func (c *Client) Join() JoinResponse            { return c.join }
func (c *Client) PubPC() *webrtc.PeerConnection { return c.pubPC }
func (c *Client) SubPC() *webrtc.PeerConnection { return c.subPC }
func (c *Client) Joined() <-chan struct{}       { return c.joined }

func (c *Client) ReliableDataChannel() *webrtc.DataChannel { return c.reliableDC }

func (c *Client) Connect() error {
	target, err := c.dialTarget()
	if err != nil {
		return err
	}
	conn, err := c.dial(target)
	if err != nil {
		return err
	}
	c.wsMu.Lock()
	c.ws = conn
	c.wsMu.Unlock()
	c.logFn("[lk] signaling connected")
	return nil
}

func (c *Client) dialTarget() (string, error) {
	if !c.singlePC {
		return c.codec.DialURL(c.wsURL, c.token)
	}
	offer, err := c.buildSinglePeerConnection()
	if err != nil {
		return "", err
	}
	request := encJoinRequest(joinRequest{
		PublisherOffer: &sessionDescription{Type: "offer", SDP: offer.SDP, ID: c.nextOfferID()},
	})
	return c.codec.SinglePCDialURL(c.wsURL, c.token, request)
}

func (c *Client) dial(target string) (*websocket.Conn, error) {
	headers := headless.ChromeWindows.Headers(headless.DestWebSocket)
	if c.ua != "" {
		headers.Set("User-Agent", c.ua)
	}
	if c.origin != "" {
		headers.Set("Origin", c.origin)
	}

	dialer := headless.ChromeWindows.WebSocketDialer(headless.TLSOptions{DialContext: c.netDialContext})
	if c.netDialContext != nil {
		dialer.NetDialContext = c.netDialContext
	}
	conn, resp, err := dialer.Dial(target, headers)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("ws dial: %w, status %d", err, resp.StatusCode)
		}
		return nil, fmt.Errorf("ws dial: %w", err)
	}
	return conn, nil
}

func (c *Client) sendSignal(payload []byte) error {
	if payload == nil {
		return nil
	}
	c.wsMu.Lock()
	defer c.wsMu.Unlock()
	if c.ws == nil {
		return fmt.Errorf("ws not connected")
	}
	return c.ws.WriteMessage(c.codec.MessageType(), payload)
}

func (c *Client) SendOffer(sdp string) error {
	return c.sendSignal(c.codec.EncodeOffer(sdp, c.nextOfferID()))
}

func (c *Client) Negotiate() error {
	c.pubMu.Lock()
	defer c.pubMu.Unlock()
	return c.negotiateLocked()
}

func (c *Client) SendAnswer(sdp string) error {
	return c.sendSignal(c.codec.EncodeAnswer(sdp))
}

func (c *Client) SendTrickle(candidate webrtc.ICECandidateInit, target int) error {
	return c.sendSignal(c.codec.EncodeTrickle(candidate, target))
}

func (c *Client) SendAddTrack(cid, name string, trackType, source int, width, height uint32) error {
	return c.sendSignal(c.codec.EncodeAddTrack(AddTrackParams{
		CID:    cid,
		Name:   name,
		Type:   trackType,
		Source: source,
		Width:  width,
		Height: height,
	}))
}

func (c *Client) SendLeave() error { return c.sendSignal(c.codec.EncodeLeave()) }

func (c *Client) SendPing() error {
	return c.sendSignal(c.codec.EncodePing(time.Now().UnixMilli()))
}

func (c *Client) Close() {
	if !c.closed.CompareAndSwap(false, true) {
		return
	}
	c.wsMu.Lock()
	ws := c.ws
	c.ws = nil
	c.wsMu.Unlock()
	common.CloseWS(ws)
	c.pubMu.Lock()
	pubPC := c.pubPC
	c.pubMu.Unlock()
	c.subMu.Lock()
	subPC := c.subPC
	c.subMu.Unlock()
	if pubPC != nil {
		_ = pubPC.Close()
	}
	if subPC != nil {
		_ = subPC.Close()
	}
}

func (c *Client) nextOfferID() uint32 {
	if !c.singlePC {
		return 0
	}
	return c.offerID.Add(1)
}

func (c *Client) negotiateLocked() error {
	if c.pubPC == nil {
		return fmt.Errorf("publisher not created")
	}
	if c.pendingInitialOffer != nil || c.pubPC.SignalingState() == webrtc.SignalingStateHaveLocalOffer {
		c.renegotiate = true
		return nil
	}
	offer, err := c.pubPC.CreateOffer(nil)
	if err != nil {
		return fmt.Errorf("create offer: %w", err)
	}
	if err := c.pubPC.SetLocalDescription(offer); err != nil {
		return fmt.Errorf("set local offer: %w", err)
	}
	if err := c.SendOffer(offer.SDP); err != nil {
		return fmt.Errorf("send offer: %w", err)
	}
	c.logFn("[lk] sent publisher offer, %d bytes", len(offer.SDP))
	return nil
}

func (c *Client) iceServersAsWebRTC(servers []ICEServer) []webrtc.ICEServer {
	out := make([]webrtc.ICEServer, 0, len(servers))
	for _, s := range servers {
		urls := common.ResolveICEHosts(s.URLs, c.resolveICEHost, c.logFn, "[lk]")
		ice := webrtc.ICEServer{URLs: urls}
		if s.Username != "" {
			ice.Username = s.Username
			ice.Credential = s.Credential
		}
		out = append(out, ice)
	}
	return out
}

func (c *Client) webRTCAPI() (*webrtc.API, error) {
	api, err := headlessapi.WebRTCAPI(headlessapi.Options{
		Profile: headless.ChromeWindows,
		Configure: func(settingEngine *webrtc.SettingEngine) {
			if c.configureSettingEngine != nil {
				c.configureSettingEngine(settingEngine)
			}
			settingEngine.DetachDataChannels()
		},
	})
	if err != nil {
		return nil, fmt.Errorf("build webrtc api: %w", err)
	}
	return api, nil
}

func (c *Client) watchPublisher(pubPC *webrtc.PeerConnection) {
	pubPC.OnICECandidate(func(cand *webrtc.ICECandidate) {
		if cand == nil {
			c.logFn("[lk] pub ICE gathering complete")
			return
		}
		c.logFn("[lk] pub local cand: %s", cand.String())
		_ = c.SendTrickle(cand.ToJSON(), TargetPublisher)
	})
	pubPC.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		c.logFn("[lk] pub PC state: %s", state.String())
		if state == webrtc.PeerConnectionStateConnected && c.OnPubConnected != nil {
			c.OnPubConnected()
		}
	})
	pubPC.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		c.logFn("[lk] pub ICE state: %s", state.String())
	})
}

func (c *Client) buildPeerConnections() error {
	cfg := webrtc.Configuration{ICEServers: c.iceServersAsWebRTC(c.join.ICEServers)}

	api, err := c.webRTCAPI()
	if err != nil {
		return err
	}

	pubPC, err := api.NewPeerConnection(cfg)
	if err != nil {
		return fmt.Errorf("create pub pc: %w", err)
	}
	subPC, err := api.NewPeerConnection(cfg)
	if err != nil {
		_ = pubPC.Close()
		return fmt.Errorf("create sub pc: %w", err)
	}

	c.pubMu.Lock()
	c.pubPC = pubPC
	c.pubMu.Unlock()
	c.subMu.Lock()
	c.subPC = subPC
	c.subMu.Unlock()

	c.watchPublisher(pubPC)
	subPC.OnICECandidate(func(cand *webrtc.ICECandidate) {
		if cand == nil {
			c.logFn("[lk] sub ICE gathering complete")
			return
		}
		c.logFn("[lk] sub local cand: %s", cand.String())
		_ = c.SendTrickle(cand.ToJSON(), TargetSubscriber)
	})
	subPC.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		c.logFn("[lk] sub PC state: %s", state.String())
		if state == webrtc.PeerConnectionStateConnected && c.OnSubConnected != nil {
			c.OnSubConnected()
		}
	})
	subPC.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		c.logFn("[lk] sub ICE state: %s", state.String())
	})

	subPC.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		c.logFn("[lk] sub remote track: %s", track.Codec().MimeType)
		if c.OnTrack != nil {
			c.OnTrack(track, receiver)
		}
	})
	subPC.OnDataChannel(func(dc *webrtc.DataChannel) {
		c.logFn("[lk] sub data channel: %s", dc.Label())
		if c.OnDataChannel != nil {
			c.OnDataChannel(dc)
		}
	})

	c.logFn("[lk] PCs created, %d ICE servers", len(c.join.ICEServers))
	for i, s := range c.join.ICEServers {
		c.logFn("[lk] iceServer[%d]: urls=%v hasCred=%v", i, s.URLs, s.Username != "")
	}
	return nil
}

func (c *Client) buildSinglePeerConnection() (webrtc.SessionDescription, error) {
	api, err := c.webRTCAPI()
	if err != nil {
		return webrtc.SessionDescription{}, err
	}
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return webrtc.SessionDescription{}, fmt.Errorf("create pc: %w", err)
	}

	c.watchPublisher(pc)
	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		c.logFn("[lk] remote track: %s", track.Codec().MimeType)
		if c.OnTrack != nil {
			c.OnTrack(track, receiver)
		}
	})
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		c.logFn("[lk] remote data channel: %s", dc.Label())
		if c.OnDataChannel != nil {
			c.OnDataChannel(dc)
		}
	})

	ordered := true
	unordered := false
	var noRetransmits uint16
	lossyChannel, err := pc.CreateDataChannel("_lossy", &webrtc.DataChannelInit{
		Ordered:        &unordered,
		MaxRetransmits: &noRetransmits,
	})
	if err != nil {
		_ = pc.Close()
		return webrtc.SessionDescription{}, fmt.Errorf("create lossy DC: %w", err)
	}
	reliableChannel, err := pc.CreateDataChannel("_reliable", &webrtc.DataChannelInit{
		Ordered: &ordered,
	})
	if err != nil {
		_ = pc.Close()
		return webrtc.SessionDescription{}, fmt.Errorf("create reliable DC: %w", err)
	}
	dataTrackChannel, err := pc.CreateDataChannel("_data_track", &webrtc.DataChannelInit{
		Ordered:        &unordered,
		MaxRetransmits: &noRetransmits,
	})
	if err != nil {
		_ = pc.Close()
		return webrtc.SessionDescription{}, fmt.Errorf("create data track DC: %w", err)
	}
	for _, channel := range []*webrtc.DataChannel{lossyChannel, dataTrackChannel} {
		channel.OnOpen(func() { go c.drainDataChannel(channel) })
	}

	if err := addRecvSections(pc, initialAudioSections, initialVideoSections); err != nil {
		_ = pc.Close()
		return webrtc.SessionDescription{}, err
	}
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		_ = pc.Close()
		return webrtc.SessionDescription{}, fmt.Errorf("create initial offer: %w", err)
	}

	c.pubMu.Lock()
	c.pubPC = pc
	c.reliableDC = reliableChannel
	c.pendingInitialOffer = &offer
	c.pubMu.Unlock()
	return offer, nil
}

func (c *Client) drainDataChannel(channel *webrtc.DataChannel) {
	rawChannel, err := channel.Detach()
	if err != nil {
		c.logFn("[lk] detach %s: %v", channel.Label(), err)
		return
	}
	buf := make([]byte, common.DCBufSize)
	for {
		if _, err := rawChannel.Read(buf); err != nil {
			return
		}
	}
}

func (c *Client) applyICEServers(servers []ICEServer) {
	resolved := c.iceServersAsWebRTC(servers)
	c.pubMu.Lock()
	defer c.pubMu.Unlock()
	if c.pubPC == nil {
		return
	}
	configuration := c.pubPC.GetConfiguration()
	configuration.ICEServers = resolved
	if err := c.pubPC.SetConfiguration(configuration); err != nil {
		c.logFn("[lk] set ICE servers: %v", err)
		return
	}
	for i, s := range servers {
		c.logFn("[lk] iceServer[%d]: urls=%v hasCred=%v", i, s.URLs, s.Username != "")
	}
}

func (c *Client) addMediaSections(audios, videos int) {
	c.pubMu.Lock()
	defer c.pubMu.Unlock()
	if c.pubPC == nil {
		return
	}
	if err := addRecvSections(c.pubPC, audios, videos); err != nil {
		c.logFn("[lk] %v", err)
		return
	}
	if err := c.negotiateLocked(); err != nil {
		c.logFn("[lk] negotiate media sections: %v", err)
	}
}

func addRecvSections(pc *webrtc.PeerConnection, audios, videos int) error {
	recvOnly := webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}
	for range audios {
		if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, recvOnly); err != nil {
			return fmt.Errorf("add audio section: %w", err)
		}
	}
	for range videos {
		if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, recvOnly); err != nil {
			return fmt.Errorf("add video section: %w", err)
		}
	}
	return nil
}

func (c *Client) handleSignal(data []byte) {
	ev, err := c.codec.Decode(data)
	if err != nil {
		c.logFn("[lk] decode signal: %v", err)
		return
	}
	switch ev.Kind {
	case EventJoin:
		c.join = *ev.Join
		c.logFn("[lk] join: room=%s participant=%s subscriberPrimary=%v iceServers=%d pingTimeout=%ds pingInterval=%ds",
			c.join.RoomName, c.join.ParticipantID, c.join.SubscriberPrimary, len(c.join.ICEServers),
			c.join.PingTimeoutSec, c.join.PingIntervalSec)
		if c.singlePC {
			c.applyICEServers(c.join.ICEServers)
			c.joinedOnce.Do(func() { close(c.joined) })
			return
		}
		if err := c.buildPeerConnections(); err != nil {
			c.logFn("[lk] %v", err)
			return
		}
		c.joinedOnce.Do(func() { close(c.joined) })
		if c.OnReady != nil {
			c.OnReady()
		}
	case EventAnswer:
		c.logFn("[lk] pub answer received, %d bytes", len(ev.SDP))
		c.applyPubAnswer(ev.SDP)
	case EventOffer:
		if c.singlePC {
			c.logFn("[lk] ignored offer, %d bytes", len(ev.SDP))
			return
		}
		c.logFn("[lk] sub offer received, %d bytes", len(ev.SDP))
		c.applySubOfferAndAnswer(ev.SDP)
	case EventTrickle:
		c.logFn("[lk] trickle target=%d", ev.Trickle.Target)
		c.applyRemoteTrickle(*ev.Trickle)
	case EventToken:
		c.token = ev.Token
		c.logFn("[lk] token refreshed")
	case EventReconnect:
		c.applyICEServers(ev.ICEServers)
	case EventMediaSections:
		c.logFn("[lk] media sections requested audio=%d video=%d", ev.MediaSections.Audios, ev.MediaSections.Videos)
		c.addMediaSections(ev.MediaSections.Audios, ev.MediaSections.Videos)
	case EventLeave:
		if ev.Leave == nil || ev.Leave.Action != leaveActionResume {
			c.leaveReceived.Store(true)
		}
		if ev.Leave != nil {
			c.logFn("[lk] ignored leave reason=%s action=%s",
				DisconnectReasonName(ev.Leave.Reason), LeaveActionName(ev.Leave.Action))
		} else {
			c.logFn("[lk] ignored leave")
		}
	case EventUpdate:
		if c.OnParticipantUpdate != nil {
			c.OnParticipantUpdate(ev.Participants)
		}
	default:
		if common.Debug {
			c.logFn("[lk] unhandled signal, %d bytes", len(data))
		}
	}
}

func (c *Client) applyPubAnswer(sdp string) {
	if c.OnRemoteSDP != nil {
		c.OnRemoteSDP(TargetPublisher, "answer", sdp)
	}
	c.pubMu.Lock()
	if c.pubPC == nil {
		c.pubMu.Unlock()
		return
	}
	initialOffer := c.pendingInitialOffer
	c.pendingInitialOffer = nil
	if initialOffer != nil {
		if err := c.pubPC.SetLocalDescription(*initialOffer); err != nil {
			c.pubMu.Unlock()
			c.logFn("[lk] set initial local offer: %v", err)
			return
		}
	}
	if err := c.pubPC.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: sdp}); err != nil {
		c.pubMu.Unlock()
		c.logFn("[lk] set pub remote answer: %v", err)
		return
	}
	c.pubRemoteSet = true
	c.drainPendingLocked(c.pubPC, &c.pubPending)
	renegotiate := c.renegotiate
	c.renegotiate = false
	c.pubMu.Unlock()

	if initialOffer != nil && c.OnReady != nil {
		c.OnReady()
	}
	if renegotiate {
		if err := c.Negotiate(); err != nil {
			c.logFn("[lk] renegotiate: %v", err)
		}
	}
}

func (c *Client) applySubOfferAndAnswer(sdp string) {
	if c.OnRemoteSDP != nil {
		c.OnRemoteSDP(TargetSubscriber, "offer", sdp)
	}
	c.subMu.Lock()
	if c.subPC == nil {
		c.subMu.Unlock()
		return
	}
	subPC := c.subPC
	if err := subPC.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: sdp}); err != nil {
		c.subMu.Unlock()
		c.logFn("[lk] set sub remote offer: %v", err)
		return
	}
	c.subRemoteSet = true
	c.drainPendingLocked(subPC, &c.subPending)
	c.subMu.Unlock()

	answer, err := subPC.CreateAnswer(nil)
	if err != nil {
		c.logFn("[lk] create sub answer: %v", err)
		return
	}
	if err := subPC.SetLocalDescription(answer); err != nil {
		c.logFn("[lk] set sub local answer: %v", err)
		return
	}
	if err := c.SendAnswer(answer.SDP); err != nil {
		c.logFn("[lk] send answer: %v", err)
	}
}

func (c *Client) drainPendingLocked(pc *webrtc.PeerConnection, pending *[]webrtc.ICECandidateInit) {
	for _, ic := range *pending {
		if err := pc.AddICECandidate(ic); err != nil {
			c.logFn("[lk] add pending candidate: %v", err)
		}
	}
	*pending = nil
}

func (c *Client) applyRemoteTrickle(m TrickleEvent) {
	if c.OnRemoteCandidate != nil {
		c.OnRemoteCandidate(m.Target, m.Candidate)
	}
	mu := &c.subMu
	pc := &c.subPC
	ready := &c.subRemoteSet
	pending := &c.subPending
	if m.Target == TargetPublisher || c.singlePC {
		mu = &c.pubMu
		pc = &c.pubPC
		ready = &c.pubRemoteSet
		pending = &c.pubPending
	}
	mu.Lock()
	defer mu.Unlock()
	if *pc == nil || !*ready {
		*pending = append(*pending, m.Candidate)
		return
	}
	if err := (*pc).AddICECandidate(m.Candidate); err != nil {
		c.logFn("[lk] add candidate: %v", err)
	}
}

func (c *Client) ReadLoop() error {
	defer c.Close()
	for {
		err := c.readSignals()
		if c.closed.Load() {
			return nil
		}
		if !c.singlePC || c.leaveReceived.Load() {
			return err
		}
		c.logFn("[lk] signal lost: %v, resuming", err)
		if resumeErr := c.resume(); resumeErr != nil {
			c.logFn("[lk] resume failed: %v", resumeErr)
			return err
		}
	}
}

func (c *Client) readSignals() error {
	c.wsMu.Lock()
	ws := c.ws
	c.wsMu.Unlock()
	if ws == nil {
		return fmt.Errorf("ws not connected")
	}
	for {
		mt, data, err := ws.ReadMessage()
		if err != nil {
			return err
		}
		if !c.codec.Accepts(mt) {
			continue
		}
		c.handleSignal(data)
	}
}

func (c *Client) resume() error {
	var lastErr error
	for attempt, delay := range resumeDelays {
		time.Sleep(delay)
		if c.closed.Load() {
			return fmt.Errorf("client closed")
		}
		err := c.resumeSignal()
		if err == nil {
			c.logFn("[lk] signal resumed, attempt %d", attempt+1)
			return c.resumePublisher()
		}
		if errors.Is(err, errResumeRefused) {
			return err
		}
		c.logFn("[lk] resume attempt %d: %v", attempt+1, err)
		lastErr = err
	}
	return lastErr
}

func (c *Client) resumeSignal() error {
	c.wsMu.Lock()
	previous := c.ws
	c.ws = nil
	c.wsMu.Unlock()
	common.CloseWS(previous)

	request := encJoinRequest(joinRequest{Reconnect: true, ParticipantSID: c.join.ParticipantSID})
	target, err := c.codec.SinglePCDialURL(c.wsURL, c.token, request)
	if err != nil {
		return err
	}
	conn, err := c.dial(target)
	if err != nil {
		return err
	}
	if err := conn.SetReadDeadline(time.Now().Add(resumeResponseTimeout)); err != nil {
		common.CloseWS(conn)
		return err
	}
	_, data, err := conn.ReadMessage()
	if err != nil {
		common.CloseWS(conn)
		return fmt.Errorf("read first message: %w", err)
	}
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		common.CloseWS(conn)
		return err
	}
	first, err := c.codec.Decode(data)
	if err != nil {
		common.CloseWS(conn)
		return err
	}
	if first.Kind == EventLeave {
		common.CloseWS(conn)
		if first.Leave != nil {
			return fmt.Errorf("%w: reason=%s", errResumeRefused, DisconnectReasonName(first.Leave.Reason))
		}
		return errResumeRefused
	}

	c.wsMu.Lock()
	if c.closed.Load() {
		c.wsMu.Unlock()
		common.CloseWS(conn)
		return fmt.Errorf("client closed")
	}
	c.ws = conn
	c.wsMu.Unlock()
	c.handleSignal(data)
	return nil
}

func (c *Client) resumePublisher() error {
	c.pubMu.Lock()
	defer c.pubMu.Unlock()
	if c.pubPC == nil || c.pendingInitialOffer != nil {
		return fmt.Errorf("publisher was never negotiated")
	}
	offerPending := c.pubPC.SignalingState() == webrtc.SignalingStateHaveLocalOffer
	if !offerPending && !c.renegotiate {
		return nil
	}
	if offerPending {
		if err := c.pubPC.SetLocalDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeRollback}); err != nil {
			return fmt.Errorf("roll back pending offer: %w", err)
		}
	}
	c.renegotiate = false
	return c.negotiateLocked()
}

func (c *Client) PingLoop() {
	select {
	case <-c.joined:
	case <-time.After(30 * time.Second):
		return
	}
	period := PingPeriod
	if c.join.PingIntervalSec > 0 {
		period = time.Duration(c.join.PingIntervalSec) * time.Second
	}
	t := time.NewTicker(period)
	defer t.Stop()
	var sentN int
	for range t.C {
		if c.closed.Load() {
			return
		}
		if err := c.SendPing(); err != nil {
			c.logFn("[lk] ping send failed: %v", err)
			if !c.singlePC {
				return
			}
			continue
		}
		sentN++
		if common.Debug && (sentN <= 3 || sentN%12 == 0) {
			c.logFn("[lk] ping #%d sent", sentN)
		}
	}
}
