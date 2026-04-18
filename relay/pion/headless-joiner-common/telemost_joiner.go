package joiner

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
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
	TmAPIBase    = "https://cloud-api.yandex.ru/telemost_front/v2/telemost"
	TmOrigin     = "https://telemost.yandex.ru"
	TmPingPeriod = 5 * time.Second
)

type TelemostHeadlessJoiner struct {
	logFn       func(string, ...any)
	OnConnected func(tunnel.DataTunnel)
	ResolveFn      ResolveFunc
	Status         StatusEmitter
	PCConfig       PeerConnectionConfigurer
	AddTracks      AddTunnelTracksFunc
	ReadTrackFn    ReadTrackFunc

	joinLink    string
	displayName string

	ws   *websocket.Conn
	wsMu sync.Mutex

	subPC        *webrtc.PeerConnection
	subSeq       int
	subRemoteSet bool
	subPending   []webrtc.ICECandidateInit

	pubPC        *webrtc.PeerConnection
	pubSeq       int
	pubRemoteSet bool
	pubPending   []webrtc.ICECandidateInit

	sampleTrack *webrtc.TrackLocalStaticSample
	vp8tunnel   *tunnel.VP8DataTunnel

	httpClient *http.Client
	instanceID string

	peerID      string
	roomID      string
	credentials string
	serviceName string
	mediaURL    string
	iceServers  []webrtc.ICEServer
}

func NewTelemostHeadlessJoiner(logFn func(string, ...any), resolveFn ResolveFunc, status StatusEmitter, pcConfig PeerConnectionConfigurer, addTracks AddTunnelTracksFunc, readTrackFn ReadTrackFunc) *TelemostHeadlessJoiner {
	return &TelemostHeadlessJoiner{
		logFn:       logFn,
		ResolveFn:   resolveFn,
		Status:      status,
		PCConfig:    pcConfig,
		AddTracks:   addTracks,
		ReadTrackFn: readTrackFn,
		instanceID:  uuid.New().String(),
	}
}

func (j *TelemostHeadlessJoiner) RunWithParams(jsonParams string) {
	var params struct {
		JoinLink    string `json:"joinLink"`
		DisplayName string `json:"displayName"`
	}
	if err := json.Unmarshal([]byte(jsonParams), &params); err != nil {
		j.logFn("telemost-joiner: failed to parse params: %v", err)
		j.Status.EmitStatusError("bad params: " + err.Error())
		return
	}
	j.joinLink = params.JoinLink
	j.displayName = params.DisplayName
	if j.displayName == "" {
		j.displayName = "Joiner"
	}
	j.logFn("telemost-joiner: link=%s name=%s mode=video", j.joinLink, j.displayName)
	j.Status.EmitStatus(common.StatusConnecting)

	if err := j.getConnection(); err != nil {
		j.logFn("telemost-joiner: ERROR: %v", err)
		j.Status.EmitStatusError(err.Error())
		return
	}

	j.connectAndRun()
}

func (j *TelemostHeadlessJoiner) Close() {
	j.wsMu.Lock()
	ws := j.ws
	j.ws = nil
	j.wsMu.Unlock()
	if ws != nil {
		ws.Close()
	}
	if j.vp8tunnel != nil {
		j.vp8tunnel.Stop()
	}
	if j.subPC != nil {
		j.subPC.Close()
	}
	if j.pubPC != nil {
		j.pubPC.Close()
	}
}

func (j *TelemostHeadlessJoiner) makeHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, port, _ := net.SplitHostPort(addr)
				resolvedIP, err := j.ResolveFn(host)
				if err != nil {
					return nil, err
				}
				return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, resolvedIP+":"+port)
			},
		},
	}
}

