package joiner

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/kulikov0/headless-client/webrtc"
	"whitelist-bypass/relay/blink"
	"whitelist-bypass/relay/common"
	"whitelist-bypass/relay/tunnel"
)

type BlinkHeadlessJoiner struct {
	logFn             func(string, ...any)
	OnConnected       func(tunnel.DataTunnel)
	OnRemoteCandidate func(target int, candidateOrSDP string)
	ResolveFn         ResolveFunc
	Status            StatusEmitter
	PCConfig          PeerConnectionConfigurer

	httpClient  *http.Client
	cookiesPath string

	mu       sync.Mutex
	session  *blink.Session
	closed   bool
	stopCh   chan struct{}
	stopOnce sync.Once
}

type blinkJoinParams struct {
	JoinLink    string `json:"joinLink"`
	CookiesPath string `json:"cookiesPath"`
	TunnelMode  string `json:"tunnelMode"`
	VP8FPS      int    `json:"vp8Fps"`
	VP8Batch    int    `json:"vp8Batch"`
	DualTrack   bool   `json:"dualTrack"`
	Reliable    *bool  `json:"reliable"`
}

func NewBlinkHeadlessJoiner(logFn func(string, ...any), resolveFn ResolveFunc, status StatusEmitter, pcConfig PeerConnectionConfigurer) *BlinkHeadlessJoiner {
	return &BlinkHeadlessJoiner{
		logFn:     logFn,
		ResolveFn: resolveFn,
		Status:    status,
		PCConfig:  pcConfig,
		stopCh:    make(chan struct{}),
	}
}

