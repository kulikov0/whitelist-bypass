package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/kulikov0/headless-client/webrtc"
	"whitelist-bypass/relay/common"
	"whitelist-bypass/relay/tunnel"
	"whitelist-bypass/relay/wtsignal"
)

const TopologyDirect = "DIRECT"
const maxServerBounces = 5

type CallInfo struct {
	CallID     string
	JoinLink   string
	ShortLink  string
	OKJoinLink string
	TurnServer TurnServer
	StunServer StunServer
	WSEndpoint string
	WtEndpoint string
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
	WtEndpoint string     `json:"wt_endpoint"`
	TurnServer TurnServer `json:"turn_server"`
	StunServer StunServer `json:"stun_server"`
}

type Bridge struct {
	mu            sync.Mutex
	sfu           *wtsignal.Conn
	vkSeq         int
	iceServers    []webrtc.ICEServer
	topology      string
	peers         map[int64]struct{}
	relay         Relay
	newRelay      func() Relay
	p2p           *P2PHandler
	screenSharing bool

	serverBounces       int
	suppressScreenshare bool
	bouncing            bool
}

func httpPost(endpoint string, form url.Values, extraHeaders map[string]string) ([]byte, error) {
	body := form.Encode()
	req, err := http.NewRequest("POST", endpoint, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", common.UserAgent)
	req.Header.Set("Origin", "https://vk.ru")
	req.Header.Set("Referer", "https://vk.ru/")
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
	req.Header.Set("User-Agent", common.UserAgent)
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

	r, err := httpPost("https://login.vk.ru/?act=web_token",
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

	r, err = httpPost("https://api.vk.ru/method/calls.getSettings",
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

	r, err = httpPost("https://api.vk.ru/method/messages.getCallToken",
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

func joinExistingCall(cookieStr, vkLink string, cfg VKConfig) (*CallInfo, error) {
	if cfg.AppID == "" || cfg.APIVersion == "" {
		return nil, fmt.Errorf("config incomplete: app_id=%q api=%q", cfg.AppID, cfg.APIVersion)
	}
	token := extractJoinToken(vkLink)
	if token == "" {
		return nil, fmt.Errorf("could not extract join token from %q", vkLink)
	}
	log.Printf("[auth] Joining existing call token=%s", token)
	joinResp, err := authAndJoin(cookieStr, token, cfg)
	if err != nil {
		return nil, err
	}
	return &CallInfo{
		JoinLink:   vkLink,
		OKJoinLink: token,
		TurnServer: joinResp.TurnServer,
		StunServer: joinResp.StunServer,
		WSEndpoint: joinResp.Endpoint,
		WtEndpoint: joinResp.WtEndpoint,
	}, nil
}

func extractJoinToken(link string) string {
	link = strings.TrimSpace(link)
	if link == "" {
		return ""
	}
	if u, err := url.Parse(link); err == nil && u.Scheme != "" {
		path := strings.Trim(u.Path, "/")
		if path != "" {
			parts := strings.Split(path, "/")
			return parts[len(parts)-1]
		}
	}
	if !strings.ContainsAny(link, "/?&=") {
		return link
	}
	parts := strings.Split(strings.TrimRight(link, "/"), "/")
	return parts[len(parts)-1]
}

func createAndJoinCall(cookieStr, peerId string, cfg VKConfig) (*CallInfo, error) {
	if cfg.AppID == "" || cfg.APIVersion == "" {
		return nil, fmt.Errorf("config incomplete: app_id=%q api=%q", cfg.AppID, cfg.APIVersion)
	}

	auth := func(bearer string) map[string]string {
		return map[string]string{"Authorization": "Bearer " + bearer}
	}

	log.Println("[auth] Getting VK token...")
	r, err := httpPost("https://login.vk.ru/?act=web_token",
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
	r, err = httpPost("https://api.vk.ru/method/calls.start",
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

	log.Println("[auth] Joining conversation...")
	joinResp, err := authAndJoin(cookieStr, c.OKJoinLink, cfg)
	if err != nil {
		return nil, err
	}

	return &CallInfo{
		CallID: c.CallID, JoinLink: c.JoinLink, ShortLink: c.ShortCredentials.LinkWithPassword,
		OKJoinLink: c.OKJoinLink, TurnServer: joinResp.TurnServer, StunServer: joinResp.StunServer,
		WSEndpoint: joinResp.Endpoint,
		WtEndpoint: joinResp.WtEndpoint,
	}, nil
}

func (b *Bridge) vkSend(command string, extra map[string]interface{}) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sfu == nil {
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
	b.sfu.Send(out)
	log.Printf("[vk-ws] -> %s", command)
}

func (b *Bridge) sendMediaSettings(screenSharing bool) {
	b.vkSend("change-media-settings", map[string]interface{}{
		"mediaSettings": map[string]interface{}{
			"isAudioEnabled": false, "isVideoEnabled": true,
			"isScreenSharingEnabled": screenSharing, "isFastScreenSharingEnabled": false,
			"isAudioSharingEnabled": false, "isAnimojiEnabled": false,
		},
	})
}

// setScreenSharing re-announces media settings when the peer track count
// crosses the single-track boundary, mirroring how the web client toggles
// screenshare mid-call. State resets to false on every WS reconnect.
func (b *Bridge) setScreenSharing(enabled bool) {
	b.mu.Lock()
	if b.sfu == nil || b.screenSharing == enabled {
		b.mu.Unlock()
		return
	}
	if enabled && b.suppressScreenshare {
		b.mu.Unlock()
		log.Println("[vk-ws] screenshare suppressed after SERVER flap, staying single-track DIRECT")
		return
	}
	b.screenSharing = enabled
	b.mu.Unlock()
	log.Printf("[vk-ws] peer track count change, screenshare=%v", enabled)
	b.sendMediaSettings(enabled)
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
			if conv, ok := msg["conversation"].(map[string]interface{}); ok {
				topo, _ := conv["topology"].(string)
				log.Printf("[vk-ws]    connection topology=%q", topo)
			}
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
				b.bounceForServerTopology("SERVER topology")
				return
			}

		case "participant-joined", "participant-added":
			if pid, ok := msg["participantId"].(float64); ok {
				b.peers[int64(pid)] = struct{}{}
				log.Printf("[vk-ws]    Participant %d joined (total: %d)", int64(pid), len(b.peers))
				if b.topology != TopologyDirect {
					b.bounceForServerTopology("participant joined under SERVER")
					return
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
			if b.sfu != nil {
				b.sfu.Close()
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

func (b *Bridge) connectVKWs(wtURL string) error {
	parsed, err := url.Parse(wtURL)
	if err != nil {
		return err
	}
	host := parsed.Hostname()
	ips, err := net.LookupHost(host)
	if err != nil || len(ips) == 0 {
		return fmt.Errorf("resolve %s: %w", host, err)
	}
	sfu, err := wtsignal.Dial(wtURL, host, ips[0])
	if err != nil {
		return err
	}
	b.mu.Lock()
	b.sfu = sfu
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

func (b *Bridge) bounceForServerTopology(reason string) {
	b.mu.Lock()
	if b.bouncing {
		b.mu.Unlock()
		return
	}
	b.bouncing = true
	b.serverBounces++
	count := b.serverBounces
	if count > maxServerBounces {
		b.suppressScreenshare = true
	}
	suppress := b.suppressScreenshare
	sfu := b.sfu
	b.mu.Unlock()

	if suppress {
		log.Printf("[vk-ws]    %s -> reconnect #%d, suppressing screenshare to settle single-track DIRECT", reason, count)
	} else {
		log.Printf("[vk-ws]    %s -> manual reconnect #%d to recover DIRECT", reason, count)
	}
	if sfu != nil {
		sfu.Close()
	}
}

func (b *Bridge) readLoop() error {
	b.mu.Lock()
	sfu := b.sfu
	b.mu.Unlock()
	if sfu == nil {
		return fmt.Errorf("no transport")
	}
	for {
		msg, err := sfu.Recv()
		if err != nil {
			return err
		}
		if string(msg) == "ping" {
			sfu.Send([]byte("pong"))
			continue
		}
		b.handleVKMessage(msg)
	}
}

func (b *Bridge) run(callInfo *CallInfo, cookieStr string, cfg VKConfig) {
	fmt.Println("")
	fmt.Println("  CALL CREATED")
	fmt.Println("  join_link:", callInfo.JoinLink)
	fmt.Println("  TURN:     ", strings.Join(callInfo.TurnServer.URLs, ", "))
	fmt.Printf("  protocol:  v%s sdk %s\n\n", cfg.ProtocolVersion, cfg.SDKVersion)

	b.iceServers = buildICEServers(callInfo)
	wtEndpoint := callInfo.WtEndpoint

	capabilities := "2F7F"
	makeWtURL := func(ep string) string {
		return ep +
			"&platform=WEB" +
			"&appVersion=" + cfg.AppVersion +
			"&version=" + cfg.ProtocolVersion +
			"&device=browser&capabilities=" + capabilities + "&clientType=VK&tgt=join&compression=deflate-raw"
	}

	for {
		b.initRelay()

		if wtEndpoint == "" {
			log.Fatal("[vk-ws] no wt_endpoint in join response")
		}

		log.Println("[vk-ws] Connecting...")
		if err := b.connectVKWs(makeWtURL(wtEndpoint)); err != nil {
			log.Printf("[vk-ws] Connect failed: %s, retrying in 5s...", common.MaskError(err))
			time.Sleep(5 * time.Second)
			continue
		}
		log.Println("[vk-ws] Connected")

		b.mu.Lock()
		b.screenSharing = false
		b.bouncing = false
		b.mu.Unlock()
		b.sendMediaSettings(false)

		err := b.readLoop()
		log.Printf("[vk-ws] Closed: %s", common.MaskError(err))

		b.mu.Lock()
		if b.sfu != nil {
			b.sfu.Close()
		}
		b.sfu = nil
		b.mu.Unlock()

		log.Println("[vk-ws] Rejoining in 3s...")
		time.Sleep(3 * time.Second)

		joinResp, rerr := authAndJoin(cookieStr, callInfo.OKJoinLink, cfg)
		if rerr != nil {
			log.Printf("[rejoin] Failed: %v, retrying in 5s...", rerr)
			time.Sleep(5 * time.Second)
			continue
		}
		wtEndpoint = joinResp.WtEndpoint
		callInfo.TurnServer = joinResp.TurnServer
		callInfo.StunServer = joinResp.StunServer
		b.iceServers = buildICEServers(callInfo)
	}
}

func main() {
	common.MaybePrintVersion()
	cookiesPath := flag.String("cookies", "", "path to cookies.json")
	cookieString := flag.String("cookie-string", "", "raw cookie string (name=val; name=val)")
	peerId := flag.String("peer-id", "", "VK peer_id for a new call")
	vkLink := flag.String("vk-link", "", "VK call link to join an existing call")
	resources := flag.String("resources", "default", "resource mode: default, moderate, unlimited, custom")
	customReadBuf := flag.Int("read-buf", 0, "read buffer size, used with -resources custom")
	customMaxDCBuf := flag.Int("max-dc-buf", 0, "max DC buffer size in bytes, used with -resources custom")
	customMemLimit := flag.Int64("mem-limit", 0, "memory limit in bytes, used with -resources custom")
	writeFile := flag.String("write-file", "", "path to file where active call link is appended")
	upstreamSocks := flag.String("upstream-socks", "", "route tunneled egress through this SOCKS5 proxy (host:port), e.g. a local VPN client")
	upstreamUser := flag.String("upstream-user", "", "upstream SOCKS5 username")
	upstreamPass := flag.String("upstream-pass", "", "upstream SOCKS5 password")
	debugFlag := flag.Bool("debug", false, "verbose debug logging")
	flag.Parse()
	common.Debug = *debugFlag

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
		readBuf = common.RTPBufSize
		maxDCBuf = 8 * 1024 * 1024
		memLimit = 256 * 1024 * 1024
	case "custom":
		readBuf = *customReadBuf
		if readBuf == 0 {
			readBuf = common.RTPBufSize
		}
		maxDCBuf = uint64(*customMaxDCBuf)
		if maxDCBuf == 0 {
			maxDCBuf = 8 * 1024 * 1024
		}
		memLimit = *customMemLimit
		if memLimit == 0 {
			memLimit = 256 * 1024 * 1024
		}
	default:
		log.Fatalf("[config] unknown resources mode: %s (use moderate, default, unlimited, custom)", *resources)
	}
	if memLimit > 0 {
		debug.SetMemoryLimit(memLimit)
	}
	log.Printf("[config] resources=%s read-buf=%d max-dc-buf=%d mem-limit=%d", *resources, readBuf, maxDCBuf, memLimit)

	var cookieStr string
	if *cookieString != "" {
		cookieStr = *cookieString
	} else if *cookiesPath != "" {
		cookieStr = common.LoadCookies(*cookiesPath)
	} else {
		fmt.Println("WAITING_FOR_COOKIES")
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil || strings.TrimSpace(line) == "" {
			log.Fatal("No cookies received on stdin")
		}
		cookieStr = strings.TrimSpace(line)
	}

	log.Println("[config] Fetching live config from VK bundle...")
	cfg, err := fetchConfig()
	if err != nil {
		log.Fatalf("[config] %v", err)
	}

	if *vkLink != "" && *peerId != "" {
		log.Fatalf("[config] -vk-link and -peer-id are mutually exclusive")
	}
	var callInfo *CallInfo
	if *vkLink != "" {
		callInfo, err = joinExistingCall(cookieStr, *vkLink, cfg)
		if err != nil {
			log.Fatalf("Failed to join existing call: %v", err)
		}
	} else {
		callInfo, err = createAndJoinCall(cookieStr, *peerId, cfg)
		if err != nil {
			log.Fatalf("Failed to create call: %v", err)
		}
	}

	if *writeFile != "" {
		f, err := os.OpenFile(*writeFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatalf("Failed to open write-file: %v", err)
		}
		fmt.Fprintln(f, callInfo.JoinLink)
		f.Close()
		log.Printf("[config] Wrote call link to %s", *writeFile)
	}

	obf, obfErr := tunnel.NewTunnelObfuscator(tunnel.DeriveSecretFromJoinLink(callInfo.JoinLink))
	if obfErr != nil {
		log.Fatalf("[config] obfuscator init failed: %v", obfErr)
	}
	log.Printf("[obf] key-source=%q localEpoch=0x%08x", callInfo.JoinLink, obf.LocalEpoch())
	bridge := &Bridge{}
	bridge.newRelay = func() Relay {
		ur := NewTunnelRelay()
		ur.readBufSize = readBuf
		ur.maxDCBuf = maxDCBuf
		ur.SetObfuscator(obf)
		ur.SetUpstreamSocks(*upstreamSocks, *upstreamUser, *upstreamPass)
		ur.OnConnected = func(tun tunnel.DataTunnel) {
			rb := tunnel.NewRelayBridge(tun, "creator", common.VP8BufSize, log.Printf)
			rb.SetUpstreamSocks(*upstreamSocks, *upstreamUser, *upstreamPass)
			if st, ok := tun.(*tunnel.SymmetricScreenTunnel); ok {
				rb.SetOnPeerConfig(func(fps, batch, trackCount int) {
					st.SetTrackCount(trackCount)
					bridge.setScreenSharing(trackCount > 1)
				})
			}
		}
		return ur
	}
	bridge.run(callInfo, cookieStr, cfg)
}
