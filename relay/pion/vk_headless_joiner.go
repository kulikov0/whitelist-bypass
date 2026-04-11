package pion

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
	"whitelist-bypass/relay/common"
	"whitelist-bypass/relay/tunnel"
)

func init() {
	// Android doesn't have /etc/resolv.conf or standard CA paths.
	// Point Go's x509 to the system CA store so TLS verification works.
	if _, err := os.Stat("/system/etc/security/cacerts"); err == nil {
		os.Setenv("SSL_CERT_DIR", "/system/etc/security/cacerts")
	}
}

const headlessUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
const sctpReceiveBufferSize = 4 * 1024 * 1024

type VKHeadlessAuthParams struct {
	SessionKey      string `json:"sessionKey"`
	ApplicationKey  string `json:"applicationKey"`
	APIBaseURL      string `json:"apiBaseURL"`
	JoinLink        string `json:"joinLink"`
	AnonymToken     string `json:"anonymToken"`
	AppVersion      string `json:"appVersion"`
	ProtocolVersion string `json:"protocolVersion"`
	TunnelMode      string `json:"tunnelMode"`
}

type VKJoinResponse struct {
	Endpoint   string `json:"endpoint"`
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

	authParams   *VKHeadlessAuthParams
	joinResp     *VKJoinResponse
	vkWs         *websocket.Conn
	vkMu         sync.Mutex
	vkSeq        int
	remotePeerID *int64

	pc          *webrtc.PeerConnection
	sampleTrack *webrtc.TrackLocalStaticSample
	dc          *webrtc.DataChannel
	vp8tunnel   *tunnel.VP8DataTunnel
	remoteSet   bool
	pendingICE  []webrtc.ICECandidateInit
}

func NewVKHeadlessJoiner(logFn func(string, ...any)) *VKHeadlessJoiner {
	if logFn == nil {
		logFn = log.Printf
	}
	return &VKHeadlessJoiner{logFn: logFn}
}

func (h *VKHeadlessJoiner) DC() *webrtc.DataChannel {
	return h.dc
}

var stdinReader = bufio.NewReader(os.Stdin)
var stdinMu sync.Mutex

func readStdinLine() (string, error) {
	stdinMu.Lock()
	defer stdinMu.Unlock()
	line, err := stdinReader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func requestResolve(hostname string) (string, error) {
	stdinMu.Lock()
	fmt.Printf("RESOLVE:%s\n", hostname)
	stdinMu.Unlock()
	line, err := readStdinLine()
	if err != nil {
		return "", fmt.Errorf("read resolve response: %w", err)
	}
	if line == "" {
		return "", fmt.Errorf("empty resolve for %s", hostname)
	}
	return line, nil
}

func (h *VKHeadlessJoiner) Run() {
	h.logFn("headless: waiting for auth params on stdin...")
	common.EmitStatus(common.StatusReady)
	for {
		line, err := readStdinLine()
		if err != nil {
			h.logFn("headless: stdin closed: %v", err)
			return
		}
		if strings.HasPrefix(line, "JOIN:") {
			jsonData := strings.TrimPrefix(line, "JOIN:")
			var params VKHeadlessAuthParams
			if err := json.Unmarshal([]byte(jsonData), &params); err != nil {
				h.logFn("headless: failed to parse auth params: %v", err)
				continue
			}
			h.authParams = &params
			h.logFn("headless: auth params received")
			h.logFn("headless:   appVersion=%s protocolVersion=%s", params.AppVersion, params.ProtocolVersion)
			common.EmitStatus(common.StatusConnecting)

			if err := h.joinCall(); err != nil {
				h.logFn("headless: joinCall failed: %v", err)
				common.EmitStatusError(err.Error())
				return
			}
			h.connectSFU()
			return
		}
	}
}

func (h *VKHeadlessJoiner) joinCall() error {
	apiURL := h.authParams.APIBaseURL
	parsed, err := url.Parse(apiURL)
	if err != nil {
		return fmt.Errorf("bad apiBaseURL: %w", err)
	}
	resolvedIP, err := requestResolve(parsed.Hostname())
	if err != nil {
		return fmt.Errorf("resolve %s: %w", parsed.Hostname(), err)
	}
	h.logFn("headless: resolved %s -> %s", parsed.Hostname(), resolvedIP)

	body := url.Values{
		"method":          {"vchat.joinConversationByLink"},
		"session_key":     {h.authParams.SessionKey},
		"application_key": {h.authParams.ApplicationKey},
		"joinLink":        {h.authParams.JoinLink},
		"anonymToken":     {h.authParams.AnonymToken},
		"isVideo":         {"true"},
		"isAudio":         {"false"},
		"mediaSettings":   {`{"isAudioEnabled":false,"isVideoEnabled":true,"isScreenSharingEnabled":false}`},
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
	req.Header.Set("User-Agent", headlessUA)

	h.logFn("headless: calling joinConversationByLink...")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("joinConversationByLink: %w", err)
	}
	defer resp.Body.Close()

	var joinResp VKJoinResponse
	if err := json.NewDecoder(resp.Body).Decode(&joinResp); err != nil {
		return fmt.Errorf("decode join response: %w", err)
	}
	if joinResp.Endpoint == "" {
		return fmt.Errorf("empty endpoint in join response")
	}

	h.joinResp = &joinResp
	h.logFn("headless: joined, turn=%v", joinResp.TurnServer.URLs)
	return nil
}

func (h *VKHeadlessJoiner) connectSFU() {
	parsed, err := url.Parse(h.joinResp.Endpoint)
	if err != nil {
		h.logFn("headless: bad endpoint URL: %v", err)
		return
	}

	hostname := parsed.Hostname()
	resolvedIP, err := requestResolve(hostname)
	if err != nil {
		h.logFn("headless: DNS resolve failed: %v", err)
		return
	}
	h.logFn("headless: resolved %s -> %s", hostname, resolvedIP)

	wsURL := h.joinResp.Endpoint +
		"&platform=WEB" +
		"&appVersion=" + h.authParams.AppVersion +
		"&version=" + h.authParams.ProtocolVersion +
		"&device=browser&capabilities=0&clientType=VK&tgt=join"

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		WriteBufferSize:  65536,
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: true, ServerName: hostname},
		NetDialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			_, port, _ := net.SplitHostPort(addr)
			return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, resolvedIP+":"+port)
		},
	}

	header := http.Header{}
	header.Set("User-Agent", headlessUA)
	header.Set("Origin", "https://vk.com")

	ws, _, err := dialer.Dial(wsURL, header)
	if err != nil {
		h.logFn("headless: WS connect failed: %v", err)
		return
	}
	h.vkMu.Lock()
	h.vkWs = ws
	h.vkSeq = 0
	h.vkMu.Unlock()
	h.logFn("headless: WS connected")

	h.vkSend("update-media-modifiers", map[string]interface{}{
		"mediaModifiers": map[string]interface{}{"denoise": true, "denoiseAnn": true},
	})
	h.vkSend("change-media-settings", map[string]interface{}{
		"mediaSettings": map[string]interface{}{
			"isAudioEnabled": false, "isVideoEnabled": true,
			"isScreenSharingEnabled": false, "isFastScreenSharingEnabled": false,
			"isAudioSharingEnabled": false, "isAnimojiEnabled": false,
		},
	})

	go h.pingLoop()
	h.readLoop()
}

