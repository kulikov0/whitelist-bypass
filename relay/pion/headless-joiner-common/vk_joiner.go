package joiner

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kulikov0/headlessclient"
	"github.com/pion/webrtc/v4"
	"whitelist-bypass/relay/common"
	"whitelist-bypass/relay/tunnel"
	"whitelist-bypass/relay/wtsignal"
)

const vkMaxReconnectAttempts = 10

const vkTopologyDirect = "DIRECT"

type vkAuthRottenError struct {
	Code string
	Msg  string
}

func (e *vkAuthRottenError) Error() string {
	if e.Msg != "" {
		return fmt.Sprintf("auth rotten: %s %s", e.Code, e.Msg)
	}
	return fmt.Sprintf("auth rotten: %s", e.Code)
}

type VKHeadlessAuthParams struct {
	SessionKey      string `json:"sessionKey"`
	ApplicationKey  string `json:"applicationKey"`
	APIBaseURL      string `json:"apiBaseURL"`
	JoinLink        string `json:"joinLink"`
	AnonymToken     string `json:"anonymToken"`
	AppVersion      string `json:"appVersion"`
	ProtocolVersion string `json:"protocolVersion"`
	TunnelMode      string `json:"tunnelMode"`
	VP8FPS          int    `json:"vp8Fps"`
	VP8Batch        int    `json:"vp8Batch"`
	DualTrack       bool   `json:"dualTrack"`
}

type VKJoinResponse struct {
	Endpoint   string `json:"endpoint"`
	WtEndpoint string `json:"wt_endpoint"`
	Token      string `json:"token"`
	TurnServer struct {
		URLs       []string `json:"urls"`
		Username   string   `json:"username"`
		Credential string   `json:"credential"`
	} `json:"turn_server"`
	StunServer struct {
		URLs []string `json:"urls"`
	} `json:"stun_server"`
}

type VKHeadlessJoiner struct {
	logFn       func(string, ...any)
	OnConnected func(tunnel.DataTunnel)
	// OnRemoteCandidate is fired for every trickle ICE candidate the
	// VK SFU sends and once per incoming SDP (target=-1) so the caller
	// can extract any candidates carried inline. The Windows joiner
	// uses this to install /32 bypass routes before the candidate
	// reaches Pion.
	OnRemoteCandidate func(target int, candidateOrSDP string)
	ResolveFn         ResolveFunc
	Status            StatusEmitter
	PCConfig          PeerConnectionConfigurer
	AddTracks         AddTunnelTracksFunc
	ReadTrackFn       ReadTrackFunc

	authParams   *VKHeadlessAuthParams
	joinResp     *VKJoinResponse
	sfu          *wtsignal.Conn
	vkMu         sync.Mutex
	vkSeq        int
	remotePeerID *int64

	pc             *webrtc.PeerConnection
	sampleTrack    *webrtc.TrackLocalStaticSample
	dc             *webrtc.DataChannel
	vp8tunnel      *tunnel.VP8DataTunnel
	sym            *tunnel.SymmetricScreenTunnel
	producerScreen screenUplink
	obf            *tunnel.TunnelObfuscator
	vp8FPS         int
	vp8Batch       int
	dualTrack      bool
	remoteSet      bool
	pendingICE     []webrtc.ICECandidateInit

	configAck        configAckTracker
	reconnectAttempt atomic.Int32
	stopCh           chan struct{}
	stopOnce         sync.Once
}

func NewVKHeadlessJoiner(logFn func(string, ...any), resolveFn ResolveFunc, status StatusEmitter, pcConfig PeerConnectionConfigurer, addTracks AddTunnelTracksFunc, readTrackFn ReadTrackFunc) *VKHeadlessJoiner {
	return &VKHeadlessJoiner{
		logFn:       logFn,
		ResolveFn:   resolveFn,
		Status:      status,
		PCConfig:    pcConfig,
		AddTracks:   addTracks,
		ReadTrackFn: readTrackFn,
		stopCh:      make(chan struct{}),
	}
}