func (j *TelemostHeadlessJoiner) tmRequest(method, path string) ([]byte, int, error) {
	if j.httpClient == nil {
		j.httpClient = j.makeHTTPClient()
	}
	req, err := http.NewRequest(method, TmAPIBase+path, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", common.UserAgent)
	req.Header.Set("Origin", TmOrigin)
	req.Header.Set("Referer", TmOrigin+"/")
	req.Header.Set("Client-Instance-Id", j.instanceID)
	resp, err := j.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return data, resp.StatusCode, err
}

func (j *TelemostHeadlessJoiner) getConnection() error {
	confURL := url.QueryEscape(j.joinLink)
	name := url.QueryEscape(j.displayName)
	if name == "" {
		name = "Joiner"
	}
	connPath := "/conferences/" + confURL + "/connection?next_gen_media_platform_allowed=true&display_name=" + name + "&waiting_room_supported=true"

	j.logFn("telemost-joiner: getting connection for %s", j.joinLink)

	responseBody, status, err := j.tmRequest("GET", connPath)
	if err != nil {
		return fmt.Errorf("get connection: %w", err)
	}
	if status != 200 {
		return fmt.Errorf("get connection: status %d: %s", status, string(responseBody))
	}

	var initial struct {
		ConnectionType string `json:"connection_type"`
		ClientConfig   struct {
			CheckInterval int `json:"conference_check_access_interval_ms"`
		} `json:"client_configuration"`
	}
	json.Unmarshal(responseBody, &initial)

	if initial.ConnectionType == "WAITING_ROOM" {
		interval := initial.ClientConfig.CheckInterval
		if interval <= 0 {
			interval = 3000
		}
		j.logFn("telemost-joiner: in waiting room, polling every %dms...", interval)
		for {
			time.Sleep(time.Duration(interval) * time.Millisecond)
			responseBody, status, err = j.tmRequest("GET", connPath)
			if err != nil {
				return fmt.Errorf("waiting room poll: %w", err)
			}
			if status != 200 {
				return fmt.Errorf("waiting room poll: status %d", status)
			}
			json.Unmarshal(responseBody, &initial)
			if initial.ConnectionType != "WAITING_ROOM" {
				j.logFn("telemost-joiner: admitted!")
				break
			}
		}
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
	json.Unmarshal(responseBody, &conn)
	if conn.ClientConfig.MediaServerURL == "" {
		return fmt.Errorf("empty media_server_url: %s", string(responseBody))
	}

	j.peerID = conn.PeerID
	j.roomID = conn.RoomID
	j.credentials = conn.Credentials
	j.mediaURL = conn.ClientConfig.MediaServerURL
	j.serviceName = conn.ClientConfig.ServiceName

	var rawIce []struct {
		URLs       []string `json:"urls"`
		Username   string   `json:"username"`
		Credential string   `json:"credential"`
	}
	json.Unmarshal(conn.ClientConfig.ICEServers, &rawIce)
	for _, s := range rawIce {
		ice := webrtc.ICEServer{URLs: s.URLs}
		if s.Username != "" {
			ice.Username = s.Username
			ice.Credential = s.Credential
		}
		j.iceServers = append(j.iceServers, ice)
	}

	j.logFn("telemost-joiner: peer_id=%s room_id=%s media_url=%s", j.peerID, j.roomID, j.mediaURL)
	return nil
}

func (j *TelemostHeadlessJoiner) wsSend(msg interface{}) {
	j.wsMu.Lock()
	defer j.wsMu.Unlock()
	if j.ws != nil {
		j.ws.WriteJSON(msg)
	}
}

func (j *TelemostHeadlessJoiner) ack(uid string) {
	if uid == "" {
		return
	}
	j.wsSend(map[string]interface{}{
		"uid": uid,
		"ack": map[string]interface{}{
			"status": map[string]interface{}{"code": "OK", "description": ""},
		},
	})
}

func (j *TelemostHeadlessJoiner) sendHello() {
	j.wsSend(map[string]interface{}{
		"uid": uuid.New().String(),
		"hello": map[string]interface{}{
			"participantMeta":       map[string]interface{}{"name": j.displayName, "role": "SPEAKER", "description": "", "sendAudio": false, "sendVideo": true},
			"participantAttributes": map[string]interface{}{"name": j.displayName, "role": "SPEAKER", "description": ""},
			"sendAudio": false, "sendVideo": true, "sendSharing": false,
			"participantId": j.peerID, "roomId": j.roomID,
			"serviceName": j.serviceName, "credentials": j.credentials,
			"capabilitiesOffer": map[string][]string{
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
				"subscriberDtlsPassiveMode":    {"SUBSCRIBER_DTLS_PASSIVE_MODE_DISABLED", "SUBSCRIBER_DTLS_PASSIVE_MODE_ENABLED"},
			},
			"sdkInfo":             map[string]interface{}{"implementation": "browser", "version": "5.27.0", "userAgent": common.UserAgent, "hwConcurrency": 8},
			"sdkInitializationId": uuid.New().String(),
			"disablePublisher": false, "disableSubscriber": false, "disableSubscriberAudio": false,
		},
	})
	j.logFn("telemost-joiner: -> hello")
}

func (j *TelemostHeadlessJoiner) sendICE(cand *webrtc.ICECandidate, target string, pcSeq int) {
	candidate := cand.ToJSON()
	j.wsSend(map[string]interface{}{
		"uid": uuid.New().String(),
		"webrtcIceCandidate": map[string]interface{}{
			"candidate": candidate.Candidate, "sdpMid": *candidate.SDPMid,
			"sdpMlineIndex": *candidate.SDPMLineIndex, "target": target, "pcSeq": pcSeq,
		},
	})
}

func (j *TelemostHeadlessJoiner) initPC() {
	config := webrtc.Configuration{ICEServers: j.iceServers}

	settingEngine := webrtc.SettingEngine{}
	settingEngine.DetachDataChannels()
	if j.PCConfig != nil {
		j.PCConfig.ConfigureSettingEngine(&settingEngine)
	}
	api := webrtc.NewAPI(webrtc.WithSettingEngine(settingEngine))

	subPC, err := api.NewPeerConnection(config)
	if err != nil {
		j.logFn("telemost-joiner: ERROR: create sub PC: %v", err)
		return
	}
	j.subPC = subPC

	subPC.OnICECandidate(func(cand *webrtc.ICECandidate) {
		if cand != nil {
			j.sendICE(cand, "SUBSCRIBER", j.subSeq)
		}
	})

	subPC.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		j.logFn("telemost-joiner: sub PC state: %s", state.String())
		if state == webrtc.PeerConnectionStateFailed {
			j.logFn("telemost-joiner: ERROR: subscriber connection failed")
			j.Status.EmitStatusError("subscriber connection failed")
		}
	})

	subPC.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		j.logFn("telemost-joiner: sub remote track: %s", track.Codec().MimeType)
		go j.ReadTrackFn(track, j.vp8tunnel, j.logFn, "telemost-joiner")
	})

	pubPC, err := api.NewPeerConnection(config)
	if err != nil {
		j.logFn("telemost-joiner: ERROR: create pub PC: %v", err)
		return
	}
	j.pubPC = pubPC
	j.pubSeq = 1

	j.sampleTrack = j.AddTracks(pubPC, j.logFn, "telemost-joiner [pub]")

	pubPC.OnICECandidate(func(cand *webrtc.ICECandidate) {
		if cand != nil {
			j.sendICE(cand, "PUBLISHER", j.pubSeq)
		}
	})

	pubPC.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		j.logFn("telemost-joiner: pub PC state: %s", state.String())
		if state == webrtc.PeerConnectionStateConnected && j.vp8tunnel == nil {
			j.logFn("telemost-joiner: === VP8 TUNNEL CONNECTED ===")
			j.Status.EmitStatus(common.StatusTunnelConnected)
			j.vp8tunnel = tunnel.NewVP8DataTunnel(j.sampleTrack, j.logFn)
			j.vp8tunnel.Start(25)
			if j.OnConnected != nil {
				j.OnConnected(j.vp8tunnel)
			}
		}
	})

	j.logFn("telemost-joiner: sub+pub PCs created with %d ICE servers", len(j.iceServers))
}

