package pion

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
	tmAPIBase    = "https://cloud-api.yandex.ru/telemost_front/v2/telemost"
	tmOrigin     = "https://telemost.yandex.ru"
	tmPingPeriod = 5 * time.Second
	tmUserAgent  = "Mozilla/5.0 (X11; Linux aarch64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

type TelemostHeadlessJoiner struct {
	logFn       func(string, ...any)
	OnConnected func(tunnel.DataTunnel)

	joinLink    string
	displayName string
	tunnelMode  string

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
	pubDC       *webrtc.DataChannel
	dcReady     chan struct{}

	httpClient *http.Client
	instanceID string

	peerID      string
	roomID      string
	credentials string
	serviceName string
	mediaURL    string
	iceServers  []webrtc.ICEServer
}

func NewTelemostHeadlessJoiner(logFn func(string, ...any)) *TelemostHeadlessJoiner {
	if logFn == nil {
		logFn = log.Printf
	}
	return &TelemostHeadlessJoiner{logFn: logFn, instanceID: uuid.New().String(), dcReady: make(chan struct{})}
}

func (j *TelemostHeadlessJoiner) SetJoinLink(link string) {
	j.joinLink = link
}

func (j *TelemostHeadlessJoiner) SetDisplayName(name string) {
	j.displayName = name
}

func (j *TelemostHeadlessJoiner) Run() {
	j.logFn("telemost-joiner: waiting for auth params on stdin...")
	common.EmitStatus(common.StatusReady)
	for {
		line, err := readStdinLine()
		if err != nil {
			j.logFn("telemost-joiner: stdin closed: %v", err)
			return
		}
		if strings.HasPrefix(line, "JOIN:") {
			var params struct {
				JoinLink    string `json:"joinLink"`
				DisplayName string `json:"displayName"`
				TunnelMode  string `json:"tunnelMode"`
			}
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "JOIN:")), &params); err != nil {
				j.logFn("telemost-joiner: failed to parse params: %v", err)
				continue
			}
			j.joinLink = params.JoinLink
			j.displayName = params.DisplayName
			j.tunnelMode = params.TunnelMode
			if j.displayName == "" {
				j.displayName = "Joiner"
			}
			if j.tunnelMode == "" {
				j.tunnelMode = "dc"
			}
			j.logFn("telemost-joiner: link=%s name=%s mode=%s", j.joinLink, j.displayName, j.tunnelMode)
			common.EmitStatus(common.StatusConnecting)
			break
		}
	}

	if err := j.getConnection(); err != nil {
		j.logFn("telemost-joiner: ERROR: %v", err)
		common.EmitStatusError(err.Error())
		return
	}

	j.connectAndRun()
}

var resolvingTransport = &http.Transport{
	DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, _ := net.SplitHostPort(addr)
		resolvedIP, err := requestResolve(host)
		if err != nil {
			return nil, err
		}
		return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, resolvedIP+":"+port)
	},
}