func (h *VKHeadlessJoiner) RunWithParams(jsonParams string) {
	var params VKHeadlessAuthParams
	if err := json.Unmarshal([]byte(jsonParams), &params); err != nil {
		h.logFn("vk-joiner: failed to parse auth params: %v", err)
		h.Status.EmitStatusError("bad params: " + err.Error())
		return
	}
	h.authParams = &params
	obf, err := tunnel.NewTunnelObfuscator(tunnel.DeriveSecretFromJoinLink(params.JoinLink))
	if err != nil {
		h.logFn("vk-joiner: obfuscator init failed: %v", err)
		h.Status.EmitStatusError("obfuscator init: " + err.Error())
		return
	}
	h.obf = obf
	h.vp8FPS = params.VP8FPS
	h.vp8Batch = params.VP8Batch
	// h.dualTrack = params.DualTrack // temporarily disabled for VK joiners
	h.logFn("vk-joiner: auth params received")
	h.logFn("vk-joiner: obf key-source=%q localEpoch=0x%08x", params.JoinLink, obf.LocalEpoch())
	h.logFn("vk-joiner:   appVersion=%s protocolVersion=%s vp8Fps=%d vp8Batch=%d",
		params.AppVersion, params.ProtocolVersion, params.VP8FPS, params.VP8Batch)

	h.Status.EmitStatus(common.StatusConnecting)
	if err := h.runOnce(); err != nil {
		h.logFn("vk-joiner: %v", err)
		h.Status.EmitStatusError(err.Error())
		return
	}

	for {
		if h.isClosed() {
			return
		}
		h.Status.EmitStatus(common.StatusTunnelLost)
		if !h.waitBeforeRetry(int(h.reconnectAttempt.Load())) {
			return
		}
		attempt := h.reconnectAttempt.Add(1)
		if h.isClosed() {
			return
		}
		if int(attempt) > vkMaxReconnectAttempts {
			h.logFn("vk-joiner: gave up after %d consecutive reconnect attempts", vkMaxReconnectAttempts)
			h.Status.EmitStatusError("reconnect attempts exhausted")
			return
		}
		h.logFn("vk-joiner: reconnect attempt #%d", attempt)
		h.Status.EmitStatus(common.StatusReconnecting)
		if err := h.runOnce(); err != nil {
			var authRotten *vkAuthRottenError
			if errors.As(err, &authRotten) {
				h.logFn("vk-joiner: %v, surrendering", err)
				h.Status.EmitStatusError("call failed: " + err.Error())
				return
			}
			h.logFn("vk-joiner: %v, will retry", err)
		}
	}
}

func (h *VKHeadlessJoiner) runOnce() error {
	h.resetSessionState()
	if err := h.joinCall(); err != nil {
		return err
	}
	h.connectSFU()
	return nil
}

func (h *VKHeadlessJoiner) MarkConfigAcked() { h.configAck.mark() }

func (h *VKHeadlessJoiner) waitBeforeRetry(attempt int) bool {
	return waitReconnectBackoff(attempt, h.logFn, "vk-joiner", h.stopCh, h.isClosed)
}

func (h *VKHeadlessJoiner) isClosed() bool {
	select {
	case <-h.stopCh:
		return true
	default:
		return false
	}
}

func (h *VKHeadlessJoiner) resetSessionState() {
	h.vkMu.Lock()
	sfu := h.sfu
	h.sfu = nil
	h.vkSeq = 0
	h.vkMu.Unlock()
	if sfu != nil {
		sfu.Close()
	}
	if h.sym != nil {
		h.sym.Stop()
		h.sym = nil
	}
	if h.vp8tunnel != nil {
		h.vp8tunnel.Stop()
		h.vp8tunnel = nil
	}
	h.producerScreen.reset()
	if h.dc != nil {
		h.dc.Close()
		h.dc = nil
	}
	if h.pc != nil {
		h.pc.Close()
		h.pc = nil
	}
	h.sampleTrack = nil
	h.remoteSet = false
	h.pendingICE = nil
	h.remotePeerID = nil
	h.joinResp = nil
}

func (h *VKHeadlessJoiner) Close() {
	h.stopOnce.Do(func() { close(h.stopCh) })
	StopCaptchaProxy()
	h.vkMu.Lock()
	sfu := h.sfu
	h.sfu = nil
	h.vkMu.Unlock()
	if sfu != nil {
		sfu.Close()
	}
	if h.vp8tunnel != nil {
		h.vp8tunnel.Stop()
	}
	if h.pc != nil {
		h.pc.Close()
	}
}

func (h *VKHeadlessJoiner) closeTransport() {
	h.vkMu.Lock()
	sfu := h.sfu
	h.vkMu.Unlock()
	if sfu != nil {
		sfu.Close()
	}
}