func (j *TelemostHeadlessJoiner) sendPubOffer() {
	if j.pubPC == nil {
		return
	}

	offer, err := j.pubPC.CreateOffer(nil)
	if err != nil {
		j.logFn("telemost-joiner: pub offer failed: %v", err)
		return
	}
	j.pubPC.SetLocalDescription(offer)

	audioMid, videoMid := TmParseMids(offer.SDP)
	j.logFn("telemost-joiner: -> publisherSdpOffer pcSeq=%d audioMid=%s videoMid=%s", j.pubSeq, audioMid, videoMid)

	var tracks []map[string]interface{}
	if audioMid != "" {
		tracks = append(tracks, map[string]interface{}{"mid": audioMid, "transceiverMid": audioMid, "kind": "AUDIO", "priority": 0, "label": "", "codecs": map[string]interface{}{}, "groupId": 1, "description": ""})
	}
	if videoMid != "" {
		tracks = append(tracks, map[string]interface{}{"mid": videoMid, "transceiverMid": videoMid, "kind": "VIDEO", "priority": 0, "label": "", "codecs": map[string]interface{}{}, "groupId": 2, "description": ""})
	}
	j.wsSend(map[string]interface{}{
		"uid":               uuid.New().String(),
		"publisherSdpOffer": map[string]interface{}{"pcSeq": j.pubSeq, "sdp": offer.SDP, "tracks": tracks},
	})
}

