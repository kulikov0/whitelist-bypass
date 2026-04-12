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

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
	"whitelist-bypass/relay/common"
	"whitelist-bypass/relay/tunnel"
)

const (
	tmAPIBase    = "https://cloud-api.yandex.ru/telemost_front/v2/telemost"
	tmOrigin     = "https://telemost.yandex.ru"
	tmPingPeriod = 5 * time.Second
)

var capabilitiesOffer = map[string][]string{
	"offerAnswerMode":              {"SEPARATE"},
	"initialSubscriberOffer":       {"ON_HELLO"},
	"slotsMode":                    {"FROM_CONTROLLER"},
	"simulcastMode":                {"DISABLED", "STATIC"},
	"selfVadStatus":                {"FROM_SERVER", "FROM_CLIENT"},
	"dataChannelSharing":           {"TO_RTP"},
	"videoEncoderConfig":           {"NO_CONFIG", "ONLY_INIT_CONFIG", "RUNTIME_CONFIG"},
	"dataChannelVideoCodec":        {"VP8", "UNIQUE_CODEC_FROM_TRACK_DESCRIPTION"},
	"bandwidthLimitationReason":    {"BANDWIDTH_REASON_DISABLED", "BANDWIDTH_REASON_ENABLED"},
	"sdkDefaultDeviceManagement":   {"SDK_DEFAULT_DEVICE_MANAGEMENT_DISABLED", "SDK_DEFAULT_DEVICE_MANAGEMENT_ENABLED"},
	"joinOrderLayout":              {"JOIN_ORDER_LAYOUT_DISABLED", "JOIN_ORDER_LAYOUT_ENABLED"},
	"pinLayout":                    {"PIN_LAYOUT_DISABLED"},
	"sendSelfViewVideoSlot":        {"SEND_SELF_VIEW_VIDEO_SLOT_DISABLED", "SEND_SELF_VIEW_VIDEO_SLOT_ENABLED"},
	"serverLayoutTransition":       {"SERVER_LAYOUT_TRANSITION_DISABLED"},
	"sdkPublisherOptimizeBitrate":  {"SDK_PUBLISHER_OPTIMIZE_BITRATE_DISABLED", "SDK_PUBLISHER_OPTIMIZE_BITRATE_FULL", "SDK_PUBLISHER_OPTIMIZE_BITRATE_ONLY_SELF"},
	"sdkNetworkLostDetection":      {"SDK_NETWORK_LOST_DETECTION_DISABLED"},
	"sdkNetworkPathMonitor":        {"SDK_NETWORK_PATH_MONITOR_DISABLED"},
	"publisherVp9":                 {"PUBLISH_VP9_DISABLED", "PUBLISH_VP9_ENABLED"},
	"svcMode":                      {"SVC_MODE_DISABLED", "SVC_MODE_L3T3", "SVC_MODE_L3T3_KEY"},
	"subscriberOfferAsyncAck":      {"SUBSCRIBER_OFFER_ASYNC_ACK_DISABLED", "SUBSCRIBER_OFFER_ASYNC_ACK_ENABLED"},
	"androidBluetoothRoutingFix":   {"ANDROID_BLUETOOTH_ROUTING_FIX_DISABLED"},
	"fixedIceCandidatesPoolSize":   {"FIXED_ICE_CANDIDATES_POOL_SIZE_DISABLED"},
	"sdkAndroidTelecomIntegration": {"SDK_ANDROID_TELECOM_INTEGRATION_DISABLED"},
	"setActiveCodecsMode":          {"SET_ACTIVE_CODECS_MODE_DISABLED", "SET_ACTIVE_CODECS_MODE_VIDEO_ONLY"},
	"subscriberDtlsPassiveMode":    {"SUBSCRIBER_DTLS_PASSIVE_MODE_DISABLED", "SUBSCRIBER_DTLS_PASSIVE_MODE_ENABLED"},
}

type ConnInfo struct {
	ConferenceURI  string
	RoomID         string
	PeerID         string
	Credentials    string
	MediaServerURL string
	ServiceName    string
	ICEServers     []webrtc.ICEServer
}

type Bridge struct {
	mu        sync.Mutex
	ws        *websocket.Conn
	relay     *SFURelay
	connInfo  *ConnInfo
	config    TMConfig
	cookieStr string
	pubSeq    int
	subSeq    int
	peers     map[string]string
	readBuf   int
	maxDCBuf  uint64
}