func (h *VKHeadlessJoiner) joinCall() error {
	apiURL := h.authParams.APIBaseURL
	parsed, err := url.Parse(apiURL)
	if err != nil {
		return fmt.Errorf("bad apiBaseURL: %w", err)
	}
	resolvedIP, err := h.ResolveFn(parsed.Hostname())
	if err != nil {
		return fmt.Errorf("resolve %s: %w", parsed.Hostname(), err)
	}
	h.logFn("vk-joiner: resolved %s -> %s", parsed.Hostname(), resolvedIP)

	screenFlag := "false"
	if h.dualTrack {
		screenFlag = "true"
	}
	body := url.Values{
		"method":          {"vchat.joinConversationByLink"},
		"session_key":     {h.authParams.SessionKey},
		"application_key": {h.authParams.ApplicationKey},
		"joinLink":        {h.authParams.JoinLink},
		"anonymToken":     {h.authParams.AnonymToken},
		"isVideo":         {"true"},
		"isAudio":         {"false"},
		"mediaSettings":   {`{"isAudioEnabled":false,"isVideoEnabled":true,"isScreenSharingEnabled":` + screenFlag + `}`},
		"format":          {"json"},
	}

	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true, ServerName: parsed.Hostname()},
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				_, port, _ := net.SplitHostPort(addr)
				return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, resolvedIP+":"+port)
			},
		},
	}

	req, err := http.NewRequest("POST", apiURL, strings.NewReader(body.Encode()))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", common.UserAgent)

	h.logFn("vk-joiner: calling joinConversationByLink...")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("joinConversationByLink: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read join response: %w", err)
	}

	var joinResp VKJoinResponse
	if jsonErr := json.Unmarshal(raw, &joinResp); jsonErr != nil {
		return fmt.Errorf("decode join response: %w (body: %s)", jsonErr, truncateBody(raw))
	}
	if joinResp.Endpoint == "" {
		if rotten := detectVKAuthRotten(raw); rotten != nil {
			return rotten
		}
		return fmt.Errorf("empty endpoint in join response: %s", truncateBody(raw))
	}

	h.joinResp = &joinResp
	h.logFn("vk-joiner: joined, turn=%v", joinResp.TurnServer.URLs)
	return nil
}

func detectVKAuthRotten(raw []byte) *vkAuthRottenError {
	var generic map[string]interface{}
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil
	}
	errCode, _ := generic["error_code"].(string)
	if errCode == "" {
		if codeNum, ok := generic["error_code"].(float64); ok {
			errCode = fmt.Sprintf("%.0f", codeNum)
		}
	}
	if errCode == "" {
		errCode, _ = generic["errorCode"].(string)
	}
	if errCode == "" {
		return nil
	}
	switch errCode {
	case "SESSION_EXPIRED", "AUTH_LOGIN", "SESSION_NOT_FOUND", "INVALID_SESSION_KEY":
		errMsg, _ := generic["error_msg"].(string)
		return &vkAuthRottenError{Code: errCode, Msg: errMsg}
	}
	return nil
}

func truncateBody(raw []byte) string {
	const maxLen = 200
	if len(raw) > maxLen {
		return string(raw[:maxLen]) + "..."
	}
	return string(raw)
}

func (h *VKHeadlessJoiner) connectSFU() {
	endpoint := h.joinResp.WtEndpoint
	if endpoint == "" {
		h.logFn("vk-joiner: no wt_endpoint in join response, cannot connect")
		return
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		h.logFn("vk-joiner: bad endpoint URL: %s", common.MaskError(err))
		return
	}

	hostname := parsed.Hostname()
	resolvedIP, err := h.ResolveFn(hostname)
	if err != nil {
		h.logFn("vk-joiner: DNS resolve failed: %s", common.MaskError(err))
		return
	}
	h.logFn("vk-joiner: resolved %s -> %s", common.MaskAddr(hostname), common.MaskAddr(resolvedIP))

	capabilities := "2F7F"
	wtURL := endpoint +
		"&platform=WEB" +
		"&appVersion=" + h.authParams.AppVersion +
		"&version=" + h.authParams.ProtocolVersion +
		"&device=browser&capabilities=" + capabilities + "&clientType=VK&tgt=join&compression=deflate-raw"

	sfu, err := wtsignal.Dial(wtURL, hostname, resolvedIP)
	if err != nil {
		h.logFn("vk-joiner: WebTransport connect failed: %s", common.MaskError(err))
		return
	}
	h.vkMu.Lock()
	h.sfu = sfu
	h.vkSeq = 0
	h.vkMu.Unlock()
	h.logFn("vk-joiner: WebTransport connected")

	h.vkSend("update-media-modifiers", map[string]interface{}{
		"mediaModifiers": map[string]interface{}{"denoise": true, "denoiseAnn": true},
	})
	h.vkSend("change-media-settings", map[string]interface{}{
		"mediaSettings": map[string]interface{}{
			"isAudioEnabled": false, "isVideoEnabled": true,
			"isScreenSharingEnabled": h.dualTrack, "isFastScreenSharingEnabled": false,
			"isAudioSharingEnabled": false, "isAnimojiEnabled": false,
		},
	})

	h.readLoop()
}