func (j *BlinkHeadlessJoiner) RunWithParams(jsonParams string) {
	var params blinkJoinParams
	if err := json.Unmarshal([]byte(jsonParams), &params); err != nil {
		j.logFn("blink-joiner: failed to parse params: %v", err)
		j.Status.EmitStatusError("bad params: " + err.Error())
		return
	}
	if params.JoinLink == "" || params.CookiesPath == "" {
		j.logFn("blink-joiner: missing joinLink or cookiesPath")
		j.Status.EmitStatusError("missing joinLink or cookiesPath")
		return
	}
	j.httpClient = resolvingHTTPClient(j.ResolveFn)
	j.cookiesPath = params.CookiesPath

	credentials, err := j.loadCredentials()
	if err != nil {
		j.logFn("blink-joiner: login failed: %v", err)
		j.Status.EmitStatusError("login: " + err.Error())
		return
	}
	chatID, err := retryAfterLogin(j, func() (int64, error) {
		return credentials.ResolveChatID(params.JoinLink)
	})
	if err != nil {
		j.logFn("blink-joiner: bad join link: %v", err)
		j.Status.EmitStatusError("bad join link: " + err.Error())
		return
	}
	j.logFn("blink-joiner: chat=%d vp8Fps=%d vp8Batch=%d dualTrack=%v", chatID, params.VP8FPS, params.VP8Batch, params.DualTrack)

	keySource := strconv.FormatInt(chatID, 10)
	obf, err := tunnel.NewTunnelObfuscator(tunnel.DeriveSecretFromJoinLink(keySource))
	if err != nil {
		j.logFn("blink-joiner: obfuscator init failed: %v", err)
		j.Status.EmitStatusError("obfuscator init: " + err.Error())
		return
	}
	j.logFn("blink-joiner: obf key-source=%q localEpoch=0x%08x", keySource, obf.LocalEpoch())

	var configureSettingEngine func(*webrtc.SettingEngine)
	if j.PCConfig != nil {
		configureSettingEngine = j.PCConfig.ConfigureSettingEngine
	}

	var attempt atomic.Int32

	j.Status.EmitStatus(common.StatusConnecting)
	if err := j.runOnce(credentials, chatID, params, obf, configureSettingEngine, &attempt); err != nil {
		j.Status.EmitStatusError(err.Error())
		return
	}

	for {
		if j.isClosed() {
			j.logFn("blink-joiner: stopped")
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
		j.logFn("blink-joiner: reconnect attempt #%d", attempt.Load())
		j.Status.EmitStatus(common.StatusReconnecting)
		if err := j.runOnce(credentials, chatID, params, obf, configureSettingEngine, &attempt); err != nil {
			j.logFn("blink-joiner: %v, will retry", err)
		}
	}
}

func (j *BlinkHeadlessJoiner) MarkConfigAcked() {
	j.mu.Lock()
	sess := j.session
	j.mu.Unlock()
	if sess != nil {
		sess.MarkConfigAcked()
	}
}

func (j *BlinkHeadlessJoiner) Close() {
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

func (j *BlinkHeadlessJoiner) runOnce(credentials *blink.Credentials, chatID int64, params blinkJoinParams, obf *tunnel.TunnelObfuscator, configureSettingEngine func(*webrtc.SettingEngine), attempt *atomic.Int32) error {
	connection, err := retryAfterLogin(j, func() (blink.CallConnection, error) {
		return credentials.ConnectCall(chatID)
	})
	if err != nil {
		return err
	}
	j.logFn("blink-joiner: call=%s created=%v endpoint=%s", connection.CallID, connection.Created, connection.APIEndpoint)
	if connection.Created {
		j.logFn("blink-joiner: the chat had no active call, this joiner started a new one and the creator is not in it")
	}

	sess := blink.NewSession(blink.SessionConfig{
		RoomToken:              connection.Token,
		ServerURL:              connection.APIEndpoint,
		TunnelMode:             params.TunnelMode,
		Obfuscator:             obf,
		LogFn:                  j.logFn,
		ConfigureSettingEngine: configureSettingEngine,
		NetDialContext:         resolvingDialContext(j.ResolveFn),
		ResolveICEHost:         j.ResolveFn,
		VP8FPS:                 params.VP8FPS,
		VP8Batch:               params.VP8Batch,
		ScreenShare:            params.DualTrack,
		IsJoiner:               true,
		Reliable:               params.Reliable != nil && *params.Reliable,
	})
	sess.OnConnected = func(tun tunnel.DataTunnel) {
		attempt.Store(0)
		j.logFn("blink-joiner: === TUNNEL CONNECTED ===")
		j.Status.EmitStatus(common.StatusTunnelConnected)
		if j.OnConnected != nil {
			j.OnConnected(tun)
		}
	}
	sess.OnRemoteCandidate = j.OnRemoteCandidate

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

func (j *BlinkHeadlessJoiner) loadCredentials() (*blink.Credentials, error) {
	credentials, err := blink.LoadCredentials(j.cookiesPath, j.httpClient, j.logFn)
	if err == nil {
		return credentials, nil
	}
	j.logFn("blink-joiner: no saved login, waiting for a new one: %v", err)
	if err := j.login(); err != nil {
		return nil, err
	}
	return blink.LoadCredentials(j.cookiesPath, j.httpClient, j.logFn)
}

func (j *BlinkHeadlessJoiner) login() error {
	return blink.LoginToFile(j.cookiesPath, j.httpClient, j.stopCh, j.Status.EmitStatus, j.logFn)
}

func (j *BlinkHeadlessJoiner) waitBeforeRetry(attempt int) bool {
	return waitReconnectBackoff(attempt, j.logFn, "blink-joiner", j.stopCh, j.isClosed)
}

func (j *BlinkHeadlessJoiner) clearSession(sess *blink.Session) {
	j.mu.Lock()
	if j.session == sess {
		j.session = nil
	}
	j.mu.Unlock()
}

func (j *BlinkHeadlessJoiner) isClosed() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.closed
}

func retryAfterLogin[T any](j *BlinkHeadlessJoiner, request func() (T, error)) (T, error) {
	result, err := request()
	if !blink.IsUnauthorized(err) {
		return result, err
	}
	j.logFn("blink-joiner: the saved login is no longer valid, waiting for a new one: %v", err)
	if err := j.login(); err != nil {
		var empty T
		return empty, err
	}
	return request()
}