func tmRequest(method, path string, body interface{}, cookieStr string, cfg TMConfig) ([]byte, int, error) {
	var bodyReader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		bodyReader = strings.NewReader(string(data))
	}
	req, err := http.NewRequest(method, tmAPIBase+path, bodyReader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", common.UserAgent)
	req.Header.Set("Origin", tmOrigin)
	req.Header.Set("Referer", tmOrigin+"/")
	req.Header.Set("Cookie", cookieStr)
	req.Header.Set("Client-Instance-Id", uuid.New().String())
	req.Header.Set("X-Telemost-Client-Version", cfg.AppVersion)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return data, resp.StatusCode, err
}

func parseICEServersJSON(raw json.RawMessage) []webrtc.ICEServer {
	var rawIce []struct {
		URLs       []string `json:"urls"`
		Username   string   `json:"username"`
		Credential string   `json:"credential"`
	}
	json.Unmarshal(raw, &rawIce)
	var out []webrtc.ICEServer
	for _, s := range rawIce {
		ice := webrtc.ICEServer{URLs: s.URLs}
		if s.Username != "" {
			ice.Username = s.Username
			ice.Credential = s.Credential
		}
		out = append(out, ice)
	}
	return out
}

func getConnection(cookieStr, confURL string, cfg TMConfig) (*ConnInfo, error) {
	r, status, err := tmRequest("GET",
		"/conferences/"+confURL+"/connection?next_gen_media_platform_allowed=true&display_name=Headless&waiting_room_supported=true",
		nil, cookieStr, cfg)
	if err != nil {
		return nil, fmt.Errorf("get connection: %w", err)
	}
	if status != 200 {
		return nil, fmt.Errorf("get connection: status %d: %s", status, string(r))
	}
	var conn struct {
		PeerID       string `json:"peer_id"`
		RoomID       string `json:"room_id"`
		Credentials  string `json:"credentials"`
		ClientConfig struct {
			MediaServerURL string          `json:"media_server_url"`
			ServiceName    string          `json:"service_name"`
			ICEServers     json.RawMessage `json:"ice_servers"`
		} `json:"client_configuration"`
	}
	json.Unmarshal(r, &conn)
	if conn.ClientConfig.MediaServerURL == "" {
		return nil, fmt.Errorf("empty media_server_url: %s", string(r))
	}
	return &ConnInfo{
		RoomID:         conn.RoomID,
		PeerID:         conn.PeerID,
		Credentials:    conn.Credentials,
		MediaServerURL: conn.ClientConfig.MediaServerURL,
		ServiceName:    conn.ClientConfig.ServiceName,
		ICEServers:     parseICEServersJSON(conn.ClientConfig.ICEServers),
	}, nil
}

func createAndJoinCall(cookieStr string, cfg TMConfig) (*ConnInfo, error) {
	log.Println("[auth] Creating conference...")
	r, status, err := tmRequest("POST", "/conferences?next_gen_media_platform_allowed=true",
		struct{}{}, cookieStr, cfg)
	if err != nil {
		return nil, fmt.Errorf("create conference: %w", err)
	}
	if status != 200 && status != 201 {
		return nil, fmt.Errorf("create conference: status %d: %s", status, string(r))
	}
	var conf struct {
		URI string `json:"uri"`
	}
	json.Unmarshal(r, &conf)
	if conf.URI == "" {
		return nil, fmt.Errorf("empty conference URI: %s", string(r))
	}
	log.Printf("[auth] Conference: %s", conf.URI)

	log.Println("[auth] Getting connection...")
	info, err := getConnection(cookieStr, url.QueryEscape(conf.URI), cfg)
	if err != nil {
		return nil, err
	}
	info.ConferenceURI = conf.URI
	log.Printf("[auth] peer_id=%s room_id=%s", info.PeerID, info.RoomID)
	log.Printf("[auth] media_server=%s", info.MediaServerURL)
	return info, nil
}

func (b *Bridge) wsSend(msg interface{}) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ws == nil {
		return
	}
	data, _ := json.Marshal(msg)
	b.ws.WriteMessage(websocket.TextMessage, data)
}

