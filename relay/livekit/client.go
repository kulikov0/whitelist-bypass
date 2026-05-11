package livekit

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
	"whitelist-bypass/relay/common"
)

const (
	ProtocolVersion = "15"
	SDKName         = "js"
	SDKVersion      = "2.7.0"
	PingPeriod      = 5 * time.Second

	TargetPublisher  = signalTargetPublisher
	TargetSubscriber = signalTargetSubscriber

	TrackTypeAudio    = trackTypeAudio
	TrackTypeVideo    = trackTypeVideo
	TrackTypeData     = trackTypeData
	TrackSourceCamera = trackSourceCamera
)

type ICEServer = iceServer
type JoinResponse = joinResponse

type Config struct {
	ServerURL      string
	Token          string
	Origin         string
	UserAgent      string
	LogFn          func(string, ...any)
	SettingEngine  *webrtc.SettingEngine
	NetDialContext func(ctx context.Context, network, addr string) (net.Conn, error)
	ResolveICEHost func(host string) (string, error)
}

type Client struct {
	logFn func(string, ...any)

	wsURL  string
	token  string
	origin string
	ua     string

	settingEngine  *webrtc.SettingEngine
	netDialContext func(ctx context.Context, network, addr string) (net.Conn, error)
	resolveICEHost func(host string) (string, error)

	ws   *websocket.Conn
	wsMu sync.Mutex

	join JoinResponse

	pubPC        *webrtc.PeerConnection
	subPC        *webrtc.PeerConnection
	pubMu        sync.Mutex
	subMu        sync.Mutex
	pubRemoteSet bool
	subRemoteSet bool

	closed atomic.Bool

	OnReady              func()
	OnTrack              func(*webrtc.TrackRemote, *webrtc.RTPReceiver)
	OnDataChannel        func(*webrtc.DataChannel)
	OnPubConnected       func()
	OnParticipantUpdate  func([]ParticipantInfo)
	// OnRemoteCandidate fires once for every trickle ICE candidate
	// arriving from the SFU. Used by the Windows joiner to install
	// /32 bypass routes so the joiner's own media flows stay on the
	// physical NIC instead of recursing through the tunnel.
	OnRemoteCandidate    func(target int, candidate webrtc.ICECandidateInit)
	// OnRemoteSDP fires when the SFU sends an SDP offer or answer.
	// The SDP itself can carry candidates inline; the Windows joiner
	// uses this to bypass them before any media starts flowing.
	OnRemoteSDP          func(target int, sdpType, sdp string)
}

func NewClient(cfg Config) *Client {
	logFn := cfg.LogFn
	if logFn == nil {
		logFn = func(string, ...any) {}
	}
	return &Client{
		logFn:          logFn,
		wsURL:          cfg.ServerURL,
		token:          cfg.Token,
		origin:         cfg.Origin,
		ua:             cfg.UserAgent,
		settingEngine:  cfg.SettingEngine,
		netDialContext: cfg.NetDialContext,
		resolveICEHost: cfg.ResolveICEHost,
	}
}

func (c *Client) Join() JoinResponse              { return c.join }
func (c *Client) PubPC() *webrtc.PeerConnection { return c.pubPC }
func (c *Client) SubPC() *webrtc.PeerConnection { return c.subPC }

func (c *Client) Connect() error {
	u, err := url.Parse(c.wsURL)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}
	u.Path = "/rtc"
	q := u.Query()
	q.Set("access_token", c.token)
	q.Set("protocol", ProtocolVersion)
	q.Set("sdk", SDKName)
	q.Set("version", SDKVersion)
	q.Set("auto_subscribe", "1")
	q.Set("adaptive_stream", "true")
	u.RawQuery = q.Encode()

	headers := http.Header{}
	if c.ua != "" {
		headers.Set("User-Agent", c.ua)
	}
	if c.origin != "" {
		headers.Set("Origin", c.origin)
	}

	dialer := *websocket.DefaultDialer
	if c.netDialContext != nil {
		dialer.NetDialContext = c.netDialContext
	}
	conn, resp, err := dialer.Dial(u.String(), headers)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("ws dial: %w (status %d)", err, resp.StatusCode)
		}
		return fmt.Errorf("ws dial: %w", err)
	}
	c.ws = conn
	c.logFn("[lk] signaling connected")
	return nil
}

func (c *Client) sendSignal(payload []byte) error {
	c.wsMu.Lock()
	defer c.wsMu.Unlock()
	if c.ws == nil {
		return fmt.Errorf("ws not connected")
	}
	return c.ws.WriteMessage(websocket.BinaryMessage, payload)
}

func (c *Client) SendOffer(sdp string) error {
	return c.sendSignal(encSignalRequestOffer(sessionDescription{Type: "offer", SDP: sdp}))
}

