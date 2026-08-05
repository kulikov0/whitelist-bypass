package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
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
	tmapi "whitelist-bypass/relay/telemost"
	"whitelist-bypass/relay/tunnel"
)

const (
	tmAPIBase    = tmapi.APIBase
	tmOrigin     = tmapi.Origin
	tmPingPeriod = 5 * time.Second
)

var clientInstanceID = uuid.New().String()

type ConnInfo struct {
	ConferenceURI       string
	RoomID              string
	PeerID              string
	Credentials         string
	MediaServerURL      string
	ServiceName         string
	ICEServers          []webrtc.ICEServer
	StateCheckIntervalS int
}

type Bridge struct {
	mu            sync.Mutex
	ws            *websocket.Conn
	relay         *SFURelay
	connInfo      *ConnInfo
	config        TMConfig
	cookieStr     string
	pubSeq        int
	subSeq        int
	peers         map[string]string
	readBuf       int
	activeBridge  *tunnel.RelayBridge
	selfName      string
	upstreamSocks string
	upstreamUser  string
	upstreamPass  string

	setSlotsKey    int
	initBundleSent bool
	pendingKicks   map[string]chan struct{}
	boundPeers     map[string]bool
	unboundPeers   map[string]bool
}

func tmRequest(method, path string, body interface{}, cookieStr string, cfg TMConfig) ([]byte, int, error) {
	c := tmapi.Client{Cookie: cookieStr, AppVersion: cfg.AppVersion, InstanceID: clientInstanceID}
	return c.Do(method, path, body)
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
			MediaServerURL         string          `json:"media_server_url"`
			ServiceName            string          `json:"service_name"`
			ICEServers             json.RawMessage `json:"ice_servers"`
			StateCheckIntervalSecs int             `json:"state_check_interval_seconds"`
		} `json:"client_configuration"`
	}
	json.Unmarshal(r, &conn)
	if conn.ClientConfig.MediaServerURL == "" {
		return nil, fmt.Errorf("empty media_server_url: %s", string(r))
	}
	return &ConnInfo{
		RoomID:              conn.RoomID,
		PeerID:              conn.PeerID,
		Credentials:         conn.Credentials,
		MediaServerURL:      conn.ClientConfig.MediaServerURL,
		ServiceName:         conn.ClientConfig.ServiceName,
		ICEServers:          parseICEServersJSON(conn.ClientConfig.ICEServers),
		StateCheckIntervalS: conn.ClientConfig.StateCheckIntervalSecs,
	}, nil
}

func joinExistingConference(cookieStr, conferenceURI string, cfg TMConfig) (*ConnInfo, error) {
	conferenceURI = strings.TrimSpace(conferenceURI)
	if conferenceURI == "" {
		return nil, fmt.Errorf("empty -tm-link")
	}
	log.Printf("[auth] Joining existing conference: %s", conferenceURI)
	info, err := getConnection(cookieStr, url.QueryEscape(conferenceURI), cfg)
	if err != nil {
		return nil, err
	}
	info.ConferenceURI = conferenceURI
	log.Printf("[auth] peer_id=%s room_id=%s", info.PeerID, info.RoomID)
	log.Printf("[auth] media_server=%s", info.MediaServerURL)
	return info, nil
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
	b.mu.Lock()
	b.selfName = "Headless"
	b.mu.Unlock()
	b.wsSend(map[string]interface{}{
		"uid": uuid.New().String(),
		"hello": map[string]interface{}{
			"participantMeta":       map[string]interface{}{"name": "Headless", "role": "SPEAKER", "description": "", "sendAudio": false, "sendVideo": true},
			"participantAttributes": map[string]interface{}{"name": "Headless", "role": "SPEAKER", "description": ""},
			"sendAudio":             false, "sendVideo": true, "sendSharing": false,
			"participantId": b.connInfo.PeerID, "roomId": b.connInfo.RoomID,
			"serviceName": b.connInfo.ServiceName, "credentials": b.connInfo.Credentials,
			"capabilitiesOffer":   tmapi.CapabilitiesOffer,
			"sdkInfo":             map[string]interface{}{"implementation": "browser", "version": b.config.SDKVersion, "userAgent": common.UserAgent, "hwConcurrency": 8},
			"sdkInitializationId": uuid.New().String(),
			"disablePublisher":    false, "disableSubscriber": false, "disableSubscriberAudio": false,
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
			"sdpMlineIndex":    idx, "target": target, "pcSeq": pcSeq,
		},
	})
}