func (b *Bridge) ack(uid string) {
	b.wsSend(map[string]interface{}{
		"uid": uid,
		"ack": map[string]interface{}{
			"status": map[string]interface{}{"code": "OK", "description": ""},
		},
	})
}

func (b *Bridge) sendHello() {
	b.wsSend(map[string]interface{}{
		"uid": uuid.New().String(),
		"hello": map[string]interface{}{
			"participantMeta":       map[string]interface{}{"name": "Headless", "role": "SPEAKER", "description": "", "sendAudio": false, "sendVideo": true},
			"participantAttributes": map[string]interface{}{"name": "Headless", "role": "SPEAKER", "description": ""},
			"sendAudio": false, "sendVideo": true, "sendSharing": false,
			"participantId": b.connInfo.PeerID, "roomId": b.connInfo.RoomID,
			"serviceName": b.connInfo.ServiceName, "credentials": b.connInfo.Credentials,
			"capabilitiesOffer": capabilitiesOffer,
			"sdkInfo":           map[string]interface{}{"implementation": "browser", "version": b.config.SDKVersion, "userAgent": common.UserAgent, "hwConcurrency": 8},
			"sdkInitializationId": uuid.New().String(),
			"disablePublisher": false, "disableSubscriber": false, "disableSubscriberAudio": false,
		},
	})
	log.Println("[tm-ws] -> hello")
}

func (b *Bridge) sendPubOffer() {
	offer, err := b.relay.CreatePubOffer()
	if err != nil {
		log.Printf("[tm-ws] pub offer failed: %v", err)
		return
	}
	audioMid, videoMid := parseMids(offer.SDP)
	log.Printf("[tm-ws] -> publisherSdpOffer pcSeq=%d", b.pubSeq)

	var tracks []map[string]interface{}
	if audioMid != "" {
		tracks = append(tracks, map[string]interface{}{"mid": audioMid, "transceiverMid": audioMid, "kind": "AUDIO", "priority": 0, "label": "", "codecs": map[string]interface{}{}, "groupId": 1, "description": ""})
	}
	if videoMid != "" {
		tracks = append(tracks, map[string]interface{}{"mid": videoMid, "transceiverMid": videoMid, "kind": "VIDEO", "priority": 0, "label": "", "codecs": map[string]interface{}{}, "groupId": 2, "description": ""})
	}
	b.wsSend(map[string]interface{}{
		"uid":               uuid.New().String(),
		"publisherSdpOffer": map[string]interface{}{"pcSeq": b.pubSeq, "sdp": offer.SDP, "tracks": tracks},
	})
}

func (b *Bridge) sendICE(cand *webrtc.ICECandidate, target string, pcSeq int) {
	c := cand.ToJSON()
	mid := ""
	if c.SDPMid != nil {
		mid = *c.SDPMid
	}
	var idx uint16
	if c.SDPMLineIndex != nil {
		idx = *c.SDPMLineIndex
	}
	b.wsSend(map[string]interface{}{
		"uid": uuid.New().String(),
		"webrtcIceCandidate": map[string]interface{}{
			"candidate": c.Candidate, "sdpMid": mid,
			"usernameFragment": extractUfrag(c.Candidate),
			"sdpMlineIndex": idx, "target": target, "pcSeq": pcSeq,
		},
	})
}

func (b *Bridge) requestVideoSlots() {
	log.Println("[tm-ws] -> setSlots")
	b.wsSend(map[string]interface{}{
		"uid": uuid.New().String(),
		"setSlots": map[string]interface{}{
			"slots": []map[string]interface{}{{"width": 320, "height": 240}},
			"audioSlotsCount": 1, "key": 1,
			"nLastConfig": map[string]interface{}{"nCount": 1, "showInSubgrid": false},
		},
	})
}

