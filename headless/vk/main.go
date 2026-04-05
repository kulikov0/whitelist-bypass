package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
	"headless-creator/tunnel"
)

const TopologyDirect = "DIRECT"

type CallInfo struct {
	CallID     string
	JoinLink   string
	ShortLink  string
	OKJoinLink string
	TurnServer TurnServer
	StunServer StunServer
	WSEndpoint string
}

type TurnServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username"`
	Credential string   `json:"credential"`
}

type StunServer struct {
	URLs []string `json:"urls"`
}

type VKTokenResponse struct {
	Data struct {
		AccessToken string `json:"access_token"`
	} `json:"data"`
}

type CallSettingsResponse struct {
	Response struct {
		Settings struct {
			PublicKey string `json:"public_key"`
		} `json:"settings"`
	} `json:"response"`
}

type CallTokenResponse struct {
	Response struct {
		Token      string `json:"token"`
		APIBaseURL string `json:"api_base_url"`
	} `json:"response"`
}

type OKAuthResponse struct {
	SessionKey string `json:"session_key"`
}

type JoinResponse struct {
	Endpoint   string     `json:"endpoint"`
	TurnServer TurnServer `json:"turn_server"`
	StunServer StunServer `json:"stun_server"`
}

type Bridge struct {
	mu         sync.Mutex
	vkWs       *websocket.Conn
	vkSeq      int
	iceServers []webrtc.ICEServer
	topology   string
	peers      map[int64]struct{}
	relay      Relay
	newRelay   func() Relay
	p2p        *P2PHandler
}

