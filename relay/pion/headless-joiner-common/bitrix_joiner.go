package joiner

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/webrtc/v4"
	"whitelist-bypass/relay/bitrix"
	"whitelist-bypass/relay/common"
	"whitelist-bypass/relay/tunnel"
)

type BitrixHeadlessJoiner struct {
	logFn       func(string, ...any)
	OnConnected func(tunnel.DataTunnel)
	ResolveFn   ResolveFunc
	Status      StatusEmitter
	PCConfig    PeerConnectionConfigurer

	joinLink    string
	displayName string
	portal      string
	alias       string
	tunnelMode  string
	vp8FPS      int
	vp8Batch    int
	reliable    bool
	dualTrack   bool

	sessMu sync.Mutex
	sig    *bitrix.Signal
	ms     *bitrix.MediaSession

	closeMu sync.Mutex
	closed  bool

	stopCh           chan struct{}
	stopOnce         sync.Once
	reconnectAttempt atomic.Int32
}

func NewBitrixHeadlessJoiner(logFn func(string, ...any), resolveFn ResolveFunc, status StatusEmitter, pcConfig PeerConnectionConfigurer) *BitrixHeadlessJoiner {
	return &BitrixHeadlessJoiner{
		logFn:     logFn,
		ResolveFn: resolveFn,
		Status:    status,
		PCConfig:  pcConfig,
		stopCh:    make(chan struct{}),
	}
}

func (j *BitrixHeadlessJoiner) MarkConfigAcked() {
	j.sessMu.Lock()
	ms := j.ms
	j.sessMu.Unlock()
	if ms != nil {
		ms.MarkConfigAcked()
	}
}

func (j *BitrixHeadlessJoiner) RunWithParams(jsonParams string) {
	var params struct {
		JoinLink    string `json:"joinLink"`
		DisplayName string `json:"displayName"`
		TunnelMode  string `json:"tunnelMode"`
		VP8FPS      int    `json:"vp8Fps"`
		VP8Batch    int    `json:"vp8Batch"`
		Reliable    bool   `json:"reliable"`
		DualTrack   bool   `json:"dualTrack"`
	}
	if err := json.Unmarshal([]byte(jsonParams), &params); err != nil {
		j.logFn("bitrix-joiner: failed to parse params: %v", err)
		j.Status.EmitStatusError("bad params: " + err.Error())
		return
	}
	j.joinLink = params.JoinLink
	j.displayName = params.DisplayName
	if j.displayName == "" {
		j.displayName = "Joiner"
	}
	if strings.EqualFold(params.TunnelMode, "dc") {
		j.tunnelMode = bitrix.TunnelModeDC
	} else {
		j.tunnelMode = bitrix.TunnelModeVideo
	}
	j.vp8FPS = params.VP8FPS
	if j.vp8FPS <= 0 {
		j.vp8FPS = 24
	}
	j.vp8Batch = params.VP8Batch
	if j.vp8Batch <= 0 {
		j.vp8Batch = 30
	}
	j.reliable = params.Reliable
	j.dualTrack = params.DualTrack

	u, err := url.Parse(j.joinLink)
	if err != nil {
		j.logFn("bitrix-joiner: bad join link: %v", err)
		j.Status.EmitStatusError("bad join link: " + err.Error())
		return
	}
	j.portal = u.Scheme + "://" + u.Host
	j.alias = strings.TrimPrefix(u.Path, "/video/")
	if j.alias == "" || j.portal == "://" {
		j.logFn("bitrix-joiner: link missing portal or /video/CODE: %s", j.joinLink)
		j.Status.EmitStatusError("bad join link")
		return
	}
	j.logFn("bitrix-joiner: portal=%s alias=%s mode=%s vp8Fps=%d vp8Batch=%d reliable=%v dualTrack=%v",
		j.portal, j.alias, j.tunnelMode, j.vp8FPS, j.vp8Batch, j.reliable, j.dualTrack)

	j.Status.EmitStatus(common.StatusConnecting)
	if err := j.runOnce(); err != nil {
		j.logFn("bitrix-joiner: %v", err)
		j.Status.EmitStatusError(err.Error())
		return
	}

	for {
		if j.isClosed() {
			return
		}
		j.Status.EmitStatus(common.StatusTunnelLost)
		j.resetSessionState()
		if !j.waitBeforeRetry(int(j.reconnectAttempt.Load())) {
			return
		}
		j.reconnectAttempt.Add(1)
		if j.isClosed() {
			return
		}
		j.logFn("bitrix-joiner: reconnect attempt #%d", j.reconnectAttempt.Load())
		j.Status.EmitStatus(common.StatusReconnecting)
		if err := j.runOnce(); err != nil {
			j.logFn("bitrix-joiner: %v, will retry", err)
		}
	}
}