func (b *Bridge) handleMessage(raw []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return
	}
	uid, _ := msg["uid"].(string)

	if sh, ok := msg["serverHello"]; ok {
		log.Println("[tm-ws] <- serverHello")
		if shMap, ok := sh.(map[string]interface{}); ok {
			b.parseICEServers(shMap)
		}
		b.ack(uid)
		b.initRelay()
		return
	}

	if pa, ok := msg["publisherSdpAnswer"]; ok {
		paMap, _ := pa.(map[string]interface{})
		sdp, _ := paMap["sdp"].(string)
		log.Printf("[tm-ws] <- publisherSdpAnswer %d bytes", len(sdp))
		if err := b.relay.SetPubAnswer(sdp); err != nil {
			log.Printf("[tm-ws]    error: %v", err)
		}
		return
	}

	if so, ok := msg["subscriberSdpOffer"]; ok {
		soMap, _ := so.(map[string]interface{})
		sdp, _ := soMap["sdp"].(string)
		pcSeq, _ := soMap["pcSeq"].(float64)
		b.subSeq = int(pcSeq)
		log.Printf("[tm-ws] <- subscriberSdpOffer pcSeq=%d", b.subSeq)
		b.ack(uid)
		answer, err := b.relay.SetSubOffer(sdp)
		if err != nil {
			log.Printf("[tm-ws]    error: %v", err)
			return
		}
		log.Printf("[tm-ws] -> subscriberSdpAnswer pcSeq=%d", b.subSeq)
		b.wsSend(map[string]interface{}{
			"uid":                 uuid.New().String(),
			"subscriberSdpAnswer": map[string]interface{}{"sdp": answer.SDP, "pcSeq": b.subSeq},
		})
		b.sendPubOffer()
		b.requestVideoSlots()
		return
	}

	if ic, ok := msg["webrtcIceCandidate"]; ok {
		icMap, _ := ic.(map[string]interface{})
		candidate, _ := icMap["candidate"].(string)
		sdpMid, _ := icMap["sdpMid"].(string)
		target, _ := icMap["target"].(string)
		sdpIdx, _ := icMap["sdpMlineIndex"].(float64)
		idx := uint16(sdpIdx)
		cand := webrtc.ICECandidateInit{Candidate: candidate, SDPMid: &sdpMid, SDPMLineIndex: &idx}
		if target == "PUBLISHER" {
			b.relay.AddPubICECandidate(cand)
		} else {
			b.relay.AddSubICECandidate(cand)
		}
		b.ack(uid)
		return
	}

	if ackData, ok := msg["ack"]; ok {
		if ackMap, ok := ackData.(map[string]interface{}); ok {
			if status, ok := ackMap["status"].(map[string]interface{}); ok {
				if code, _ := status["code"].(string); code != "OK" {
					desc, _ := status["description"].(string)
					log.Printf("[tm-ws] <- ack error: %s %s", code, desc)
				}
			}
		}
		return
	}

	if ud, ok := msg["upsertDescription"]; ok {
		udMap, _ := ud.(map[string]interface{})
		descs, _ := udMap["description"].([]interface{})
		for _, d := range descs {
			dm, _ := d.(map[string]interface{})
			pid, _ := dm["id"].(string)
			if pid == "" || pid == b.connInfo.PeerID {
				continue
			}
			name := ""
			if meta, ok := dm["meta"].(map[string]interface{}); ok {
				name, _ = meta["name"].(string)
			}
			b.mu.Lock()
			b.peers[pid] = name
			b.mu.Unlock()
			log.Printf("[tm-ws] Participant joined: %s (%s) total=%d", name, pid, len(b.peers))
		}
		b.ack(uid)
		return
	}

	if rd, ok := msg["removeDescription"]; ok {
		rdMap, _ := rd.(map[string]interface{})
		ids, _ := rdMap["descriptionId"].([]interface{})
		for _, id := range ids {
			pid, _ := id.(string)
			b.mu.Lock()
			name := b.peers[pid]
			delete(b.peers, pid)
			remaining := len(b.peers)
			b.mu.Unlock()
			log.Printf("[tm-ws] Participant left: %s (%s) total=%d", name, pid, remaining)
			if remaining == 0 {
				go b.pollAndAdmit()
			}
		}
		b.ack(uid)
		return
	}

	if _, ok := msg["notification"]; ok {
		b.ack(uid)
		go b.pollAndAdmit()
		return
	}

	if _, ok := msg["participantsChanged"]; ok {
		b.ack(uid)
		go b.pollAndAdmit()
		return
	}

	if uid != "" {
		b.ack(uid)
	}
}