func (h *VKHeadlessJoiner) vkSend(command string, extra map[string]interface{}) {
	h.vkMu.Lock()
	defer h.vkMu.Unlock()
	if h.vkWs == nil {
		return
	}
	h.vkSeq++
	extra["command"] = command
	extra["sequence"] = h.vkSeq
	out, _ := json.Marshal(extra)
	h.vkWs.WriteMessage(websocket.TextMessage, out)
	h.logFn("headless: -> %s", command)
}

func (h *VKHeadlessJoiner) vkSendTransmitData(participantId int64, payload map[string]interface{}) {
	h.vkMu.Lock()
	defer h.vkMu.Unlock()
	if h.vkWs == nil {
		return
	}
	h.vkSeq++
	payloadJSON, _ := json.Marshal(payload)
	out := fmt.Sprintf(`{"command":"transmit-data","sequence":%d,"participantId":%d,"data":%s}`,
		h.vkSeq, participantId, payloadJSON)
	h.vkWs.WriteMessage(websocket.TextMessage, []byte(out))
}

func (h *VKHeadlessJoiner) pingLoop() {
	for {
		time.Sleep(15 * time.Second)
		h.vkMu.Lock()
		ws := h.vkWs
		h.vkMu.Unlock()
		if ws == nil {
			return
		}
		h.vkMu.Lock()
		ws.WriteMessage(websocket.PingMessage, nil)
		h.vkMu.Unlock()
	}
}

func (h *VKHeadlessJoiner) readLoop() {
	for {
		_, msg, err := h.vkWs.ReadMessage()
		if err != nil {
			h.logFn("headless: WS closed: %v", err)
			return
		}
		if string(msg) == "ping" {
			h.vkMu.Lock()
			h.vkWs.WriteMessage(websocket.TextMessage, []byte("pong"))
			h.vkMu.Unlock()
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
			h.logFn("headless: topology: %s", topo)
			if topo == "server" {
				h.logFn("headless: ERROR: server topology not supported")
			}
		case "participant-joined", "participant-added":
			h.logFn("headless: <- %s", notif)
		case "participant-left":
			h.logFn("headless: <- %s: %s", notif, string(raw))
		case "hungup":
			h.logFn("headless: <- %s: %s", notif, string(raw))
			h.logFn("headless: ERROR: call ended (hungup)")
			common.EmitStatusError("call ended")
		}

	case "response":
		seq, _ := msg["sequence"].(float64)
		h.logFn("headless: <- response seq=%d", int(seq))

	case "error":
		errMsg, _ := msg["message"].(string)
		errCode, _ := msg["error"].(string)
		h.logFn("headless: ERROR: %s %s", errCode, errMsg)
	}
}

