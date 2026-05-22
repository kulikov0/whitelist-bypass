package joiner

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
	"whitelist-bypass/relay/common"
	"whitelist-bypass/relay/tunnel"
	"whitelist-bypass/relay/wbstream"
)

type WBStreamHeadlessJoiner struct {
	logFn       func(string, ...any)
	OnConnected func(tunnel.DataTunnel)
	ResolveFn   ResolveFunc
	Status      StatusEmitter
	PCConfig    PeerConnectionConfigurer

	mu      sync.Mutex
	session *wbstream.Session
	closed  bool
}

func NewWBStreamHeadlessJoiner(logFn func(string, ...any), resolveFn ResolveFunc, status StatusEmitter, pcConfig PeerConnectionConfigurer) *WBStreamHeadlessJoiner {
	return &WBStreamHeadlessJoiner{
		logFn:     logFn,
		ResolveFn: resolveFn,
		Status:    status,
		PCConfig:  pcConfig,
	}
}

func (j *WBStreamHeadlessJoiner) RunWithParams(jsonParams string) {
	var params struct {
		RoomID      string `json:"roomId"`
		DisplayName string `json:"displayName"`
		VP8FPS      int    `json:"vp8Fps"`
		VP8Batch    int    `json:"vp8Batch"`
	}
	if err := json.Unmarshal([]byte(jsonParams), &params); err != nil {
		j.logFn("wbstream-joiner: failed to parse params: %v", err)
		j.Status.EmitStatusError("bad params: " + err.Error())
		return
	}
	if params.RoomID == "" {
		j.logFn("wbstream-joiner: missing roomId")
		j.Status.EmitStatusError("missing roomId")
		return
	}
	if params.DisplayName == "" {
		params.DisplayName = "Joiner"
	}

	httpClient := j.makeHTTPClient()
	j.logFn("wbstream-joiner: room=%s name=%s vp8Fps=%d vp8Batch=%d", params.RoomID, params.DisplayName, params.VP8FPS, params.VP8Batch)
	j.Status.EmitStatus(common.StatusConnecting)

	roomID, roomToken, _, serverURL, err := wbstream.AuthAndGetToken(httpClient, params.RoomID, params.DisplayName)
	if err != nil {
		j.logFn("wbstream-joiner: auth failed: %v", err)
		j.Status.EmitStatusError("auth: " + err.Error())
		return
	}
	j.logFn("wbstream-joiner: server=%s", serverURL)

	obf, err := tunnel.NewTunnelObfuscator(tunnel.DeriveSecretFromJoinLink(roomID))
	if err != nil {
		j.logFn("wbstream-joiner: obfuscator init failed: %v", err)
		j.Status.EmitStatusError("obfuscator init: " + err.Error())
		return
	}
	j.logFn("wbstream-joiner: obf key-source=%q localEpoch=0x%08x", roomID, obf.LocalEpoch())

	var settingEngine *webrtc.SettingEngine
	if j.PCConfig != nil {
		se := webrtc.SettingEngine{}
		j.PCConfig.ConfigureSettingEngine(&se)
		settingEngine = &se
	}

	sess := wbstream.NewSession(wbstream.SessionConfig{
		RoomToken:      roomToken,
		ServerURL:      serverURL,
		DisplayName:    params.DisplayName,
		Obfuscator:     obf,
		LogFn:          j.logFn,
		SettingEngine:  settingEngine,
		NetDialContext: j.makeDialContext(),
		ResolveICEHost: j.ResolveFn,
		VP8FPS:         params.VP8FPS,
		VP8Batch:       params.VP8Batch,
		ScreenShare:    true,
	})
	sess.OnConnected = func(tun tunnel.DataTunnel) {
		j.logFn("wbstream-joiner: === TUNNEL CONNECTED ===")
		j.Status.EmitStatus(common.StatusTunnelConnected)
		if j.OnConnected != nil {
			j.OnConnected(tun)
		}
	}

	j.mu.Lock()
	j.session = sess
	closed := j.closed
	j.mu.Unlock()
	if closed {
		sess.Close()
		return
	}

	if err := sess.Start(); err != nil {
		j.logFn("wbstream-joiner: session start failed: %v", err)
		j.Status.EmitStatusError("session: " + err.Error())
		return
	}
	<-sess.Done()
	j.logFn("wbstream-joiner: session ended")
	j.Status.EmitStatus(common.StatusTunnelLost)
}

func (j *WBStreamHeadlessJoiner) Close() {
	j.mu.Lock()
	j.closed = true
	sess := j.session
	j.session = nil
	j.mu.Unlock()
	if sess != nil {
		sess.Close()
	}
}

func (j *WBStreamHeadlessJoiner) makeDialContext() func(ctx context.Context, network, addr string) (net.Conn, error) {
	if j.ResolveFn == nil {
		return nil
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, _ := net.SplitHostPort(addr)
		resolvedIP, err := j.ResolveFn(host)
		if err != nil {
			return nil, err
		}
		return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, resolvedIP+":"+port)
	}
}

func (j *WBStreamHeadlessJoiner) makeHTTPClient() *http.Client {
	transport := &http.Transport{DialContext: j.makeDialContext()}
	return &http.Client{Timeout: 60 * time.Second, Transport: transport}
}