func (j *BitrixHeadlessJoiner) Close() {
	j.closeMu.Lock()
	j.closed = true
	j.closeMu.Unlock()
	j.stopOnce.Do(func() { close(j.stopCh) })
	j.resetSessionState()
}

func (j *BitrixHeadlessJoiner) runOnce() error {
	userAgent := common.RandomDeviceProfile().UserAgent
	c, err := bitrix.NewClient(j.portal, userAgent)
	if err != nil {
		return fmt.Errorf("new client: %w", err)
	}
	c.HTTP.Transport = j.makeTransport()
	c.LogFn = j.logFn

	res, err := c.JoinAsGuest(j.alias, j.displayName)
	if err != nil {
		return fmt.Errorf("join as guest: %w", err)
	}
	j.logFn("bitrix-joiner: roomId=%s mediaServer=%s", res.RoomID, res.MediaServerURL)

	settingEngine := webrtc.SettingEngine{}
	if j.PCConfig != nil {
		j.PCConfig.ConfigureSettingEngine(&settingEngine)
	}

	var once sync.Once
	connected := make(chan struct{})
	sig, err := bitrix.ConnectSignal(bitrix.SignalConfig{
		SignalURL:      bitrix.SignalURL(res),
		Origin:         j.portal,
		UserAgent:      userAgent,
		LogFn:          j.logFn,
		SettingEngine:  &settingEngine,
		NetDialContext: j.makeDialContext(),
		OnConnected: func() {
			once.Do(func() { close(connected) })
		},
	})
	if err != nil {
		return fmt.Errorf("signal connect: %w", err)
	}

	ms, err := bitrix.NewMediaSession(bitrix.MediaParams{
		Signal:    sig,
		Alias:     j.alias,
		Mode:      j.tunnelMode,
		FPS:       j.vp8FPS,
		Batch:     j.vp8Batch,
		Reliable:  j.reliable,
		DualTrack: j.dualTrack,
		LogFn:     j.logFn,
	})
	if err != nil {
		sig.Close()
		return fmt.Errorf("media session: %w", err)
	}
	ms.OnConnected = func(tun tunnel.DataTunnel) {
		j.reconnectAttempt.Store(0)
		j.Status.EmitStatus(common.StatusTunnelConnected)
		j.logFn("bitrix-joiner: === TUNNEL CONNECTED === %T", tun)
		if j.OnConnected != nil {
			j.OnConnected(tun)
		}
	}
	j.setSession(sig, ms)

	done := make(chan struct{})
	go func() {
		if err := sig.Run(); err != nil {
			j.logFn("bitrix-joiner: signal run ended: %s", common.MaskError(err))
		}
		close(done)
	}()

	select {
	case <-connected:
	case <-done:
		return fmt.Errorf("signal closed before media connect")
	case <-j.stopCh:
		sig.Close()
		return nil
	}

	if err := ms.Start(); err != nil {
		sig.Close()
		return fmt.Errorf("media start: %w", err)
	}

	select {
	case <-done:
	case <-j.stopCh:
		sig.Close()
	}
	return nil
}

func (j *BitrixHeadlessJoiner) makeDialContext() func(ctx context.Context, network, addr string) (net.Conn, error) {
	if j.ResolveFn == nil {
		return nil
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		resolvedIP, err := j.ResolveFn(host)
		if err != nil {
			return nil, err
		}
		return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, resolvedIP+":"+port)
	}
}

func (j *BitrixHeadlessJoiner) makeTransport() *http.Transport {
	return &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		DialContext:     j.makeDialContext(),
	}
}

func (j *BitrixHeadlessJoiner) setSession(sig *bitrix.Signal, ms *bitrix.MediaSession) {
	j.sessMu.Lock()
	j.sig = sig
	j.ms = ms
	j.sessMu.Unlock()
}

func (j *BitrixHeadlessJoiner) resetSessionState() {
	j.sessMu.Lock()
	sig := j.sig
	ms := j.ms
	j.sig = nil
	j.ms = nil
	j.sessMu.Unlock()
	if ms != nil {
		ms.Stop()
	}
	if sig != nil {
		sig.Close()
	}
}

func (j *BitrixHeadlessJoiner) waitBeforeRetry(attempt int) bool {
	return waitReconnectBackoff(attempt, j.logFn, "bitrix-joiner", j.stopCh, j.isClosed)
}

func (j *BitrixHeadlessJoiner) isClosed() bool {
	j.closeMu.Lock()
	defer j.closeMu.Unlock()
	return j.closed
}