func (h *VKHeadlessJoiner) handleConnection(msg map[string]interface{}) {
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
	h.logFn("headless: TURN from connection: %v", urls)

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
	settingEngine.SetNet(&AndroidNet{})
	settingEngine.DisableCloseByDTLS(true)
	settingEngine.DetachDataChannels()

	pc, err := webrtc.NewAPI(webrtc.WithSettingEngine(settingEngine)).NewPeerConnection(webrtc.Configuration{
		ICEServers: iceServers,
	})
	if err != nil {
		h.logFn("headless: failed to create PC: %v", err)
		return
	}
	h.pc = pc

	h.logFn("headless: tunnel mode: %s", mode)

	if mode == "video" {
		h.sampleTrack = AddTunnelTracks(pc, h.logFn, "headless")
	}

	negotiated := true
	dcID := uint16(2)
	dc, err := pc.CreateDataChannel("tunnel", &webrtc.DataChannelInit{
		Negotiated: &negotiated,
		ID:         &dcID,
	})
	if err != nil {
		h.logFn("headless: warning: could not create tunnel DC: %v", err)
	} else {
		h.dc = dc
		dc.OnOpen(func() {
			h.logFn("headless: tunnel DC open")
			if mode == "dc" {
				h.logFn("headless: === DC TUNNEL CONNECTED ===")
				common.EmitStatus(common.StatusTunnelConnected)
				if h.OnConnected != nil {
					h.OnConnected(tunnel.NewDCTunnel(dc, common.RTPBufSize, h.logFn))
				}
			}
		})
		dc.OnClose(func() {
			h.logFn("headless: tunnel DC closed")
		})
	}

	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}
		h.onLocalICECandidate(candidate)
	})
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		h.logFn("headless: PC state: %s", state.String())
		if state == webrtc.PeerConnectionStateFailed {
			h.logFn("headless: ERROR: connection failed")
			common.EmitStatusError("connection failed")
		} else if state == webrtc.PeerConnectionStateDisconnected {
			h.logFn("headless: ERROR: connection lost")
			common.EmitStatus(common.StatusTunnelLost)
		}
		if mode == "video" && state == webrtc.PeerConnectionStateConnected && h.vp8tunnel == nil {
			h.logFn("headless: === TUNNEL CONNECTED ===")
			common.EmitStatus(common.StatusTunnelConnected)
			h.vp8tunnel = tunnel.NewVP8DataTunnel(h.sampleTrack, h.logFn)
			h.vp8tunnel.Start(25)
			if h.OnConnected != nil {
				h.OnConnected(h.vp8tunnel)
			}
		}
	})
	if mode == "video" {
		pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
			h.logFn("headless: remote track: %s", track.Codec().MimeType)
			go ReadTrack(track, h.vp8tunnel, h.logFn, "headless")
		})
	}

	h.logFn("headless: PC ready, waiting for remote offer")
}

func (h *VKHeadlessJoiner) onRegisteredPeer(pid int64) {
	h.remotePeerID = &pid
	h.logFn("headless: peer registered: %d", pid)
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
		if h.remoteSet {
			h.pc.AddICECandidate(candidateInit)
		} else {
			h.pendingICE = append(h.pendingICE, candidateInit)
		}
	}

	if sdp, ok := data["sdp"].(map[string]interface{}); ok {
		sdpType, _ := sdp["type"].(string)
		sdpStr, _ := sdp["sdp"].(string)
		h.logFn("headless: remote SDP: %s", sdpType)

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
				h.logFn("headless: create answer failed: %v", err)
				return
			}
			h.pc.SetLocalDescription(answer)
			sdpJSON, _ := json.Marshal(answer.SDP)
			h.vkMu.Lock()
			if h.vkWs != nil {
				h.vkSeq++
				raw := fmt.Sprintf(`{"command":"transmit-data","sequence":%d,"participantId":%d,"data":{"sdp":{"sdp":%s,"type":%q},"animojiVersion":2},"participantType":"USER"}`,
					h.vkSeq, *h.remotePeerID, sdpJSON, answer.Type.String())
				h.vkWs.WriteMessage(websocket.TextMessage, []byte(raw))
				h.logFn("headless: -> answer (seq=%d)", h.vkSeq)
			}
			h.vkMu.Unlock()
		}
	}
}
