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
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

type Cookie struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

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

func loadCookies(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("Cannot read cookies: %v", err)
	}
	var cookies []Cookie
	if err := json.Unmarshal(data, &cookies); err != nil {
		log.Fatalf("Cannot parse cookies: %v", err)
	}
	parts := make([]string, len(cookies))
	for i, c := range cookies {
		parts[i] = c.Name + "=" + c.Value
	}
	return strings.Join(parts, "; ")
}

func httpPost(endpoint string, form url.Values, extraHeaders map[string]string) ([]byte, error) {
	body := form.Encode()
	req, err := http.NewRequest("POST", endpoint, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)
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
	req.Header.Set("User-Agent", userAgent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func createAndJoinCall(cookieStr, peerId string, cfg VKConfig) (*CallInfo, error) {
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
	var tok struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	json.Unmarshal(r, &tok)
	vkToken := tok.Data.AccessToken
	if vkToken == "" {
		return nil, fmt.Errorf("empty VK token, response: %s", string(r))
	}

	log.Println("[auth] Getting call settings...")
	r, _ = httpPost("https://api.vk.com/method/calls.getSettings",
		url.Values{"v": {cfg.APIVersion}}, auth(vkToken))
	var settings struct {
		Response struct {
			Settings struct {
				PublicKey string `json:"public_key"`
				IsDev     bool   `json:"is_dev"`
			} `json:"settings"`
		} `json:"response"`
	}
	json.Unmarshal(r, &settings)
	appKey := settings.Response.Settings.PublicKey
	env := "production"
	if settings.Response.Settings.IsDev {
		env = "development"
	}

	log.Printf("[auth] Creating call peer_id=%s...", peerId)
	r, _ = httpPost("https://api.vk.com/method/calls.start",
		url.Values{"v": {cfg.APIVersion}, "peer_id": {peerId}}, auth(vkToken))
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
	log.Printf("[auth] call_id: %s", c.CallID)
	log.Printf("[auth] join_link: %s", c.JoinLink)

	log.Println("[auth] Getting call token...")
	r, _ = httpPost("https://api.vk.com/method/messages.getCallToken",
		url.Values{"v": {cfg.APIVersion}, "env": {env}}, auth(vkToken))
	var ct struct {
		Response struct {
			Token      string `json:"token"`
			APIBaseURL string `json:"api_base_url"`
		} `json:"response"`
	}
	json.Unmarshal(r, &ct)

	log.Println("[auth] OK.ru auth...")
	apiBaseURL := strings.TrimRight(ct.Response.APIBaseURL, "/")
	if !strings.HasSuffix(apiBaseURL, "/fb.do") {
		apiBaseURL += "/fb.do"
	}
	sd, _ := json.Marshal(map[string]interface{}{
		"device_id": "headless-go-1", "client_version": cfg.AppVersion,
		"client_type": "SDK_JS", "auth_token": ct.Response.Token, "version": 3,
	})
	r, _ = httpPost(apiBaseURL, url.Values{
		"method": {"auth.anonymLogin"}, "application_key": {appKey},
		"format": {"json"}, "session_data": {string(sd)},
	}, nil)
	var okAuth struct {
		SessionKey string `json:"session_key"`
	}
	json.Unmarshal(r, &okAuth)

	log.Println("[auth] Joining conversation...")
	ms, _ := json.Marshal(map[string]bool{
		"isAudioEnabled": false, "isVideoEnabled": true, "isScreenSharingEnabled": false,
	})
	r, _ = httpPost(apiBaseURL, url.Values{
		"method": {"vchat.joinConversationByLink"}, "session_key": {okAuth.SessionKey},
		"application_key": {appKey}, "format": {"json"}, "joinLink": {c.OKJoinLink},
		"isVideo": {"true"}, "isAudio": {"false"}, "mediaSettings": {string(ms)},
	}, nil)
	var jr struct {
		Endpoint   string     `json:"endpoint"`
		TurnServer TurnServer `json:"turn_server"`
		StunServer StunServer `json:"stun_server"`
	}
	json.Unmarshal(r, &jr)

	return &CallInfo{
		CallID: c.CallID, JoinLink: c.JoinLink, ShortLink: c.ShortCredentials.LinkWithPassword,
		OKJoinLink: c.OKJoinLink, TurnServer: jr.TurnServer, StunServer: jr.StunServer,
		WSEndpoint: jr.Endpoint,
	}, nil
}

type Bridge struct {
	mu                sync.Mutex
	vkWs              *websocket.Conn
	pionWs            *websocket.Conn
	vkSeq             int
	pionReqID         int
	pionPending       map[int]chan json.RawMessage
	pendingOffer      json.RawMessage
	remotePeerId      *int64
	pendingCandidates []json.RawMessage
	iceServers        []map[string]interface{}
	connected         bool
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

func (b *Bridge) pionSend(msgType string, data interface{}) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pionWs == nil {
		return
	}
	msg := map[string]interface{}{"type": msgType, "data": data}
	raw, _ := json.Marshal(msg)
	b.pionWs.WriteMessage(websocket.TextMessage, raw)
	log.Printf("[pion] -> %s", msgType)
}

func (b *Bridge) pionRequest(msgType string, data interface{}) (json.RawMessage, error) {
	b.mu.Lock()
	b.pionReqID++
	id := b.pionReqID
	ch := make(chan json.RawMessage, 1)
	b.pionPending[id] = ch
	msg := map[string]interface{}{"type": msgType, "data": data, "id": id}
	raw, _ := json.Marshal(msg)
	if b.pionWs != nil {
		b.pionWs.WriteMessage(websocket.TextMessage, raw)
	}
	b.mu.Unlock()
	log.Printf("[pion] -> request %s id=%d", msgType, id)

	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(10 * time.Second):
		b.mu.Lock()
		delete(b.pionPending, id)
		b.mu.Unlock()
		return nil, fmt.Errorf("timeout: %s", msgType)
	}
}

func (b *Bridge) resetPion() {
	log.Println("[bridge] Resetting Pion PC for reconnection...")
	b.pionRequest("reset", map[string]interface{}{})

	// Re-send ICE servers so Pion creates a fresh PC
	b.pionSend("ice-servers", b.iceServers)

	// Create new offer
	offerRaw, err := b.pionRequest("create-offer", map[string]interface{}{})
	if err != nil {
		log.Printf("[bridge] Reset create-offer failed: %v", err)
		return
	}
	b.mu.Lock()
	b.pendingOffer = offerRaw
	b.pendingCandidates = nil
	b.mu.Unlock()
	log.Printf("[bridge] New offer ready after reset, length: %d", len(offerRaw))
}

func (b *Bridge) sendOfferToPeer(participantId int64) {
	b.mu.Lock()
	offer := b.pendingOffer
	candidates := b.pendingCandidates
	b.pendingOffer = nil
	b.pendingCandidates = nil
	b.mu.Unlock()

	if offer != nil {
		log.Printf("[bridge] Sending offer to peer %d", participantId)
		var offerObj struct {
			Type string `json:"type"`
			SDP  string `json:"sdp"`
		}
		json.Unmarshal(offer, &offerObj)
		sdpStr, _ := json.Marshal(offerObj.SDP)
		b.mu.Lock()
		b.vkSeq++
		seq := b.vkSeq
		raw := fmt.Sprintf(`{"command":"transmit-data","sequence":%d,"participantId":%d,"data":{"sdp":{"type":%q,"sdp":%s}}}`,
			seq, participantId, offerObj.Type, sdpStr)
		if b.vkWs != nil {
			b.vkWs.WriteMessage(websocket.TextMessage, []byte(raw))
		}
		b.mu.Unlock()
		log.Printf("[vk-ws] -> transmit-data (offer)")
	}

	for _, cand := range candidates {
		var c interface{}
		json.Unmarshal(cand, &c)
		b.vkSend("transmit-data", map[string]interface{}{
			"participantId": participantId,
			"data":          map[string]interface{}{"candidate": c},
		})
	}
	if len(candidates) > 0 {
		log.Printf("[bridge] Flushed %d ICE candidates", len(candidates))
	}
}

func (b *Bridge) onRegisteredPeer(participantId int64) {
	b.mu.Lock()
	oldPeer := b.remotePeerId
	isConnected := b.connected
	b.remotePeerId = &participantId
	b.mu.Unlock()

	log.Printf("[bridge] Remote peer registered: %d (connected=%v)", participantId, isConnected)

	// Reset Pion if connection is dead or peer changed
	needsReset := false
	if oldPeer != nil && *oldPeer != participantId {
		log.Printf("[bridge] New peer %d replacing old peer %d", participantId, *oldPeer)
		needsReset = true
	} else if oldPeer != nil && !isConnected {
		log.Printf("[bridge] Same peer %d re-registered, connection dead, resetting", participantId)
		needsReset = true
	}
	if needsReset {
		b.resetPion()
	}

	b.sendOfferToPeer(participantId)
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
			if data == nil {
				break
			}
			if cand, ok := data["candidate"]; ok {
				log.Println("[bridge] Remote ICE candidate from VK")
				candJSON, _ := json.Marshal(cand)
				b.pionSend("remote-ice-candidate", json.RawMessage(candJSON))
			}
			if sdp, ok := data["sdp"].(map[string]interface{}); ok {
				sdpType, _ := sdp["type"].(string)
				log.Printf("[bridge] Remote SDP from VK: %s", sdpType)
				sdpJSON, _ := json.Marshal(sdp)
				if sdpType == "answer" {
					b.pionRequest("set-remote-description", json.RawMessage(sdpJSON))
				} else if sdpType == "offer" {
					b.pionRequest("set-remote-description", json.RawMessage(sdpJSON))
					answerRaw, err := b.pionRequest("create-answer", map[string]interface{}{})
					if err == nil {
						b.mu.Lock()
						pid := b.remotePeerId
						b.mu.Unlock()
						if pid != nil {
							var answer interface{}
							json.Unmarshal(answerRaw, &answer)
							b.vkSend("transmit-data", map[string]interface{}{
								"participantId": *pid,
								"data":          map[string]interface{}{"sdp": answer},
							})
						}
					}
				}
			}

		case "registered-peer":
			if pid, ok := msg["participantId"].(float64); ok {
				b.onRegisteredPeer(int64(pid))
			}

		case "participant-joined", "participant-added":
			log.Println("[vk-ws]    Participant event")

		default:
			// Log unknown notifications briefly
		}

	case "response":
		seq, _ := msg["sequence"].(float64)
		log.Printf("[vk-ws] <- response seq=%d", int(seq))

	case "error":
		errMsg, _ := msg["message"].(string)
		errCode, _ := msg["error"].(string)
		log.Printf("[vk-ws] <- error: %s %s", errCode, errMsg)
	}
}