func (j *TelemostHeadlessJoiner) handlePubAnswer(sdp string) {
	if j.pubPC == nil {
		return
	}
	err := j.pubPC.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  sdp,
	})
	if err != nil {
		j.logFn("telemost-joiner: set pub remote desc: %v", err)
		return
	}
	j.pubRemoteSet = true
	for _, candidate := range j.pubPending {
		j.pubPC.AddICECandidate(candidate)
	}
	j.pubPending = nil
}

func (j *TelemostHeadlessJoiner) requestVideoSlots() {
	j.logFn("telemost-joiner: -> setSlots")
	j.wsSend(map[string]interface{}{
		"uid": uuid.New().String(),
		"setSlots": map[string]interface{}{
			"slots":           []map[string]interface{}{{"width": 320, "height": 240}},
			"audioSlotsCount": 1, "key": 1,
			"nLastConfig": map[string]interface{}{"nCount": 1, "showInSubgrid": false},
		},
	})
}

func (j *TelemostHeadlessJoiner) handleSubOffer(sdp string, pcSeq int) {
	j.subSeq = pcSeq

	if j.subPC == nil {
		j.logFn("telemost-joiner: sub PC not ready for offer")
		return
	}

	err := j.subPC.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  sdp,
	})
	if err != nil {
		j.logFn("telemost-joiner: set sub remote desc: %v", err)
		return
	}
	j.subRemoteSet = true

	for _, candidate := range j.subPending {
		j.subPC.AddICECandidate(candidate)
	}
	j.subPending = nil

	answer, err := j.subPC.CreateAnswer(nil)
	if err != nil {
		j.logFn("telemost-joiner: create sub answer: %v", err)
		return
	}
	j.subPC.SetLocalDescription(answer)

	j.logFn("telemost-joiner: -> subscriberSdpAnswer pcSeq=%d", pcSeq)
	j.wsSend(map[string]interface{}{
		"uid":                 uuid.New().String(),
		"subscriberSdpAnswer": map[string]interface{}{"sdp": answer.SDP, "pcSeq": pcSeq},
	})

	j.sendPubOffer()
	j.requestVideoSlots()
}

func (j *TelemostHeadlessJoiner) handleMessage(raw []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return
	}
	uid, _ := msg["uid"].(string)

	if _, ok := msg["serverHello"]; ok {
		j.logFn("telemost-joiner: <- serverHello")
		if sh, ok := msg["serverHello"].(map[string]interface{}); ok {
			j.parseICEServersFromHello(sh)
		}
		j.ack(uid)
		j.initPC()
		return
	}

	if so, ok := msg["subscriberSdpOffer"]; ok {
		soMap, _ := so.(map[string]interface{})
		sdp, _ := soMap["sdp"].(string)
		pcSeq, _ := soMap["pcSeq"].(float64)
		j.logFn("telemost-joiner: <- subscriberSdpOffer pcSeq=%d len=%d", int(pcSeq), len(sdp))
		j.ack(uid)
		j.handleSubOffer(sdp, int(pcSeq))
		return
	}

	if pa, ok := msg["publisherSdpAnswer"]; ok {
		paMap, _ := pa.(map[string]interface{})
		sdp, _ := paMap["sdp"].(string)
		j.logFn("telemost-joiner: <- publisherSdpAnswer %d bytes", len(sdp))
		j.handlePubAnswer(sdp)
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

		if target == "SUBSCRIBER" {
			if j.subRemoteSet {
				j.subPC.AddICECandidate(cand)
			} else {
				j.subPending = append(j.subPending, cand)
			}
		} else if target == "PUBLISHER" {
			if j.pubRemoteSet {
				j.pubPC.AddICECandidate(cand)
			} else {
				j.pubPending = append(j.pubPending, cand)
			}
		}
		j.ack(uid)
		return
	}

	if ackData, ok := msg["ack"]; ok {
		if ackMap, ok := ackData.(map[string]interface{}); ok {
			if status, ok := ackMap["status"].(map[string]interface{}); ok {
				if code, _ := status["code"].(string); code != "OK" {
					desc, _ := status["description"].(string)
					j.logFn("telemost-joiner: ack error: %s %s", code, desc)
				}
			}
		}
		return
	}

	if ud, ok := msg["upsertDescription"]; ok {
		udMap, _ := ud.(map[string]interface{})
		if descs, ok := udMap["description"].([]interface{}); ok {
			for _, d := range descs {
				dm, _ := d.(map[string]interface{})
				pid, _ := dm["id"].(string)
				if pid != "" && pid != j.peerID {
					participantName := ""
					if meta, ok := dm["meta"].(map[string]interface{}); ok {
						participantName, _ = meta["name"].(string)
					}
					j.logFn("telemost-joiner: participant: %s (%s)", participantName, pid)
				}
			}
		}
		j.ack(uid)
		return
	}

	if _, ok := msg["removeDescription"]; ok {
		j.logFn("telemost-joiner: participant left")
		j.ack(uid)
		return
	}

	if uid != "" {
		j.ack(uid)
	}
}