func (b *Bridge) parseICEServers(sh map[string]interface{}) {
	rtcCfg, ok := sh["rtcConfiguration"].(map[string]interface{})
	if !ok {
		return
	}
	servers, ok := rtcCfg["iceServers"].([]interface{})
	if !ok {
		return
	}
	var iceServers []webrtc.ICEServer
	for _, s := range servers {
		sm, _ := s.(map[string]interface{})
		var urls []string
		if u, ok := sm["urls"].([]interface{}); ok {
			for _, v := range u {
				if vs, ok := v.(string); ok {
					urls = append(urls, vs)
				}
			}
		}
		ice := webrtc.ICEServer{URLs: urls}
		if u, ok := sm["username"].(string); ok && u != "" {
			ice.Username = u
			ice.Credential, _ = sm["credential"].(string)
		}
		iceServers = append(iceServers, ice)
	}
	b.connInfo.ICEServers = iceServers
	log.Printf("[tm-ws] %d ICE servers", len(iceServers))
}

func (b *Bridge) kickPeer(peerID string) {
	confURL := url.QueryEscape(b.connInfo.ConferenceURI)
	log.Printf("[tm-ws] Kicking %s", peerID)
	tmRequest("POST", "/conferences/"+confURL+"/commands/kick?peer_id="+url.QueryEscape(peerID)+"&with_ban=false",
		nil, b.cookieStr, b.config)
}

func (b *Bridge) pollAndAdmit() {
	confURL := url.QueryEscape(b.connInfo.ConferenceURI)
	r, status, err := tmRequest("GET", "/conferences/"+confURL+"/waiting-rooms/peers", nil, b.cookieStr, b.config)
	if err != nil || status != 200 {
		return
	}
	var resp struct {
		Peers []struct {
			PeerID string `json:"peer_id"`
			State  struct {
				DisplayName string `json:"display_name"`
			} `json:"state"`
		} `json:"peers"`
	}
	json.Unmarshal(r, &resp)
	if len(resp.Peers) == 0 {
		return
	}

	b.mu.Lock()
	toKick := make(map[string]string)
	for pid, name := range b.peers {
		toKick[pid] = name
	}
	b.mu.Unlock()

	if len(toKick) > 0 {
		for pid, name := range toKick {
			log.Printf("[tm-ws] Kicking %s (%s) for new peer", name, pid)
			b.kickPeer(pid)
		}
		return
	}

	for _, p := range resp.Peers {
		log.Printf("[tm-ws] Admitting %s (%s)", p.State.DisplayName, p.PeerID)
		tmRequest("PUT", "/conferences/"+confURL+"/commands/admit?peer_id="+url.QueryEscape(p.PeerID),
			nil, b.cookieStr, b.config)
	}
}

func (b *Bridge) initRelay() {
	if b.relay != nil {
		b.relay.Close()
	}
	b.pubSeq = 1
	b.subSeq = 0

	relay := NewSFURelay()
	relay.readBufSize = b.readBuf
	relay.maxDCBuf = b.maxDCBuf
	relay.OnConnected = func(tun *tunnel.VP8DataTunnel) {
		tunnel.NewRelayBridge(tun, "creator", b.readBuf, log.Printf)
		fmt.Println("\n  TUNNEL CONNECTED\n")
	}
	relay.OnPubICE = func(cand *webrtc.ICECandidate) {
		if cand == nil {
			return
		}
		b.sendICE(cand, "PUBLISHER", b.pubSeq)
	}
	relay.OnSubICE = func(cand *webrtc.ICECandidate) {
		if cand == nil {
			return
		}
		b.sendICE(cand, "SUBSCRIBER", b.subSeq)
	}
	if err := relay.Init(b.connInfo.ICEServers); err != nil {
		log.Fatalf("[relay] init failed: %v", err)
	}
	b.relay = relay
}