func (h *VKHeadlessJoiner) vkSend(command string, extra map[string]interface{}) {
	h.vkMu.Lock()
	defer h.vkMu.Unlock()
	if h.sfu == nil {
		return
	}
	h.vkSeq++
	extra["command"] = command
	extra["sequence"] = h.vkSeq
	out, _ := json.Marshal(extra)
	h.sfu.Send(out)
	h.logFn("vk-joiner: -> %s", command)
}

func (h *VKHeadlessJoiner) vkSendTransmitData(participantId int64, payload map[string]interface{}) {
	h.vkMu.Lock()
	defer h.vkMu.Unlock()
	if h.sfu == nil {
		return
	}
	h.vkSeq++
	payloadJSON, _ := json.Marshal(payload)
	out := fmt.Sprintf(`{"command":"transmit-data","sequence":%d,"participantId":%d,"data":%s}`,
		h.vkSeq, participantId, payloadJSON)
	h.sfu.Send([]byte(out))
}

func (h *VKHeadlessJoiner) readLoop() {
	h.vkMu.Lock()
	sfu := h.sfu
	h.vkMu.Unlock()
	if sfu == nil {
		return
	}
	for {
		msg, err := sfu.Recv()
		if err != nil {
			h.logFn("vk-joiner: WebTransport closed: %s", common.MaskError(err))
			h.Status.EmitStatus(common.StatusTunnelLost)
			return
		}
		if string(msg) == "ping" {
			sfu.Send([]byte("pong"))
			continue
		}
		h.handleVKMessage(msg)
	}
}

func (h *VKHeadlessJoiner) handleVKMessage(raw []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return
	}

	msgType, _ := msg["type"].(string)
	switch msgType {
	case "notification":
		notif, _ := msg["notification"].(string)
		switch notif {
		case "connection":
			h.handleConnection(msg)
		case "transmitted-data":
			data, _ := msg["data"].(map[string]interface{})
			if data != nil {
				if pid, ok := msg["participantId"].(float64); ok && h.remotePeerID == nil {
					h.onRegisteredPeer(int64(pid))
				}
				h.onTransmittedData(data)
			}
		case "registered-peer":
			if pid, ok := msg["participantId"].(float64); ok {
				h.onRegisteredPeer(int64(pid))
			}
		case "topology-changed":
			topo, _ := msg["topology"].(string)
			h.logFn("vk-joiner: topology: %s", topo)
			if topo != "" && topo != vkTopologyDirect {
				h.logFn("vk-joiner: %s topology -> closing transport to reconnect and recover DIRECT", topo)
				h.closeTransport()
			}
		case "participant-joined", "participant-added":
			h.logFn("vk-joiner: <- %s", notif)
		case "participant-left":
			h.logFn("vk-joiner: <- %s", notif)
		case "hungup":
			h.logFn("vk-joiner: peer hungup -> closing transport to reconnect")
			h.closeTransport()
		}

	case "response":
		seq, _ := msg["sequence"].(float64)
		h.logFn("vk-joiner: <- response seq=%d", int(seq))

	case "error":
		errMsg, _ := msg["message"].(string)
		errCode, _ := msg["error"].(string)
		h.logFn("vk-joiner: ERROR: %s %s", errCode, errMsg)
	}
}

func (h *VKHeadlessJoiner) handleConnection(msg map[string]interface{}) {
	if conv, ok := msg["conversation"].(map[string]interface{}); ok {
		topo, _ := conv["topology"].(string)
		h.logFn("vk-joiner: connection topology=%q", topo)
	}
	convParams, ok := msg["conversationParams"].(map[string]interface{})
	if !ok {
		return
	}
	turn, ok := convParams["turn"].(map[string]interface{})
	if !ok {
		return
	}
	urlsRaw, _ := turn["urls"].([]interface{})
	var urls []string
	for _, u := range urlsRaw {
		if s, ok := u.(string); ok {
			urls = append(urls, s)
		}
	}
	username, _ := turn["username"].(string)
	credential, _ := turn["credential"].(string)
	h.joinResp.TurnServer.URLs = urls
	h.joinResp.TurnServer.Username = username
	h.joinResp.TurnServer.Credential = credential
	h.logFn("vk-joiner: TURN from connection: %v", urls)

	if h.pc == nil {
		h.initPC()
	}
}