func (j *TelemostHeadlessJoiner) parseICEServersFromHello(sh map[string]interface{}) {
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
					urls = append(urls, common.FixICEURL(vs))
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
	resolved := make(map[string]string)
	for i, s := range iceServers {
		for k, u := range s.URLs {
			host := common.ExtractICEHost(u)
			if host == "" || net.ParseIP(host) != nil {
				continue
			}
			ip, ok := resolved[host]
			if !ok {
				var err error
				ip, err = j.ResolveFn(host)
				if err != nil {
					j.logFn("telemost-joiner: resolve ICE host %s failed: %s", common.MaskAddr(host), common.MaskError(err))
					continue
				}
				resolved[host] = ip
				j.logFn("telemost-joiner: resolved ICE host %s -> %s", host, ip)
			}
			iceServers[i].URLs[k] = strings.Replace(u, host, ip, 1)
		}
	}
	j.iceServers = iceServers
	for i, s := range iceServers {
		j.logFn("telemost-joiner: ICE server %d: urls=%v", i, s.URLs)
	}
	j.logFn("telemost-joiner: %d ICE servers from serverHello", len(iceServers))
}

func (j *TelemostHeadlessJoiner) connectAndRun() {
	parsed, err := url.Parse(j.mediaURL)
	if err != nil {
		j.logFn("telemost-joiner: ERROR: bad media URL: %s", common.MaskError(err))
		j.Status.EmitStatusError("bad media URL")
		return
	}

	hostname := parsed.Hostname()
	resolvedIP, err := j.ResolveFn(hostname)
	if err != nil {
		j.logFn("telemost-joiner: ERROR: DNS resolve failed: %s", common.MaskError(err))
		j.Status.EmitStatusError("DNS resolve failed")
		return
	}
	j.logFn("telemost-joiner: resolved %s -> %s", common.MaskAddr(hostname), common.MaskAddr(resolvedIP))

	wsHeader := http.Header{}
	wsHeader.Set("User-Agent", common.UserAgent)
	wsHeader.Set("Origin", TmOrigin)

	j.logFn("telemost-joiner: connecting to %s", j.mediaURL)
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		WriteBufferSize:  65536,
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: true, ServerName: hostname},
		NetDialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			_, port, _ := net.SplitHostPort(addr)
			return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, resolvedIP+":"+port)
		},
	}
	ws, _, err := dialer.Dial(j.mediaURL, wsHeader)
	if err != nil {
		j.logFn("telemost-joiner: ERROR: ws connect: %s", common.MaskError(err))
		j.Status.EmitStatusError("ws connect failed")
		return
	}
	j.wsMu.Lock()
	j.ws = ws
	j.wsMu.Unlock()
	j.logFn("telemost-joiner: ws connected")

	j.sendHello()

	stopPing := make(chan struct{})
	go func() {
		ticker := time.NewTicker(TmPingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-stopPing:
				return
			case <-ticker.C:
				j.wsSend(map[string]interface{}{"uid": uuid.New().String(), "ping": map[string]interface{}{}})
			}
		}
	}()

	for {
		_, raw, err := ws.ReadMessage()
		if err != nil {
			j.logFn("telemost-joiner: ws read error: %s", common.MaskError(err))
			break
		}
		j.handleMessage(raw)
	}

	close(stopPing)
	if j.vp8tunnel != nil {
		j.vp8tunnel.Stop()
	}
	if j.subPC != nil {
		j.subPC.Close()
	}
	if j.pubPC != nil {
		j.pubPC.Close()
	}
	j.logFn("telemost-joiner: disconnected")
	j.Status.EmitStatus(common.StatusTunnelLost)
}
