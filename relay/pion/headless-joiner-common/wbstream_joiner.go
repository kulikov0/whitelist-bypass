package joiner

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kulikov0/headlessclient/webrtc"
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

	mu       sync.Mutex
	session  *wbstream.Session
	closed   bool
	stopCh   chan struct{}
	stopOnce sync.Once
}

func NewWBStreamHeadlessJoiner(logFn func(string, ...any), resolveFn ResolveFunc, status StatusEmitter, pcConfig PeerConnectionConfigurer) *WBStreamHeadlessJoiner {
	return &WBStreamHeadlessJoiner{
		logFn:     logFn,
		ResolveFn: resolveFn,
		Status:    status,
		PCConfig:  pcConfig,
		stopCh:    make(chan struct{}),
	}
}

func (j *WBStreamHeadlessJoiner) RunWithParams(jsonParams string) {
	var params struct {
		RoomID      string `json:"roomId"`
		DisplayName string `json:"displayName"`
		TunnelMode  string `json:"tunnelMode"`
		VP8FPS      int    `json:"vp8Fps"`
		VP8Batch    int    `json:"vp8Batch"`
		DualTrack   bool   `json:"dualTrack"`
		Reliable    *bool  `json:"reliable"`
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
	reliable := params.Reliable != nil && *params.Reliable

	httpClient := j.makeHTTPClient()
	j.logFn("wbstream-joiner: room=%s name=%s vp8Fps=%d vp8Batch=%d dualTrack=%v", params.RoomID, params.DisplayName, params.VP8FPS, params.VP8Batch, params.DualTrack)

	obf, err := tunnel.NewTunnelObfuscator(tunnel.DeriveSecretFromJoinLink(params.RoomID))
	if err != nil {
		j.logFn("wbstream-joiner: obfuscator init failed: %v", err)
		j.Status.EmitStatusError("obfuscator init: " + err.Error())
		return
	}
	j.logFn("wbstream-joiner: obf key-source=%q localEpoch=0x%08x", params.RoomID, obf.LocalEpoch())

	var settingEngine *webrtc.SettingEngine
	if j.PCConfig != nil {
		se := webrtc.SettingEngine{}
		j.PCConfig.ConfigureSettingEngine(&se)
		settingEngine = &se
	}

	var attempt atomic.Int32

	j.Status.EmitStatus(common.StatusConnecting)
	if err := j.runOnce(httpClient, params.RoomID, params.DisplayName, params.TunnelMode, obf, settingEngine, params.VP8FPS, params.VP8Batch, params.DualTrack, reliable, &attempt); err != nil {
		j.Status.EmitStatusError(err.Error())
		return
	}

	for {
		if j.isClosed() {
			j.logFn("wbstream-joiner: stopped")
			return
		}
		j.Status.EmitStatus(common.StatusTunnelLost)
		if !j.waitBeforeRetry(int(attempt.Load())) {
			return
		}
		attempt.Add(1)
		if j.isClosed() {
			return
		}
		j.logFn("wbstream-joiner: reconnect attempt #%d", attempt.Load())
		j.Status.EmitStatus(common.StatusReconnecting)
		if err := j.runOnce(httpClient, params.RoomID, params.DisplayName, params.TunnelMode, obf, settingEngine, params.VP8FPS, params.VP8Batch, params.DualTrack, reliable, &attempt); err != nil {
			j.logFn("wbstream-joiner: %v, will retry", err)
		}
	}
}

func (j *WBStreamHeadlessJoiner) runOnce(httpClient *http.Client, roomID, displayName, tunnelMode string, obf *tunnel.TunnelObfuscator, settingEngine *webrtc.SettingEngine, vp8FPS, vp8Batch int, dualTrack, reliable bool, attempt *atomic.Int32) error {
	_, roomToken, _, serverURL, authErr := wbstream.AuthAndGetToken(httpClient, roomID, displayName)
	if authErr != nil {
		return fmt.Errorf("auth: %w", authErr)
	}
	j.logFn("wbstream-joiner: server=%s", serverURL)

	sess := wbstream.NewSession(wbstream.SessionConfig{
		RoomToken:      roomToken,
		ServerURL:      serverURL,
		DisplayName:    displayName,
		TunnelMode:     tunnelMode,
		Obfuscator:     obf,
		LogFn:          j.logFn,
		SettingEngine:  settingEngine,
		NetDialContext: j.makeDialContext(),
		ResolveICEHost: j.ResolveFn,
		VP8FPS:         vp8FPS,
		VP8Batch:       vp8Batch,
		ScreenShare:    dualTrack,
		IsJoiner:       true,
		Reliable:       reliable,
	})
	sess.OnConnected = func(tun tunnel.DataTunnel) {
		attempt.Store(0)
		j.logFn("wbstream-joiner: === TUNNEL CONNECTED ===")
		j.Status.EmitStatus(common.StatusTunnelConnected)
		if j.OnConnected != nil {
			j.OnConnected(tun)
		}
	}

	j.mu.Lock()
	if j.closed {
		j.mu.Unlock()
		sess.Close()
		return nil
	}
	j.session = sess
	j.mu.Unlock()

	if err := sess.Start(); err != nil {
		j.clearSession(sess)
		return fmt.Errorf("session: %w", err)
	}

	<-sess.Done()
	sess.Close()
	j.clearSession(sess)
	return nil
}

func (j *WBStreamHeadlessJoiner) MarkConfigAcked() {
	j.mu.Lock()
	sess := j.session
	j.mu.Unlock()
	if sess != nil {
		sess.MarkConfigAcked()
	}
}

func (j *WBStreamHeadlessJoiner) waitBeforeRetry(attempt int) bool {
	return waitReconnectBackoff(attempt, j.logFn, "wbstream-joiner", j.stopCh, j.isClosed)
}

func (j *WBStreamHeadlessJoiner) clearSession(sess *wbstream.Session) {
	j.mu.Lock()
	if j.session == sess {
		j.session = nil
	}
	j.mu.Unlock()
}

func (j *WBStreamHeadlessJoiner) isClosed() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.closed
}

func (j *WBStreamHeadlessJoiner) Close() {
	j.stopOnce.Do(func() { close(j.stopCh) })
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