func (b *Bridge) requestVideoSlots() {
	b.setSlotsKey++
	log.Printf("[tm-ws] -> setSlots key=%d", b.setSlotsKey)
	b.wsSend(tmapi.SetSlotsMessage(b.setSlotsKey))
}

func (b *Bridge) forceReconnect(reason string) {
	oldPeerID := b.connInfo.PeerID
	log.Printf("[tm-ws] forcing reconnect: %s", reason)
	if oldPeerID != "" {
		log.Printf("[tm-ws] kicking self pid=%s to leave call cleanly", oldPeerID)
		if err := b.kickPeer(oldPeerID); err != nil {
			log.Printf("[tm-ws] self-kick failed: %v", err)
		}
	}
	clientInstanceID = uuid.New().String()
	log.Printf("[tm-ws] new instance-id=%s", clientInstanceID)
	b.mu.Lock()
	ws := b.ws
	b.mu.Unlock()
	common.CloseWS(ws)
}

func (b *Bridge) sendInitBundle() {
	if b.initBundleSent {
		return
	}
	b.initBundleSent = true
	log.Printf("[tm-ws] -> sdkCodecsInfo + updatePublisherTrackDescription")
	b.wsSend(tmapi.SdkCodecsInfoMessage())
	b.wsSend(tmapi.UpdatePublisherTrackDescriptionMessage(b.relay.pubPC, "Microphone", "MacBook Pro Camera (0000:0001)"))
	b.sendStartupSlotsRamp()
}