func httpPost(endpoint string, form url.Values, extraHeaders map[string]string) ([]byte, error) {
	body := form.Encode()
	req, err := http.NewRequest("POST", endpoint, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", tunnel.UserAgent)
	req.Header.Set("Origin", "https://vk.com")
	req.Header.Set("Referer", "https://vk.com/")
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func httpGet(endpoint string) ([]byte, error) {
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", tunnel.UserAgent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if strings.Contains(resp.Request.URL.String(), "challenge") {
		return nil, fmt.Errorf("VK captcha required - open %s in browser and solve it", resp.Request.URL.String())
	}
	return io.ReadAll(resp.Body)
}

func authAndJoin(cookieStr, okJoinLink string, cfg VKConfig) (*JoinResponse, error) {
	auth := func(bearer string) map[string]string {
		return map[string]string{"Authorization": "Bearer " + bearer}
	}

	r, err := httpPost("https://login.vk.com/?act=web_token",
		url.Values{"version": {"1"}, "app_id": {cfg.AppID}},
		map[string]string{"Cookie": cookieStr})
	if err != nil {
		return nil, fmt.Errorf("web_token: %w", err)
	}
	var tok VKTokenResponse
	json.Unmarshal(r, &tok)
	if tok.Data.AccessToken == "" {
		return nil, fmt.Errorf("empty VK token, response: %s", string(r))
	}

	r, err = httpPost("https://api.vk.com/method/calls.getSettings",
		url.Values{"v": {cfg.APIVersion}}, auth(tok.Data.AccessToken))
	if err != nil {
		return nil, fmt.Errorf("calls.getSettings: %w", err)
	}
	var settings CallSettingsResponse
	json.Unmarshal(r, &settings)
	appKey := settings.Response.Settings.PublicKey
	if appKey == "" {
		return nil, fmt.Errorf("empty public_key, response: %s", string(r))
	}

	r, err = httpPost("https://api.vk.com/method/messages.getCallToken",
		url.Values{"v": {cfg.APIVersion}, "env": {"production"}}, auth(tok.Data.AccessToken))
	if err != nil {
		return nil, fmt.Errorf("messages.getCallToken: %w", err)
	}
	var callToken CallTokenResponse
	json.Unmarshal(r, &callToken)
	if callToken.Response.Token == "" {
		return nil, fmt.Errorf("empty call token, response: %s", string(r))
	}
	if callToken.Response.APIBaseURL == "" {
		return nil, fmt.Errorf("empty api_base_url, response: %s", string(r))
	}

	apiBaseURL := strings.TrimRight(callToken.Response.APIBaseURL, "/")
	if !strings.HasSuffix(apiBaseURL, "/fb.do") {
		apiBaseURL += "/fb.do"
	}
	sd, _ := json.Marshal(map[string]interface{}{
		"device_id": "headless-go-1", "client_version": cfg.AppVersion,
		"client_type": "SDK_JS", "auth_token": callToken.Response.Token, "version": 3,
	})
	r, err = httpPost(apiBaseURL, url.Values{
		"method": {"auth.anonymLogin"}, "application_key": {appKey},
		"format": {"json"}, "session_data": {string(sd)},
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("auth.anonymLogin: %w", err)
	}
	var okAuth OKAuthResponse
	json.Unmarshal(r, &okAuth)
	if okAuth.SessionKey == "" {
		return nil, fmt.Errorf("empty session_key, response: %s", string(r))
	}

	ms, _ := json.Marshal(map[string]bool{
		"isAudioEnabled": false, "isVideoEnabled": true, "isScreenSharingEnabled": false,
	})
	r, err = httpPost(apiBaseURL, url.Values{
		"method": {"vchat.joinConversationByLink"}, "session_key": {okAuth.SessionKey},
		"application_key": {appKey}, "format": {"json"}, "joinLink": {okJoinLink},
		"isVideo": {"true"}, "isAudio": {"false"}, "mediaSettings": {string(ms)},
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("vchat.joinConversationByLink: %w", err)
	}
	var joinResp JoinResponse
	json.Unmarshal(r, &joinResp)
	if joinResp.Endpoint == "" {
		return nil, fmt.Errorf("empty WS endpoint, response: %s", string(r))
	}
	return &joinResp, nil
}

func createAndJoinCall(cookieStr, peerId string, cfg VKConfig) (*CallInfo, error) {
	if cfg.AppID == "" || cfg.APIVersion == "" {
		return nil, fmt.Errorf("config incomplete: app_id=%q api=%q", cfg.AppID, cfg.APIVersion)
	}

	auth := func(bearer string) map[string]string {
		return map[string]string{"Authorization": "Bearer " + bearer}
	}

	log.Println("[auth] Getting VK token...")
	r, err := httpPost("https://login.vk.com/?act=web_token",
		url.Values{"version": {"1"}, "app_id": {cfg.AppID}},
		map[string]string{"Cookie": cookieStr})
	if err != nil {
		return nil, fmt.Errorf("web_token: %w", err)
	}
	var tok VKTokenResponse
	json.Unmarshal(r, &tok)
	vkToken := tok.Data.AccessToken
	if vkToken == "" {
		return nil, fmt.Errorf("empty VK token, response: %s", string(r))
	}

	log.Printf("[auth] Creating call peer_id=%s...", peerId)
	r, err = httpPost("https://api.vk.com/method/calls.start",
		url.Values{"v": {cfg.APIVersion}, "peer_id": {peerId}}, auth(vkToken))
	if err != nil {
		return nil, fmt.Errorf("calls.start: %w", err)
	}
	var call struct {
		Response struct {
			CallID           string `json:"call_id"`
			JoinLink         string `json:"join_link"`
			OKJoinLink       string `json:"ok_join_link"`
			ShortCredentials struct {
				LinkWithPassword string `json:"link_with_password"`
			} `json:"short_credentials"`
		} `json:"response"`
	}
	json.Unmarshal(r, &call)
	c := call.Response
	if c.CallID == "" {
		return nil, fmt.Errorf("empty call_id, response: %s", string(r))
	}
	if c.OKJoinLink == "" {
		return nil, fmt.Errorf("empty ok_join_link, response: %s", string(r))
	}
	log.Printf("[auth] call_id: %s", c.CallID)
	log.Printf("[auth] join_link: %s", c.JoinLink)

	log.Println("[auth] Joining conversation...")
	joinResp, err := authAndJoin(cookieStr, c.OKJoinLink, cfg)
	if err != nil {
		return nil, err
	}

	return &CallInfo{
		CallID: c.CallID, JoinLink: c.JoinLink, ShortLink: c.ShortCredentials.LinkWithPassword,
		OKJoinLink: c.OKJoinLink, TurnServer: joinResp.TurnServer, StunServer: joinResp.StunServer,
		WSEndpoint: joinResp.Endpoint,
	}, nil
}

func (b *Bridge) vkSend(command string, extra map[string]interface{}) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.vkWs == nil {
		return
	}
	b.vkSeq++
	seq := b.vkSeq
	// VK SFU requires command, sequence, participantId before data
	var out []byte
	if pid, ok := extra["participantId"]; ok {
		dataJSON, _ := json.Marshal(extra["data"])
		out = []byte(fmt.Sprintf(`{"command":%q,"sequence":%d,"participantId":%v,"data":%s}`,
			command, seq, pid, dataJSON))
	} else {
		// Non-transmit-data commands: just marshal normally with command+sequence first
		extra["command"] = command
		extra["sequence"] = seq
		out, _ = json.Marshal(extra)
	}
	b.vkWs.WriteMessage(websocket.TextMessage, out)
	log.Printf("[vk-ws] -> %s", command)
}

func (b *Bridge) handleVKMessage(raw []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return
	}

	msgType, _ := msg["type"].(string)
	switch msgType {
	case "notification":
		notif, _ := msg["notification"].(string)
		log.Printf("[vk-ws] <- notification: %s", notif)

		switch notif {
		case "connection":
			log.Println("[vk-ws]    TURN creds received")

		case "transmitted-data":
			data, _ := msg["data"].(map[string]interface{})
			if data != nil && b.topology == TopologyDirect && b.p2p != nil {
				b.p2p.OnTransmittedData(data)
			}

		case "registered-peer":
			pid, _ := msg["participantId"].(float64)
			if b.topology == TopologyDirect && b.p2p != nil {
				b.p2p.OnRegisteredPeer(int64(pid))
			}

		case "topology-changed":
			topo, _ := msg["topology"].(string)
			log.Printf("[vk-ws]    Topology changed to %s", topo)
			b.topology = topo
			if topo != TopologyDirect {
				log.Printf("[vk-ws]    SFU not supported, kicking %d peers", len(b.peers))
				for pid := range b.peers {
					b.vkSend("remove-participant", map[string]interface{}{
						"participantId": pid,
						"ban":           false,
					})
				}
			}

		case "participant-joined", "participant-added":
			if pid, ok := msg["participantId"].(float64); ok {
				b.peers[int64(pid)] = struct{}{}
				log.Printf("[vk-ws]    Participant %d joined (total: %d)", int64(pid), len(b.peers))
				if b.topology != TopologyDirect {
					log.Printf("[vk-ws]    Kicking peer %d (SFU topology)", int64(pid))
					b.vkSend("remove-participant", map[string]interface{}{
						"participantId": int64(pid),
						"ban":           false,
					})
					log.Println("[FATAL] SFU topology is not supported, exiting")
					os.Exit(1)
				}
			}

		case "participant-left":
			if pid, ok := msg["participantId"].(float64); ok {
				delete(b.peers, int64(pid))
				log.Printf("[vk-ws]    Participant %d left (total: %d)", int64(pid), len(b.peers))
			}

		case "hungup":
			if pid, ok := msg["participantId"].(float64); ok {
				delete(b.peers, int64(pid))
				log.Printf("[vk-ws]    Participant %d hung up (total: %d)", int64(pid), len(b.peers))
			} else {
				log.Println("[vk-ws]    Participant hung up")
			}

		case "closed-conversation":
			reason, _ := msg["reason"].(string)
			log.Printf("[vk-ws]    Conversation closed: %s", reason)
			b.mu.Lock()
			if b.vkWs != nil {
				b.vkWs.Close()
			}
			b.mu.Unlock()

		default:
			snippet, _ := json.Marshal(msg)
			if len(snippet) > 1000 {
				snippet = append(snippet[:1000], '.', '.', '.')
			}
			log.Printf("[vk-ws]    unhandled: %s", string(snippet))
		}

	case "response":
		seq, _ := msg["sequence"].(float64)
		snippet, _ := json.Marshal(msg)
		if len(snippet) > 1000 {
			snippet = append(snippet[:1000], '.', '.', '.')
		}
		log.Printf("[vk-ws] <- response seq=%d: %s", int(seq), string(snippet))

	case "error":
		errMsg, _ := msg["message"].(string)
		errCode, _ := msg["error"].(string)
		log.Printf("[vk-ws] <- error: %s %s", errCode, errMsg)
	}
}

func buildICEServers(callInfo *CallInfo) []webrtc.ICEServer {
	var servers []webrtc.ICEServer
	if len(callInfo.StunServer.URLs) > 0 {
		servers = append(servers, webrtc.ICEServer{URLs: callInfo.StunServer.URLs})
	}
	if len(callInfo.TurnServer.URLs) > 0 {
		urls := append([]string{}, callInfo.TurnServer.URLs...)
		urls = append(urls, urls[len(urls)-1]+"?transport=tcp")
		servers = append(servers, webrtc.ICEServer{
			URLs: urls, Username: callInfo.TurnServer.Username, Credential: callInfo.TurnServer.Credential,
		})
	}
	return servers
}

func (b *Bridge) connectVKWs(wsURL string) error {
	vkHeader := http.Header{}
	vkHeader.Set("User-Agent", tunnel.UserAgent)
	vkHeader.Set("Origin", "https://vk.com")
	vkDialer := websocket.Dialer{WriteBufferSize: tunnel.RTPBufSize}
	vkWs, _, err := vkDialer.Dial(wsURL, vkHeader)
	if err != nil {
		return err
	}
	b.mu.Lock()
	b.vkWs = vkWs
	b.vkSeq = 0
	b.mu.Unlock()
	return nil
}

func (b *Bridge) initRelay() {
	if b.relay != nil {
		b.relay.Close()
	}
	b.topology = TopologyDirect
	b.peers = make(map[int64]struct{})
	b.relay = b.newRelay()
	b.p2p = NewP2PHandler(b)
	b.p2p.Init()
}

func (b *Bridge) readLoop() error {
	for {
		_, msg, err := b.vkWs.ReadMessage()
		if err != nil {
			return err
		}
		if string(msg) == "ping" {
			b.mu.Lock()
			b.vkWs.WriteMessage(websocket.TextMessage, []byte("pong"))
			b.mu.Unlock()
			continue
		}
		b.handleVKMessage(msg)
	}
}

func (b *Bridge) run(callInfo *CallInfo, cookieStr string, cfg VKConfig) {
	fmt.Println("")
	fmt.Println("  CALL CREATED")
	fmt.Println("  join_link:", callInfo.JoinLink)
	if callInfo.ShortLink != "" {
		fmt.Println("  short:    ", callInfo.ShortLink)
	}
	fmt.Println("  TURN:     ", strings.Join(callInfo.TurnServer.URLs, ", "))
	fmt.Printf("  protocol:  v%s sdk %s\n\n", cfg.ProtocolVersion, cfg.SDKVersion)

	b.iceServers = buildICEServers(callInfo)
	wsEndpoint := callInfo.WSEndpoint

	makeWSURL := func(ep string) string {
		return ep +
			"&platform=WEB" +
			"&appVersion=" + cfg.AppVersion +
			"&version=" + cfg.ProtocolVersion +
			"&device=browser&capabilities=0&clientType=VK&tgt=join"
	}

	go func() {
		for {
			time.Sleep(15 * time.Second)
			b.mu.Lock()
			ws := b.vkWs
			b.mu.Unlock()
			if ws != nil {
				b.mu.Lock()
				ws.WriteMessage(websocket.PingMessage, nil)
				b.mu.Unlock()
			}
		}
	}()

	for {
		b.initRelay()

		log.Println("[vk-ws] Connecting...")
		if err := b.connectVKWs(makeWSURL(wsEndpoint)); err != nil {
			log.Printf("[vk-ws] Connect failed: %v, retrying in 5s...", err)
			time.Sleep(5 * time.Second)
			continue
		}
		log.Println("[vk-ws] Connected")

		b.vkSend("change-media-settings", map[string]interface{}{
			"mediaSettings": map[string]interface{}{
				"isAudioEnabled": false, "isVideoEnabled": true,
				"isScreenSharingEnabled": false, "isFastScreenSharingEnabled": false,
				"isAudioSharingEnabled": false, "isAnimojiEnabled": false,
			},
		})

		err := b.readLoop()
		log.Printf("[vk-ws] Closed: %v", err)

		b.mu.Lock()
		b.vkWs = nil
		b.mu.Unlock()

		log.Println("[vk-ws] Rejoining in 3s...")
		time.Sleep(3 * time.Second)

		joinResp, rerr := authAndJoin(cookieStr, callInfo.OKJoinLink, cfg)
		if rerr != nil {
			log.Printf("[rejoin] Failed: %v, retrying in 5s...", rerr)
			time.Sleep(5 * time.Second)
			continue
		}
		wsEndpoint = joinResp.Endpoint
		callInfo.TurnServer = joinResp.TurnServer
		callInfo.StunServer = joinResp.StunServer
		b.iceServers = buildICEServers(callInfo)
	}
}

func main() {
	cookiesPath := flag.String("cookies", "", "path to cookies.json")
	cookieString := flag.String("cookie-string", "", "raw cookie string (name=val; name=val)")
	peerId := flag.String("peer-id", "", "VK peer_id for the call")
	resources := flag.String("resources", "default", "resource mode: default, moderate, unlimited")
	flag.Parse()

	var readBuf int
	var maxDCBuf uint64
	var memLimit int64
	switch *resources {
	case "moderate":
		readBuf = 16384
		maxDCBuf = 1 * 1024 * 1024
		memLimit = 64 * 1024 * 1024
	case "default":
		readBuf = 32768
		maxDCBuf = 4 * 1024 * 1024
		memLimit = 128 * 1024 * 1024
	case "unlimited":
		readBuf = tunnel.RTPBufSize
		maxDCBuf = 8 * 1024 * 1024
		memLimit = 256 * 1024 * 1024
	default:
		log.Fatalf("[config] unknown resources mode: %s (use moderate, default, unlimited)", *resources)
	}
	if memLimit > 0 {
		debug.SetMemoryLimit(memLimit)
	}
	log.Printf("[config] resources=%s read-buf=%d max-dc-buf=%d mem-limit=%d", *resources, readBuf, maxDCBuf, memLimit)

	var cookieStr string
	if *cookieString != "" {
		cookieStr = *cookieString
	} else if *cookiesPath != "" {
		cookieStr = tunnel.LoadCookies(*cookiesPath)
	} else {
		log.Fatal("Either --cookies or --cookie-string is required")
	}

	log.Println("[config] Fetching live config from VK bundle...")
	cfg, err := fetchConfig()
	if err != nil {
		log.Fatalf("[config] %v", err)
	}

	callInfo, err := createAndJoinCall(cookieStr, *peerId, cfg)
	if err != nil {
		log.Fatalf("Failed to create call: %v", err)
	}

	bridge := &Bridge{}
	bridge.newRelay = func() Relay {
		ur := NewTunnelRelay()
		ur.readBufSize = readBuf
		ur.maxDCBuf = maxDCBuf
		ur.OnConnected = func(tun *tunnel.VP8DataTunnel) {
			tunnel.NewRelayBridge(tun, "creator", log.Printf)
		}
		return ur
	}
	bridge.run(callInfo, cookieStr, cfg)
}