func (j *TelemostHeadlessJoiner) tmRequest(method, path string) ([]byte, int, error) {
	if j.httpClient == nil {
		j.httpClient = &http.Client{Timeout: 15 * time.Second, Transport: resolvingTransport}
	}
	req, err := http.NewRequest(method, tmAPIBase+path, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", tmUserAgent)
	req.Header.Set("Origin", tmOrigin)
	req.Header.Set("Referer", tmOrigin+"/")
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

	r, status, err := j.tmRequest("GET", connPath)
	if err != nil {
		return fmt.Errorf("get connection: %w", err)
	}
	if status != 200 {
		return fmt.Errorf("get connection: status %d: %s", status, string(r))
	}

	var initial struct {
		ConnectionType string `json:"connection_type"`
		ClientConfig   struct {
			CheckInterval int `json:"conference_check_access_interval_ms"`
		} `json:"client_configuration"`
	}
	json.Unmarshal(r, &initial)

	if initial.ConnectionType == "WAITING_ROOM" {
		interval := initial.ClientConfig.CheckInterval
		if interval <= 0 {
			interval = 3000
		}
		j.logFn("telemost-joiner: in waiting room, polling every %dms...", interval)
		for {
			time.Sleep(time.Duration(interval) * time.Millisecond)
			r, status, err = j.tmRequest("GET", connPath)
			if err != nil {
				return fmt.Errorf("waiting room poll: %w", err)
			}
			if status != 200 {
				return fmt.Errorf("waiting room poll: status %d", status)
			}
			json.Unmarshal(r, &initial)
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
	json.Unmarshal(r, &conn)
	if conn.ClientConfig.MediaServerURL == "" {
		return fmt.Errorf("empty media_server_url: %s", string(r))
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
			"sdkInfo":             map[string]interface{}{"implementation": "browser", "version": "5.27.0", "userAgent": tmUserAgent, "hwConcurrency": 8},
			"sdkInitializationId": uuid.New().String(),
			"disablePublisher": false, "disableSubscriber": false, "disableSubscriberAudio": false,
		},
	})
	j.logFn("telemost-joiner: -> hello")
}

func (j *TelemostHeadlessJoiner) sendICE(cand *webrtc.ICECandidate, target string, pcSeq int) {
	c := cand.ToJSON()
	j.wsSend(map[string]interface{}{
		"uid": uuid.New().String(),
		"webrtcIceCandidate": map[string]interface{}{
			"candidate": c.Candidate, "sdpMid": *c.SDPMid,
			"sdpMlineIndex": *c.SDPMLineIndex, "target": target, "pcSeq": pcSeq,
		},
	})
}

func (j *TelemostHeadlessJoiner) initPC() {
	config := webrtc.Configuration{ICEServers: j.iceServers}

	settingEngine := webrtc.SettingEngine{}
	settingEngine.SetNet(&AndroidNet{})
	settingEngine.DetachDataChannels()
	api := webrtc.NewAPI(webrtc.WithSettingEngine(settingEngine))

	// Subscriber PC - receives data from SFU
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
			common.EmitStatusError("subscriber connection failed")
		}
	})

	subPC.OnDataChannel(func(dc *webrtc.DataChannel) {
		j.logFn("telemost-joiner: sub incoming DC: label=%s id=%d", dc.Label(), dc.ID())
		if dc.Label() == "sharing" {
			j.setupIncomingDC(dc)
		}
	})

	subPC.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		j.logFn("telemost-joiner: sub remote track: %s", track.Codec().MimeType)
		if j.tunnelMode == "video" {
			go ReadTrack(track, j.vp8tunnel, j.logFn, "telemost-joiner")
		} else {
			go drainTrack(track)
		}
	})

	// Publisher PC - sends data to SFU
	pubPC, err := api.NewPeerConnection(config)
	if err != nil {
		j.logFn("telemost-joiner: ERROR: create pub PC: %v", err)
		return
	}
	j.pubPC = pubPC
	j.pubSeq = 1

	j.sampleTrack = AddTunnelTracks(pubPC, j.logFn, "telemost-joiner [pub]")
	j.createSharingDC()

	pubPC.OnICECandidate(func(cand *webrtc.ICECandidate) {
		if cand != nil {
			j.sendICE(cand, "PUBLISHER", j.pubSeq)
		}
	})

	pubPC.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		j.logFn("telemost-joiner: pub PC state: %s", state.String())
		if state == webrtc.PeerConnectionStateConnected && j.tunnelMode == "video" && j.vp8tunnel == nil {
			j.logFn("telemost-joiner: === VP8 TUNNEL CONNECTED ===")
			common.EmitStatus(common.StatusTunnelConnected)
			j.vp8tunnel = tunnel.NewVP8DataTunnel(j.sampleTrack, j.logFn)
			j.vp8tunnel.Start(25)
			if j.OnConnected != nil {
				j.OnConnected(j.vp8tunnel)
			}
		}
	})

	j.logFn("telemost-joiner: sub+pub PCs created with %d ICE servers, mode=%s", len(j.iceServers), j.tunnelMode)
}