func (b *Bridge) sendStartupSlotsRamp() {
	for i := 0; i < 4; i++ {
		b.setSlotsKey++
		log.Printf("[tm-ws] -> setSlots key=%d (startup %d/4)", b.setSlotsKey, i+1)
		b.wsSend(tmapi.StartupSetSlotsMessage(i, b.setSlotsKey))
	}
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
		log.Println("[tm-ws] -> setSlotsOffset")
		b.wsSend(tmapi.SetSlotsOffsetMessage(0))
		b.initRelay()
		return
	}

	if pa, ok := msg["publisherSdpAnswer"]; ok {
		paMap, _ := pa.(map[string]interface{})
		sdp, _ := paMap["sdp"].(string)
		log.Printf("[tm-ws] <- publisherSdpAnswer %d bytes", len(sdp))
		if err := b.relay.SetPubAnswer(sdp); err != nil {
			log.Printf("[tm-ws]    error: %v", err)
			return
		}
		b.sendInitBundle()
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

	if ud, ok := msg["updateDescription"]; ok {
		if common.Debug {
			log.Printf("[tm-ws] <- updateDescription %s", tmapi.BriefJSON(ud))
		}
		udMap, _ := ud.(map[string]interface{})
		descs, _ := udMap["description"].([]interface{})
		b.applyDescriptionSnapshot(descs)
		b.ack(uid)
		return
	}

	if ud, ok := msg["upsertDescription"]; ok {
		udMap, _ := ud.(map[string]interface{})
		descs, _ := udMap["description"].([]interface{})
		for _, d := range descs {
			dm, _ := d.(map[string]interface{})
			b.applyDescriptionEntry(dm)
		}
		b.kickStaleSelves()
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
			ch, hadPendingKick := b.pendingKicks[pid]
			if hadPendingKick {
				delete(b.pendingKicks, pid)
			}
			b.mu.Unlock()
			log.Printf("[tm-ws] Participant left: %s (%s) total=%d", name, pid, remaining)
			if hadPendingKick {
				close(ch)
			}
			if remaining == 0 {
				go b.pollAndAdmit()
			}
		}
		b.ack(uid)
		return
	}

	if n, ok := msg["notification"]; ok {
		log.Printf("[tm-ws] <- notification %s", tmapi.BriefJSON(n))
		b.ack(uid)
		go b.pollAndAdmit()
		return
	}

	if pc, ok := msg["participantsChanged"]; ok {
		log.Printf("[tm-ws] <- participantsChanged %s", tmapi.BriefJSON(pc))
		b.ack(uid)
		go b.pollAndAdmit()
		return
	}

	if sc, ok := msg["slotsConfig"]; ok {
		if common.Debug {
			log.Printf("[tm-ws] <- slotsConfig %s", tmapi.BriefJSON(sc))
		}
		needRebind := false
		presentPids := make(map[string]bool)
		for _, ev := range tmapi.SlotsConfigBindings(sc) {
			fullPid := ev.ParticipantID
			if fullPid != "" {
				presentPids[fullPid] = true
			}
			pid := fullPid
			if len(pid) > 8 {
				pid = pid[:8]
			}
			if ev.Reason == "NO_LIMITATION" && ev.Mid != "" {
				log.Printf("[bind] BOUND slot=%d pid=%s mid=%s", ev.Slot, pid, ev.Mid)
				b.mu.Lock()
				if b.boundPeers == nil {
					b.boundPeers = make(map[string]bool)
				}
				b.boundPeers[fullPid] = true
				delete(b.unboundPeers, fullPid)
				b.mu.Unlock()
			} else if fullPid != "" {
				b.mu.Lock()
				wasBound := b.boundPeers[fullPid]
				if wasBound {
					if b.unboundPeers == nil {
						b.unboundPeers = make(map[string]bool)
					}
					b.unboundPeers[fullPid] = true
					delete(b.boundPeers, fullPid)
				}
				b.mu.Unlock()
				if wasBound {
					log.Printf("[bind] KILL slot=%d pid=%s reason=%s - rebinding", ev.Slot, pid, ev.Reason)
					needRebind = true
				} else {
					log.Printf("[bind] UNBOUND slot=%d pid=%s reason=%s mid=%q", ev.Slot, pid, ev.Reason, ev.Mid)
				}
			}
		}
		b.mu.Lock()
		for boundPid := range b.boundPeers {
			if !presentPids[boundPid] {
				short := boundPid
				if len(short) > 8 {
					short = short[:8]
				}
				log.Printf("[bind] VANISHED pid=%s - rebinding", short)
				delete(b.boundPeers, boundPid)
				needRebind = true
			}
		}
		b.mu.Unlock()
		if needRebind {
			go b.forceReconnect("slot binding killed")
		}
		b.ack(uid)
		return
	}

	for k, v := range msg {
		if k == "uid" || k == "ack" {
			continue
		}
		log.Printf("[tm-ws] <- %s (unhandled) %s", k, tmapi.BriefJSON(v))
		break
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

func (b *Bridge) requestStates() error {
	c := tmapi.Client{Cookie: b.cookieStr, AppVersion: b.config.AppVersion, InstanceID: clientInstanceID}
	return c.RequestStates(b.connInfo.ConferenceURI, b.connInfo.PeerID)
}

func (b *Bridge) applyDescriptionEntry(dm map[string]interface{}) {
	pid, _ := dm["id"].(string)
	if pid == "" {
		return
	}
	name := ""
	if meta, ok := dm["meta"].(map[string]interface{}); ok {
		name, _ = meta["name"].(string)
	}
	if pid == b.connInfo.PeerID {
		b.mu.Lock()
		if name != "" {
			b.selfName = name
		}
		b.mu.Unlock()
		return
	}
	_, disconnected := dm["disconnectedAt"]
	b.mu.Lock()
	_, wasKnown := b.peers[pid]
	if disconnected {
		delete(b.peers, pid)
	} else {
		b.peers[pid] = name
	}
	total := len(b.peers)
	b.mu.Unlock()
	switch {
	case disconnected && wasKnown:
		log.Printf("[tm-ws] Participant left: %s (%s) total=%d", name, pid, total)
	case disconnected:
		log.Printf("[tm-ws] Ghost participant: %s (%s) - kicking", name, pid)
		go b.kickPeer(pid)
	case !wasKnown:
		log.Printf("[tm-ws] Participant joined: %s (%s) total=%d", name, pid, total)
	}
}

func (b *Bridge) applyDescriptionSnapshot(descs []interface{}) {
	b.mu.Lock()
	b.peers = make(map[string]string)
	b.mu.Unlock()
	for _, d := range descs {
		dm, _ := d.(map[string]interface{})
		b.applyDescriptionEntry(dm)
	}
	b.kickStaleSelves()
}

func (b *Bridge) kickStaleSelves() {
	b.mu.Lock()
	selfName := b.selfName
	stale := make([]string, 0)
	if selfName != "" {
		for pid, name := range b.peers {
			if name == selfName {
				stale = append(stale, pid)
			}
		}
	}
	b.mu.Unlock()
	for _, pid := range stale {
		log.Printf("[tm-ws] Kicking stale self %s (name=%q)", pid, selfName)
		b.kickPeer(pid)
		b.mu.Lock()
		delete(b.peers, pid)
		b.mu.Unlock()
	}
}

func (b *Bridge) kickPeer(peerID string) error {
	confURL := url.QueryEscape(b.connInfo.ConferenceURI)
	log.Printf("[tm-ws] Kicking %s", peerID)
	body, status, err := tmRequest("POST", "/conferences/"+confURL+"/commands/kick?peer_id="+url.QueryEscape(peerID)+"&with_ban=false",
		nil, b.cookieStr, b.config)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("kick %s status %d: %s", peerID, status, string(body))
	}
	return nil
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
	if b.pendingKicks == nil {
		b.pendingKicks = make(map[string]chan struct{})
	}
	toKick := make(map[string]string, len(b.peers))
	waits := make(map[string]<-chan struct{}, len(b.peers))
	for pid, name := range b.peers {
		toKick[pid] = name
		ch := make(chan struct{})
		b.pendingKicks[pid] = ch
		waits[pid] = ch
	}
	b.mu.Unlock()

	for pid, name := range toKick {
		log.Printf("[tm-ws] Kicking %s (%s) for one-to-one", name, pid)
		if err := b.kickPeer(pid); err != nil {
			log.Printf("[tm-ws] kick failed: %v", err)
			b.mu.Lock()
			delete(b.pendingKicks, pid)
			b.mu.Unlock()
			return
		}
	}

	for pid, ch := range waits {
		<-ch
		log.Printf("[tm-ws] kick confirmed for %s", pid)
	}

	p := resp.Peers[0]
	log.Printf("[tm-ws] Admitting %s (%s)", p.State.DisplayName, p.PeerID)
	tmRequest("PUT", "/conferences/"+confURL+"/commands/admit?peer_id="+url.QueryEscape(p.PeerID),
		nil, b.cookieStr, b.config)
}