func (c *Client) SendAnswer(sdp string) error {
	return c.sendSignal(encSignalRequestAnswer(sessionDescription{Type: "answer", SDP: sdp}))
}

func (c *Client) SendTrickle(candidate webrtc.ICECandidateInit, target int) error {
	js, _ := json.Marshal(candidate)
	return c.sendSignal(encSignalRequestTrickle(trickleMsg{
		CandidateInit: string(js),
		Target:        target,
	}))
}

func (c *Client) SendAddTrack(cid, name string, trackType, source int, width, height uint32) error {
	return c.sendSignal(encSignalRequestAddTrack(cid, name, trackType, source, width, height))
}

func (c *Client) SendLeave() error { return c.sendSignal(encSignalRequestLeave()) }

func (c *Client) SendPing() error {
	return c.sendSignal(encSignalRequestPing(time.Now().UnixMilli()))
}

func (c *Client) Close() {
	if !c.closed.CompareAndSwap(false, true) {
		return
	}
	c.wsMu.Lock()
	ws := c.ws
	c.wsMu.Unlock()
	if ws != nil {
		_ = ws.Close()
	}
	if c.pubPC != nil {
		_ = c.pubPC.Close()
	}
	if c.subPC != nil {
		_ = c.subPC.Close()
	}
}

func (c *Client) iceServersAsWebRTC() []webrtc.ICEServer {
	out := make([]webrtc.ICEServer, 0, len(c.join.ICEServers))
	resolved := make(map[string]string)
	for _, s := range c.join.ICEServers {
		urls := make([]string, len(s.URLs))
		copy(urls, s.URLs)
		if c.resolveICEHost != nil {
			for k, u := range urls {
				host := common.ExtractICEHost(u)
				if host == "" || net.ParseIP(host) != nil {
					continue
				}
				ip, ok := resolved[host]
				if !ok {
					var err error
					ip, err = c.resolveICEHost(host)
					if err != nil {
						c.logFn("[lk] resolve ICE host %s failed: %v", host, err)
						continue
					}
					resolved[host] = ip
					c.logFn("[lk] resolved ICE host %s -> %s", host, ip)
				}
				urls[k] = strings.Replace(u, host, ip, 1)
			}
		}
		ice := webrtc.ICEServer{URLs: urls}
		if s.Username != "" {
			ice.Username = s.Username
			ice.Credential = s.Credential
		}
		out = append(out, ice)
	}
	return out
}

func (c *Client) buildPeerConnections() error {
	cfg := webrtc.Configuration{ICEServers: c.iceServersAsWebRTC()}

	se := webrtc.SettingEngine{}
	if c.settingEngine != nil {
		se = *c.settingEngine
	}
	se.DetachDataChannels()
	api := webrtc.NewAPI(webrtc.WithSettingEngine(se))

	pubPC, err := api.NewPeerConnection(cfg)
	if err != nil {
		return fmt.Errorf("create pub pc: %w", err)
	}
	subPC, err := api.NewPeerConnection(cfg)
	if err != nil {
		_ = pubPC.Close()
		return fmt.Errorf("create sub pc: %w", err)
	}

	c.pubPC = pubPC
	c.subPC = subPC

	pubPC.OnICECandidate(func(cand *webrtc.ICECandidate) {
		if cand == nil {
			c.logFn("[lk] pub ICE gathering complete")
			return
		}
		c.logFn("[lk] pub local cand: %s", cand.String())
		_ = c.SendTrickle(cand.ToJSON(), TargetPublisher)
	})
	subPC.OnICECandidate(func(cand *webrtc.ICECandidate) {
		if cand == nil {
			c.logFn("[lk] sub ICE gathering complete")
			return
		}
		c.logFn("[lk] sub local cand: %s", cand.String())
		_ = c.SendTrickle(cand.ToJSON(), TargetSubscriber)
	})

	pubPC.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		c.logFn("[lk] pub PC state: %s", state.String())
		if state == webrtc.PeerConnectionStateConnected && c.OnPubConnected != nil {
			c.OnPubConnected()
		}
	})
	subPC.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		c.logFn("[lk] sub PC state: %s", state.String())
	})
	pubPC.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		c.logFn("[lk] pub ICE state: %s", state.String())
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

	c.logFn("[lk] PCs created (%d ICE servers)", len(c.join.ICEServers))
	for i, s := range c.join.ICEServers {
		c.logFn("[lk] iceServer[%d]: urls=%v hasCred=%v", i, s.URLs, s.Username != "")
	}
	return nil
}