func (b *Bridge) handlePionMessage(raw []byte) {
	var msg struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
		ID   int             `json:"id,omitempty"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return
	}

	if msg.ID > 0 {
		b.mu.Lock()
		ch, ok := b.pionPending[msg.ID]
		if ok {
			delete(b.pionPending, msg.ID)
		}
		b.mu.Unlock()
		if ok {
			ch <- msg.Data
			return
		}
	}

	switch msg.Type {
	case "ice-candidate":
		log.Println("[pion] <- ice-candidate")
		b.mu.Lock()
		pid := b.remotePeerId
		b.mu.Unlock()
		if pid != nil {
			var cand interface{}
			json.Unmarshal(msg.Data, &cand)
			b.vkSend("transmit-data", map[string]interface{}{
				"participantId": *pid,
				"data":          map[string]interface{}{"candidate": cand},
			})
		} else {
			b.mu.Lock()
			b.pendingCandidates = append(b.pendingCandidates, msg.Data)
			b.mu.Unlock()
		}

	case "connection-state":
		var state string
		json.Unmarshal(msg.Data, &state)
		log.Printf("[pion] <- connection-state: %s", state)
		b.mu.Lock()
		switch state {
		case "connected":
			b.connected = true
			b.mu.Unlock()
			log.Println("\n  TUNNEL CONNECTED\n")
		case "disconnected":
			b.connected = false
			b.mu.Unlock()
			log.Println("[bridge] Connection disconnected, waiting for peer to rejoin")
		case "failed":
			b.connected = false
			b.mu.Unlock()
			log.Println("[bridge] Connection failed, waiting for peer to rejoin")
		case "closed":
			b.connected = false
			b.mu.Unlock()
			log.Println("[bridge] Connection closed")
		default:
			b.mu.Unlock()
		}

	case "remote-track":
		log.Printf("[pion] <- remote-track: %s", string(msg.Data))
	}
}

func (b *Bridge) run(callInfo *CallInfo, cfg VKConfig, pionAddr string) {
	b.pionPending = make(map[int]chan json.RawMessage)

	fmt.Println("")
	fmt.Println("  CALL CREATED")
	fmt.Println("  join_link:", callInfo.JoinLink)
	if callInfo.ShortLink != "" {
		fmt.Println("  short:    ", callInfo.ShortLink)
	}
	fmt.Println("  TURN:     ", strings.Join(callInfo.TurnServer.URLs, ", "))
	fmt.Printf("  protocol:  v%s sdk %s\n\n", cfg.ProtocolVersion, cfg.SDKVersion)

	// Connect to Pion relay
	log.Printf("[pion] Connecting to %s ...", pionAddr)
	pionWs, _, err := websocket.DefaultDialer.Dial(pionAddr, nil)
	if err != nil {
		log.Fatalf("[pion] Connect failed: %v", err)
	}
	b.pionWs = pionWs
	log.Println("[pion] Connected")

	// Send ICE servers to Pion
	iceServers := []map[string]interface{}{}
	if len(callInfo.StunServer.URLs) > 0 {
		iceServers = append(iceServers, map[string]interface{}{
			"urls": callInfo.StunServer.URLs, "username": "", "credential": "",
		})
	}
	if len(callInfo.TurnServer.URLs) > 0 {
		urls := append([]string{}, callInfo.TurnServer.URLs...)
		urls = append(urls, urls[len(urls)-1]+"?transport=tcp")
		iceServers = append(iceServers, map[string]interface{}{
			"urls": urls, "username": callInfo.TurnServer.Username, "credential": callInfo.TurnServer.Credential,
		})
	}
	b.iceServers = iceServers
	b.pionSend("ice-servers", iceServers)

	// Start reading Pion messages before making requests
	go func() {
		for {
			_, msg, err := pionWs.ReadMessage()
			if err != nil {
				log.Println("[pion] Disconnected")
				return
			}
			b.handlePionMessage(msg)
		}
	}()

	// Create offer from Pion, queue it until a peer joins
	offerRaw, err := b.pionRequest("create-offer", map[string]interface{}{})
	if err != nil {
		log.Fatalf("[bridge] Create offer failed: %v", err)
	}
	b.mu.Lock()
	b.pendingOffer = offerRaw
	b.mu.Unlock()
	log.Printf("[bridge] Pion offer ready, length: %d", len(offerRaw))

	// Connect to VK signaling WebSocket
	wsURL := callInfo.WSEndpoint +
		"&platform=WEB" +
		"&appVersion=" + cfg.AppVersion +
		"&version=" + cfg.ProtocolVersion +
		"&device=browser&capabilities=0&clientType=VK&tgt=join"

	log.Println("[vk-ws] Connecting...")
	vkHeader := http.Header{}
	vkHeader.Set("User-Agent", userAgent)
	vkHeader.Set("Origin", "https://vk.com")
	vkDialer := websocket.Dialer{WriteBufferSize: 65536}
	vkWs, _, err := vkDialer.Dial(wsURL, vkHeader)
	if err != nil {
		log.Fatalf("[vk-ws] Connect failed: %v", err)
	}
	b.vkWs = vkWs
	log.Println("[vk-ws] Connected")

	b.vkSend("change-media-settings", map[string]interface{}{
		"mediaSettings": map[string]interface{}{
			"isAudioEnabled": false, "isVideoEnabled": true,
			"isScreenSharingEnabled": false, "isFastScreenSharingEnabled": false,
			"isAudioSharingEnabled": false, "isAnimojiEnabled": false,
		},
	})

	// VK keepalive
	go func() {
		for {
			time.Sleep(15 * time.Second)
			b.mu.Lock()
			ws := b.vkWs
			b.mu.Unlock()
			if ws != nil {
				ws.WriteMessage(websocket.PingMessage, nil)
			}
		}
	}()

	// Read VK messages (blocks main goroutine)
	for {
		_, msg, err := vkWs.ReadMessage()
		if err != nil {
			log.Printf("[vk-ws] Closed: %v", err)
			return
		}
		if string(msg) == "ping" {
			vkWs.WriteMessage(websocket.TextMessage, []byte("pong"))
			continue
		}
		b.handleVKMessage(msg)
	}
}

func main() {
	cookiesPath := flag.String("cookies", "cookies.json", "path to cookies.json")
	peerId := flag.String("peer-id", "", "VK peer_id for the call")
	pionPort := flag.Int("pion-port", 9001, "Pion relay WebSocket port")
	flag.Parse()

	cookieStr := loadCookies(*cookiesPath)

	log.Println("[config] Fetching live config from VK bundle...")
	cfg := fetchConfig()

	callInfo, err := createAndJoinCall(cookieStr, *peerId, cfg)
	if err != nil {
		log.Fatalf("Failed to create call: %v", err)
	}

	bridge := &Bridge{}
	bridge.run(callInfo, cfg, fmt.Sprintf("ws://127.0.0.1:%d/signaling", *pionPort))
}