func (b *Bridge) initRelay() {
	if b.relay != nil {
		b.relay.Close()
	}
	b.pubSeq = 1
	b.subSeq = 0
	b.initBundleSent = false

	relay := NewSFURelay()
	relay.readBufSize = b.readBuf
	obf, err := tunnel.NewTunnelObfuscator(tunnel.DeriveSecretFromJoinLink(b.connInfo.ConferenceURI))
	if err != nil {
		log.Fatalf("[relay] obfuscator init failed: %v", err)
	}
	relay.SetObfuscator(obf)
	log.Printf("[relay] obfuscator localEpoch=0x%08x", obf.LocalEpoch())
	relay.OnPubReady = func() {
		log.Printf("[relay] pub PC connected")
	}
	relay.OnConnected = func(tun *tunnel.VP8DataTunnel) {
		if b.activeBridge != nil {
			b.activeBridge.Reset()
		}
		b.activeBridge = tunnel.NewRelayBridge(tun, "creator", common.VP8BufSize, log.Printf)
		b.activeBridge.SetUpstreamSocks(b.upstreamSocks, b.upstreamUser, b.upstreamPass)
		fmt.Print("\n  TUNNEL CONNECTED\n")
	}
	relay.OnPeerRestart = func() {
		if b.activeBridge != nil {
			log.Printf("[relay] new peer detected, resetting relay bridge")
			b.activeBridge.Reset()
		}
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
		go b.pollAndAdmit()

		stopWaitingRoomPoll := make(chan struct{})
		go func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-stopWaitingRoomPoll:
					return
				case <-ticker.C:
					b.pollAndAdmit()
				}
			}
		}()

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

		stopStateKeepalive := make(chan struct{})
		go func() {
			interval := b.connInfo.StateCheckIntervalS
			if interval <= 0 {
				interval = 30
			}
			if err := b.requestStates(); err != nil {
				log.Printf("[tm-state] initial request-states: %v", err)
			}
			ticker := time.NewTicker(time.Duration(interval) * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-stopStateKeepalive:
					return
				case <-ticker.C:
					if err := b.requestStates(); err != nil {
						log.Printf("[tm-state] request-states: %v", err)
					}
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
		close(stopStateKeepalive)
		close(stopWaitingRoomPoll)
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
		b.connInfo.StateCheckIntervalS = newConn.StateCheckIntervalS
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
	common.MaybePrintVersion()
	cookiesPath := flag.String("cookies", "", "path to cookies-yandex.json")
	cookieString := flag.String("cookie-string", "", "raw cookie string")
	tmLink := flag.String("tm-link", "", "Telemost conference URI to join an existing conference")
	resources := flag.String("resources", "default", "resource mode: default, moderate, unlimited, custom")
	customReadBuf := flag.Int("read-buf", 0, "read buffer size, used with -resources custom")
	customMemLimit := flag.Int64("mem-limit", 0, "memory limit in bytes, used with -resources custom")
	writeFile := flag.String("write-file", "", "path to file where active call link is appended")
	upstreamSocks := flag.String("upstream-socks", "", "route tunneled egress through this SOCKS5 proxy (host:port), e.g. a local VPN client")
	upstreamUser := flag.String("upstream-user", "", "upstream SOCKS5 username")
	upstreamPass := flag.String("upstream-pass", "", "upstream SOCKS5 password")
	debugFlag := flag.Bool("debug", false, "verbose debug logging")
	allowPrivate := flag.Bool("allow-private-dst", false, "let the joiner reach private/internal addresses through this creator")
	flag.Parse()
	common.Debug = *debugFlag
	common.AllowPrivateDst = *allowPrivate

	var readBuf int
	var memLimit int64
	switch *resources {
	case "moderate":
		readBuf = 16384
		memLimit = 64 << 20
	case "default":
		readBuf = 32768
		memLimit = 128 << 20
	case "unlimited":
		readBuf = common.RTPBufSize
		memLimit = 256 << 20
	case "custom":
		readBuf = *customReadBuf
		if readBuf == 0 {
			readBuf = common.RTPBufSize
		}
		memLimit = *customMemLimit
		if memLimit == 0 {
			memLimit = 256 << 20
		}
	default:
		log.Fatalf("[config] unknown resources mode: %s (use moderate, default, unlimited, custom)", *resources)
	}
	if memLimit > 0 {
		debug.SetMemoryLimit(memLimit)
	}
	log.Printf("[config] resources=%s read-buf=%d mem-limit=%d", *resources, readBuf, memLimit)

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

	log.Println("[config] Fetching live config from Telemost bundle...")
	cfg, err := fetchConfig()
	if err != nil {
		log.Fatalf("[config] %v", err)
	}

	var connInfo *ConnInfo
	if *tmLink != "" {
		connInfo, err = joinExistingConference(cookieStr, *tmLink, cfg)
		if err != nil {
			log.Fatalf("Failed to join existing conference: %v", err)
		}
	} else {
		connInfo, err = createAndJoinCall(cookieStr, cfg)
		if err != nil {
			log.Fatalf("Failed to create call: %v", err)
		}
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
		connInfo:      connInfo,
		config:        cfg,
		cookieStr:     cookieStr,
		peers:         make(map[string]string),
		readBuf:       readBuf,
		upstreamSocks: *upstreamSocks,
		upstreamUser:  *upstreamUser,
		upstreamPass:  *upstreamPass,
	}
	bridge.run()
}