func (c *Client) handleSignal(data []byte) {
	sr, err := decSignalResponse(data)
	if err != nil {
		c.logFn("[lk] decode signal: %v", err)
		return
	}
	switch sr.Kind {
	case signalRespJoin:
		if sr.Join != nil {
			c.join = *sr.Join
			c.logFn("[lk] join: room=%s participant=%s subscriberPrimary=%v iceServers=%d pingTimeout=%ds pingInterval=%ds",
				c.join.RoomName, c.join.ParticipantID, c.join.SubscriberPrimary, len(c.join.ICEServers),
				c.join.PingTimeoutSec, c.join.PingIntervalSec)
			if err := c.buildPeerConnections(); err != nil {
				c.logFn("[lk] %v", err)
				return
			}
			if c.OnReady != nil {
				c.OnReady()
			}
		}
	case signalRespAnswer:
		c.logFn("[lk] <- pub answer (%d bytes)", len(sr.SDP.SDP))
		if sr.SDP != nil {
			c.applyPubAnswer(sr.SDP.SDP)
		}
	case signalRespOffer:
		c.logFn("[lk] <- sub offer (%d bytes)", len(sr.SDP.SDP))
		if sr.SDP != nil {
			c.applySubOfferAndAnswer(sr.SDP.SDP)
		}
	case signalRespTrickle:
		if sr.Trickle != nil {
			c.logFn("[lk] <- trickle target=%d", sr.Trickle.Target)
			c.applyRemoteTrickle(*sr.Trickle)
		}
	case signalRespRefreshToken:
		if sr.Token != "" {
			c.token = sr.Token
			c.logFn("[lk] token refreshed")
		}
	case signalRespLeave:
		if sr.Leave != nil {
			c.logFn("[lk] ignored leave reason=%s action=%s",
				DisconnectReasonName(sr.Leave.Reason), LeaveActionName(sr.Leave.Action))
		} else {
			c.logFn("[lk] ignored leave")
		}
	case signalRespUpdate:
		if c.OnParticipantUpdate != nil && len(sr.Participants) > 0 {
			c.OnParticipantUpdate(sr.Participants)
		}
	default:
		c.logFn("[lk] <- signal kind=%d (%d bytes)", sr.Kind, len(data))
	}
}

func (c *Client) applyPubAnswer(sdp string) {
	if c.OnRemoteSDP != nil {
		c.OnRemoteSDP(TargetPublisher, "answer", sdp)
	}
	c.pubMu.Lock()
	defer c.pubMu.Unlock()
	if c.pubPC == nil {
		return
	}
	if err := c.pubPC.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: sdp}); err != nil {
		c.logFn("[lk] set pub remote answer: %v", err)
		return
	}
	c.pubRemoteSet = true
}

func (c *Client) applySubOfferAndAnswer(sdp string) {
	if c.OnRemoteSDP != nil {
		c.OnRemoteSDP(TargetSubscriber, "offer", sdp)
	}
	c.subMu.Lock()
	defer c.subMu.Unlock()
	if c.subPC == nil {
		return
	}
	if err := c.subPC.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: sdp}); err != nil {
		c.logFn("[lk] set sub remote offer: %v", err)
		return
	}
	c.subRemoteSet = true
	answer, err := c.subPC.CreateAnswer(nil)
	if err != nil {
		c.logFn("[lk] create sub answer: %v", err)
		return
	}
	if err := c.subPC.SetLocalDescription(answer); err != nil {
		c.logFn("[lk] set sub local answer: %v", err)
		return
	}
	if err := c.SendAnswer(answer.SDP); err != nil {
		c.logFn("[lk] send answer: %v", err)
	}
}

func (c *Client) applyRemoteTrickle(m trickleMsg) {
	if m.CandidateInit == "" {
		return
	}
	var ic webrtc.ICECandidateInit
	if err := json.Unmarshal([]byte(m.CandidateInit), &ic); err != nil {
		c.logFn("[lk] decode trickle candidate: %v", err)
		return
	}
	if c.OnRemoteCandidate != nil {
		c.OnRemoteCandidate(m.Target, ic)
	}
	switch m.Target {
	case TargetPublisher:
		c.pubMu.Lock()
		ready := c.pubRemoteSet
		c.pubMu.Unlock()
		if ready {
			_ = c.pubPC.AddICECandidate(ic)
		}
	case TargetSubscriber:
		c.subMu.Lock()
		ready := c.subRemoteSet
		c.subMu.Unlock()
		if ready {
			_ = c.subPC.AddICECandidate(ic)
		}
	}
}

func (c *Client) ReadLoop() error {
	defer c.Close()
	for {
		mt, data, err := c.ws.ReadMessage()
		if err != nil {
			return err
		}
		if mt != websocket.BinaryMessage {
			continue
		}
		c.handleSignal(data)
	}
}

func (c *Client) PingLoop() {
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
			return
		}
		sentN++
		if sentN <= 3 || sentN%12 == 0 {
			c.logFn("[lk] ping #%d sent", sentN)
		}
	}
}