func (j *TelemostHeadlessJoiner) createSharingDC() {
	ordered := true
	dc, err := j.pubPC.CreateDataChannel("sharing", &webrtc.DataChannelInit{Ordered: &ordered})
	if err != nil {
		j.logFn("telemost-joiner: failed to create sharing DC: %v", err)
		return
	}
	j.pubDC = dc
	dc.OnOpen(func() {
		j.logFn("telemost-joiner: pub sharing DC open")
		if j.tunnelMode == "dc" {
			go func() {
				for {
					select {
					case <-j.dcReady:
						return
					default:
					}
					if dc.ReadyState() != webrtc.DataChannelStateOpen {
						return
					}
					j.logFn("telemost-joiner: sending tunnel:ping on pub DC")
					if err := dc.SendText("tunnel:ping"); err != nil {
						j.logFn("telemost-joiner: tunnel:ping send error: %v", err)
					}
					select {
					case <-j.dcReady:
						return
					case <-time.After(5 * time.Second):
					}
				}
			}()
		}
	})
	dc.OnClose(func() {
		j.logFn("telemost-joiner: pub sharing DC closed")
	})
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		if msg.IsString {
			s := string(msg.Data)
			if s == "tunnel:ping" {
				dc.SendText("tunnel:pong")
				return
			}
		}
	})
}

func (j *TelemostHeadlessJoiner) setupIncomingDC(dc *webrtc.DataChannel) {
	j.logFn("telemost-joiner: incoming sharing DC found")

	dc.OnOpen(func() {
		j.logFn("telemost-joiner: incoming sharing DC open")
		if j.tunnelMode == "dc" {
			go j.waitForPong(dc)
		}
	})
}

func (j *TelemostHeadlessJoiner) waitForPong(dc *webrtc.DataChannel) {
	raw, err := dc.Detach()
	if err != nil {
		j.logFn("telemost-joiner: incoming DC detach failed: %v", err)
		return
	}
	buf := make([]byte, 1024)
	for {
		n, isString, err := raw.ReadDataChannel(buf)
		if err != nil {
			j.logFn("telemost-joiner: incoming DC read error: %v", err)
			return
		}
		if isString {
			s := string(buf[:n])
			j.logFn("telemost-joiner: incoming DC text: %s", s)
			if s == "tunnel:pong" {
				close(j.dcReady)
				j.logFn("telemost-joiner: === DC TUNNEL CONNECTED ===")
				common.EmitStatus(common.StatusTunnelConnected)
				dcTunnel := tunnel.NewChunkedDCTunnel(raw, j.pubDC, common.DCBufSize, j.logFn)
				if j.OnConnected != nil {
					j.OnConnected(dcTunnel)
				}
				return
			}
		}
	}
}

func drainTrack(track *webrtc.TrackRemote) {
	buf := make([]byte, 4096)
	for {
		if _, _, err := track.Read(buf); err != nil {
			return
		}
	}
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

	audioMid, videoMid := tmParseMids(offer.SDP)
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
	for _, c := range j.pubPending {
		j.pubPC.AddICECandidate(c)
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

	for _, c := range j.subPending {
		j.subPC.AddICECandidate(c)
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
					name := ""
					if meta, ok := dm["meta"].(map[string]interface{}); ok {
						name, _ = meta["name"].(string)
					}
					j.logFn("telemost-joiner: participant: %s (%s)", name, pid)
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
					urls = append(urls, fixICEURL(vs))
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
			host := extractICEHost(u)
			if host == "" || net.ParseIP(host) != nil {
				continue
			}
			ip, ok := resolved[host]
			if !ok {
				var err error
				ip, err = requestResolve(host)
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
	wsHeader := http.Header{}
	wsHeader.Set("User-Agent", tmUserAgent)
	wsHeader.Set("Origin", tmOrigin)

	j.logFn("telemost-joiner: connecting to %s", j.mediaURL)
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		NetDialContext:   resolvingTransport.DialContext,
	}
	ws, _, err := dialer.Dial(j.mediaURL, wsHeader)
	if err != nil {
		j.logFn("telemost-joiner: ERROR: ws connect: %s", common.MaskError(err))
		common.EmitStatusError("ws connect failed")
		return
	}
	j.ws = ws
	j.logFn("telemost-joiner: ws connected")

	j.sendHello()

	stopPing := make(chan struct{})
	go func() {
		ticker := time.NewTicker(tmPingPeriod)
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
}

func extractICEHost(iceURL string) string {
	idx := strings.Index(iceURL, ":")
	if idx < 0 {
		return ""
	}
	rest := iceURL[idx+1:]
	params := strings.Index(rest, "?")
	if params >= 0 {
		rest = rest[:params]
	}
	host, _, err := net.SplitHostPort(rest)
	if err != nil {
		return rest
	}
	return host
}

func tmParseMids(sdp string) (audioMid, videoMid string) {
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