func (h *VKHeadlessJoiner) initPC() {
	var iceServers []webrtc.ICEServer
	if len(h.joinResp.StunServer.URLs) > 0 {
		iceServers = append(iceServers, webrtc.ICEServer{URLs: h.joinResp.StunServer.URLs})
	}
	if len(h.joinResp.TurnServer.URLs) > 0 {
		iceServers = append(iceServers, webrtc.ICEServer{
			URLs:       h.joinResp.TurnServer.URLs,
			Username:   h.joinResp.TurnServer.Username,
			Credential: h.joinResp.TurnServer.Credential,
		})
	}

	mode := h.authParams.TunnelMode

	settingEngine := webrtc.SettingEngine{}
	settingEngine.DisableCloseByDTLS(true)
	settingEngine.DetachDataChannels()
	if h.PCConfig != nil {
		h.PCConfig.ConfigureSettingEngine(&settingEngine)
	}
	headlessclient.ChromeWindows.WithDTLSMimicChrome13().ApplyWebRTC(&settingEngine)

	pc, err := webrtc.NewAPI(webrtc.WithSettingEngine(settingEngine)).NewPeerConnection(webrtc.Configuration{
		ICEServers: iceServers,
	})
	if err != nil {
		h.logFn("vk-joiner: failed to create PC: %v", err)
		return
	}
	h.pc = pc

	h.logFn("vk-joiner: tunnel mode: %s", mode)

	if mode == "video" {
		h.sampleTrack = h.AddTracks(pc, h.logFn, "vk-joiner")
	}

	negotiated := true
	dcID := uint16(2)
	dc, err := pc.CreateDataChannel("tunnel", &webrtc.DataChannelInit{
		Negotiated: &negotiated,
		ID:         &dcID,
	})
	if err != nil {
		h.logFn("vk-joiner: warning: could not create tunnel DC: %v", err)
	} else {
		h.dc = dc
		dc.OnOpen(func() {
			h.logFn("vk-joiner: tunnel DC open")
			if mode == "dc" {
				h.reconnectAttempt.Store(0)
				h.logFn("vk-joiner: === DC TUNNEL CONNECTED ===")
				h.Status.EmitStatus(common.StatusTunnelConnected)
				if h.OnConnected != nil {
					h.OnConnected(tunnel.NewDCTunnel(dc, h.obf, common.RTPBufSize, h.logFn))
				}
			}
		})
		dc.OnClose(func() {
			h.logFn("vk-joiner: tunnel DC closed")
		})
	}

	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}
		h.onLocalICECandidate(candidate)
	})
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		h.logFn("vk-joiner: PC state: %s", state.String())
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateDisconnected {
			h.logFn("vk-joiner: PC %s, closing transport to trigger reconnect", state.String())
			h.closeTransport()
		}
		if mode == "video" && state == webrtc.PeerConnectionStateConnected && h.vp8tunnel == nil {
			h.reconnectAttempt.Store(0)
			h.logFn("vk-joiner: === TUNNEL CONNECTED ===")
			h.Status.EmitStatus(common.StatusTunnelConnected)
			h.vp8tunnel = tunnel.NewVP8DataTunnel(h.sampleTrack, h.obf, h.logFn)
			h.vp8tunnel.Start(h.vp8FPS, h.vp8Batch)
			var downlink tunnel.DataTunnel = h.vp8tunnel
			trackCount := 1
			if h.dualTrack {
				writer := tunnel.NewScreenWriter(h.obf, "screen-up", h.logFn)
				writer.Reconfigure(h.vp8tunnel.FPS(), h.vp8tunnel.Batch())
				writer.SetSend(h.producerScreen.send)
				h.sym = tunnel.NewSymmetricScreenTunnel(h.vp8tunnel, writer, h.obf, h.producerScreen.ready, h.logFn)
				h.sym.SetTrackCount(2)
				downlink = h.sym
				trackCount = 2
				h.logFn("vk-joiner: === SYMMETRIC DUAL-TRACK: camera VP8 + screen DCs ===")
			}
			vp8tun := h.vp8tunnel
			if !h.configAck.acknowledged() {
				acked, cancel := h.configAck.arm()
				go sendVP8ConfigUntilAcked(acked, cancel, h.stopCh, vp8tun,
					vp8tun.FPS(), vp8tun.Batch(), trackCount, h.logFn, "vk-joiner")
				h.logFn("vk-joiner: pushed vp8 config to creator fps=%d batch=%d trackCount=%d", vp8tun.FPS(), vp8tun.Batch(), trackCount)
			}
			if h.OnConnected != nil {
				h.OnConnected(downlink)
			}
		}
	})
	if mode == "video" {
		pc.OnDataChannel(func(dc *webrtc.DataChannel) {
			h.logFn("vk-joiner: remote DataChannel: label=%q id=%v", dc.Label(), dc.ID())
			if !h.dualTrack {
				return
			}
			switch dc.Label() {
			case "consumerScreenShare":
				readScreenDataChannel(dc, func(frame []byte) {
					if h.sym != nil {
						h.sym.HandleScreenFrame(frame)
					}
				}, h.logFn)
			case "producerScreenShare":
				attachScreenWriterDC(dc, h.producerScreen.attach, h.logFn)
			}
		})
		pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
			h.logFn("vk-joiner: remote track: codec=%s ssrc=%d", track.Codec().MimeType, track.SSRC())
			go h.ReadTrackFn(track, func(frame []byte) {
				if h.vp8tunnel != nil {
					h.vp8tunnel.HandleFrame(frame)
				}
			}, h.logFn, "vk-joiner")
		})
	}

	h.logFn("vk-joiner: PC ready, waiting for remote offer")
}