func (b *Bridge) run() {
	fmt.Println("")
	fmt.Println("  CALL CREATED")
	fmt.Println("  join_link:", b.connInfo.ConferenceURI)
	fmt.Printf("  protocol:  sdk %s app %s\n\n", b.config.SDKVersion, b.config.AppVersion)

	wsHeader := http.Header{}
	wsHeader.Set("User-Agent", common.UserAgent)
	wsHeader.Set("Origin", tmOrigin)

	for {
		log.Println("[tm-ws] Connecting...")
		ws, _, err := websocket.DefaultDialer.Dial(b.connInfo.MediaServerURL, wsHeader)
		if err != nil {
			log.Printf("[tm-ws] Connect failed: %s, retrying in 5s...", common.MaskError(err))
			time.Sleep(5 * time.Second)
			continue
		}
		b.mu.Lock()
		b.ws = ws
		b.mu.Unlock()
		log.Println("[tm-ws] Connected")

		b.sendHello()

		stopPing := make(chan struct{})
		go func() {
			ticker := time.NewTicker(tmPingPeriod)
			defer ticker.Stop()
			for {
				select {
				case <-stopPing:
					return
				case <-ticker.C:
					b.wsSend(map[string]interface{}{"uid": uuid.New().String(), "ping": map[string]interface{}{}})
				}
			}
		}()

		for {
			_, raw, err := ws.ReadMessage()
			if err != nil {
				log.Printf("[tm-ws] Closed: %s", common.MaskError(err))
				break
			}
			b.handleMessage(raw)
		}

		close(stopPing)
		b.mu.Lock()
		b.ws = nil
		b.mu.Unlock()

		log.Println("[tm-ws] Rejoining in 3s...")
		time.Sleep(3 * time.Second)

		newConn, err := getConnection(b.cookieStr, url.QueryEscape(b.connInfo.ConferenceURI), b.config)
		if err != nil {
			log.Printf("[rejoin] Failed: %v, retrying in 5s...", err)
			time.Sleep(5 * time.Second)
			continue
		}
		b.connInfo.PeerID = newConn.PeerID
		b.connInfo.Credentials = newConn.Credentials
		b.connInfo.MediaServerURL = newConn.MediaServerURL
		b.connInfo.ICEServers = newConn.ICEServers
	}
}

func parseMids(sdp string) (audioMid, videoMid string) {
	var media string
	for _, line := range strings.Split(sdp, "\r\n") {
		if strings.HasPrefix(line, "m=audio") {
			media = "audio"
		} else if strings.HasPrefix(line, "m=video") {
			media = "video"
		}
		if strings.HasPrefix(line, "a=mid:") {
			mid := strings.TrimPrefix(line, "a=mid:")
			if media == "audio" && audioMid == "" {
				audioMid = mid
			} else if media == "video" && videoMid == "" {
				videoMid = mid
			}
		}
	}
	return
}

func extractUfrag(candidate string) string {
	parts := strings.Split(candidate, " ")
	for i, p := range parts {
		if p == "ufrag" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func main() {
	cookiesPath := flag.String("cookies", "", "path to cookies-yandex.json")
	cookieString := flag.String("cookie-string", "", "raw cookie string")
	resources := flag.String("resources", "default", "resource mode: default, moderate, unlimited")
	writeFile := flag.String("write-file", "", "path to file where active call link is appended")
	flag.Parse()

	var readBuf int
	var maxDCBuf uint64
	var memLimit int64
	switch *resources {
	case "moderate":
		readBuf = 16384
		maxDCBuf = 1 << 20
		memLimit = 64 << 20
	case "default":
		readBuf = 32768
		maxDCBuf = 4 << 20
		memLimit = 128 << 20
	case "unlimited":
		readBuf = common.RTPBufSize
		maxDCBuf = 8 << 20
		memLimit = 256 << 20
	default:
		log.Fatalf("[config] unknown resources mode: %s", *resources)
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
		log.Fatal("Either --cookies or --cookie-string is required")
	}

	log.Println("[config] Fetching live config from Telemost bundle...")
	cfg, err := fetchConfig()
	if err != nil {
		log.Fatalf("[config] %v", err)
	}

	connInfo, err := createAndJoinCall(cookieStr, cfg)
	if err != nil {
		log.Fatalf("Failed to create call: %v", err)
	}

	if *writeFile != "" {
		f, err := os.OpenFile(*writeFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatalf("Failed to open write-file: %v", err)
		}
		fmt.Fprintln(f, connInfo.ConferenceURI)
		f.Close()
		log.Printf("[config] Wrote call link to %s", *writeFile)
	}

	bridge := &Bridge{
		connInfo:  connInfo,
		config:    cfg,
		cookieStr: cookieStr,
		peers:     make(map[string]string),
		readBuf:   readBuf,
		maxDCBuf:  maxDCBuf,
	}
	bridge.run()
}