func (h *VKHeadlessJoiner) onRegisteredPeer(pid int64) {
	h.remotePeerID = &pid
	h.logFn("vk-joiner: peer registered: %d", pid)
}

func (h *VKHeadlessJoiner) onLocalICECandidate(candidate *webrtc.ICECandidate) {
	if h.remotePeerID == nil {
		return
	}
	candidateJSON := candidate.ToJSON()
	raw, _ := json.Marshal(candidateJSON)
	var parsed interface{}
	json.Unmarshal(raw, &parsed)
	h.vkSendTransmitData(*h.remotePeerID, map[string]interface{}{"candidate": parsed})
}

func (h *VKHeadlessJoiner) onTransmittedData(data map[string]interface{}) {
	if h.pc == nil {
		return
	}

	if candidate, ok := data["candidate"]; ok {
		candidateJSON, _ := json.Marshal(candidate)
		var candidateInit webrtc.ICECandidateInit
		json.Unmarshal(candidateJSON, &candidateInit)
		if h.OnRemoteCandidate != nil {
			h.OnRemoteCandidate(0, candidateInit.Candidate)
		}
		if h.remoteSet {
			h.pc.AddICECandidate(candidateInit)
		} else {
			h.pendingICE = append(h.pendingICE, candidateInit)
		}
	}

	if sdp, ok := data["sdp"].(map[string]interface{}); ok {
		sdpType, _ := sdp["type"].(string)
		sdpStr, _ := sdp["sdp"].(string)
		if h.OnRemoteCandidate != nil {
			h.OnRemoteCandidate(-1, sdpStr)
		}
		h.logFn("vk-joiner: remote SDP: %s", sdpType)

		if sdpType == "answer" {
			h.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: sdpStr})
			h.remoteSet = true
			for _, candidate := range h.pendingICE {
				h.pc.AddICECandidate(candidate)
			}
			h.pendingICE = nil
		} else if sdpType == "offer" {
			h.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: sdpStr})
			h.remoteSet = true
			for _, candidate := range h.pendingICE {
				h.pc.AddICECandidate(candidate)
			}
			h.pendingICE = nil

			answer, err := h.pc.CreateAnswer(nil)
			if err != nil || h.remotePeerID == nil {
				h.logFn("vk-joiner: create answer failed: %v", err)
				return
			}
			h.pc.SetLocalDescription(answer)
			sdpJSON, _ := json.Marshal(answer.SDP)
			h.vkMu.Lock()
			if h.sfu != nil {
				h.vkSeq++
				raw := fmt.Sprintf(`{"command":"transmit-data","sequence":%d,"participantId":%d,"data":{"sdp":{"sdp":%s,"type":%q},"animojiVersion":2},"participantType":"USER"}`,
					h.vkSeq, *h.remotePeerID, sdpJSON, answer.Type.String())
				h.sfu.Send([]byte(raw))
				h.logFn("vk-joiner: -> answer (seq=%d)", h.vkSeq)
			}
			h.vkMu.Unlock()
		}
	}
}
