package telemost

import (
	"encoding/json"
	"fmt"
	"io"
	mathrand "math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kulikov0/headless-client"
	"github.com/kulikov0/headless-client/webrtc"
	"github.com/pion/interceptor"
	"whitelist-bypass/relay/headlessapi"
)

const (
	APIBase = "https://cloud-api.yandex.ru/telemost_front/v2/telemost"
	Origin  = "https://telemost.yandex.ru"
)

var CapabilitiesOffer = map[string][]string{
	"offerAnswerMode":                       {"SEPARATE"},
	"initialSubscriberOffer":                {"ON_HELLO"},
	"slotsMode":                             {"FROM_CONTROLLER"},
	"simulcastMode":                         {"DISABLED", "STATIC"},
	"selfVadStatus":                         {"FROM_SERVER", "FROM_CLIENT"},
	"dataChannelSharing":                    {"TO_RTP"},
	"videoEncoderConfig":                    {"NO_CONFIG", "ONLY_INIT_CONFIG", "RUNTIME_CONFIG"},
	"dataChannelVideoCodec":                 {"VP8", "UNIQUE_CODEC_FROM_TRACK_DESCRIPTION"},
	"bandwidthLimitationReason":             {"BANDWIDTH_REASON_DISABLED", "BANDWIDTH_REASON_ENABLED"},
	"sdkDefaultDeviceManagement":            {"SDK_DEFAULT_DEVICE_MANAGEMENT_DISABLED", "SDK_DEFAULT_DEVICE_MANAGEMENT_ENABLED"},
	"joinOrderLayout":                       {"JOIN_ORDER_LAYOUT_DISABLED", "JOIN_ORDER_LAYOUT_ENABLED"},
	"pinLayout":                             {"PIN_LAYOUT_DISABLED"},
	"sendSelfViewVideoSlot":                 {"SEND_SELF_VIEW_VIDEO_SLOT_DISABLED", "SEND_SELF_VIEW_VIDEO_SLOT_ENABLED"},
	"serverLayoutTransition":                {"SERVER_LAYOUT_TRANSITION_DISABLED"},
	"sdkPublisherOptimizeBitrate":           {"SDK_PUBLISHER_OPTIMIZE_BITRATE_DISABLED", "SDK_PUBLISHER_OPTIMIZE_BITRATE_FULL", "SDK_PUBLISHER_OPTIMIZE_BITRATE_ONLY_SELF"},
	"sdkNetworkLostDetection":               {"SDK_NETWORK_LOST_DETECTION_DISABLED"},
	"sdkNetworkPathMonitor":                 {"SDK_NETWORK_PATH_MONITOR_DISABLED"},
	"publisherVp9":                          {"PUBLISH_VP9_DISABLED", "PUBLISH_VP9_ENABLED"},
	"svcMode":                               {"SVC_MODE_DISABLED", "SVC_MODE_L3T3", "SVC_MODE_L3T3_KEY"},
	"subscriberOfferAsyncAck":               {"SUBSCRIBER_OFFER_ASYNC_ACK_DISABLED", "SUBSCRIBER_OFFER_ASYNC_ACK_ENABLED"},
	"subscriberDtlsPassiveMode":             {"SUBSCRIBER_DTLS_PASSIVE_MODE_DISABLED", "SUBSCRIBER_DTLS_PASSIVE_MODE_ENABLED"},
	"androidBluetoothRoutingFix":            {"ANDROID_BLUETOOTH_ROUTING_FIX_DISABLED"},
	"fixedIceCandidatesPoolSize":            {"FIXED_ICE_CANDIDATES_POOL_SIZE_DISABLED"},
	"sdkAndroidTelecomIntegration":          {"SDK_ANDROID_TELECOM_INTEGRATION_DISABLED"},
	"setActiveCodecsMode":                   {"SET_ACTIVE_CODECS_MODE_DISABLED", "SET_ACTIVE_CODECS_MODE_VIDEO_ONLY"},
	"publisherOpusDred":                     {"PUBLISHER_OPUS_DRED_DISABLED"},
	"publisherOpusLowBitrate":               {"PUBLISHER_OPUS_LOW_BITRATE_DISABLED"},
	"sdkAndroidDestroySessionOnTaskRemoved": {"SDK_ANDROID_DESTROY_SESSION_ON_TASK_REMOVED_DISABLED"},
	"svcModes":                              {"FALSE"},
	"reportTelemetryModes":                  {"TRUE"},
	"keepDefaultDevicesModes":               {"FALSE"},
}

var StartupSlotSizes = [][][2]int{
	{{0, 0}, {0, 0}, {0, 0}, {0, 0}, {0, 0}, {0, 0}, {0, 0}, {0, 0}, {0, 0}, {0, 0}, {0, 0}, {0, 0}},
	{{464, 261}, {464, 261}, {464, 261}, {336, 189}, {272, 153}, {272, 153}, {272, 153}, {272, 153}, {224, 126}, {224, 126}, {224, 126}, {224, 126}},
	{{464, 261}, {464, 261}, {464, 261}, {336, 189}, {272, 153}, {272, 153}, {272, 153}, {272, 153}, {224, 126}, {224, 126}, {224, 126}, {224, 126}},
	{{672, 378}, {672, 378}, {464, 261}, {336, 189}, {320, 180}, {320, 180}, {320, 180}, {320, 180}, {272, 153}, {272, 153}, {224, 126}, {224, 126}},
}

type SlotBindEvent struct {
	Slot          int
	ParticipantID string
	Mid           string
	Reason        string
}

type Client struct {
	HTTP       *http.Client
	Cookie     string
	UserAgent  string
	AppVersion string
	InstanceID string
}

func NewAPI(configure func(*webrtc.SettingEngine)) (*webrtc.API, error) {
	mediaEngine := &webrtc.MediaEngine{}
	if err := mediaEngine.RegisterDefaultCodecs(); err != nil {
		return nil, err
	}
	if err := headless.ChromeWindows.RegisterHeaderExtensions(mediaEngine); err != nil {
		return nil, fmt.Errorf("telemost: %w", err)
	}
	registry := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(mediaEngine, registry); err != nil {
		return nil, err
	}
	return headlessapi.WebRTCAPI(
		headlessapi.Options{
			Profile:            headless.ChromeWindows,
			AnswerAsDTLSServer: true,
			Configure:          configure,
		},
		webrtc.WithMediaEngine(mediaEngine),
		webrtc.WithInterceptorRegistry(registry),
	)
}

func NewPeerConnection(config webrtc.Configuration) (*webrtc.PeerConnection, error) {
	api, err := NewAPI(nil)
	if err != nil {
		return nil, err
	}
	return api.NewPeerConnection(config)
}

func MungeSDPAddVideoContent(sdp string) string {
	lines := strings.Split(sdp, "\r\n")
	out := make([]string, 0, len(lines)+4)
	inVideo := false
	inserted := false
	for _, line := range lines {
		if strings.HasPrefix(line, "m=") {
			if inVideo && !inserted {
				out = append(out, "a=content:speaker,main")
				inserted = true
			}
			inVideo = strings.HasPrefix(line, "m=video")
			inserted = false
		}
		out = append(out, line)
		if inVideo && !inserted && strings.HasPrefix(line, "a=mid:") {
			out = append(out, "a=content:speaker,main")
			inserted = true
		}
	}
	return strings.Join(out, "\r\n")
}

func SlotsConfigBindings(v interface{}) []SlotBindEvent {
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	slots, _ := m["slots"].([]interface{})
	var out []SlotBindEvent
	for idx, s := range slots {
		sm, _ := s.(map[string]interface{})
		if pv, _ := sm["participantVideoByMid"].(map[string]interface{}); pv != nil {
			pid, _ := pv["participantId"].(string)
			mid, _ := pv["mid"].(string)
			reason, _ := pv["limitationReason"].(string)
			out = append(out, SlotBindEvent{Slot: idx, ParticipantID: pid, Mid: mid, Reason: reason})
			continue
		}
		if p, _ := sm["participant"].(map[string]interface{}); p != nil {
			pid, _ := p["participantId"].(string)
			out = append(out, SlotBindEvent{Slot: idx, ParticipantID: pid})
		}
	}
	return out
}

func ScreenShareBindings(v interface{}) []SlotBindEvent {
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	slots, _ := m["slots"].([]interface{})
	var out []SlotBindEvent
	for idx, s := range slots {
		sm, _ := s.(map[string]interface{})
		if ss, _ := sm["participantScreenSharingByMid"].(map[string]interface{}); ss != nil {
			pid, _ := ss["participantId"].(string)
			mid, _ := ss["mid"].(string)
			reason, _ := ss["limitationReason"].(string)
			out = append(out, SlotBindEvent{Slot: idx, ParticipantID: pid, Mid: mid, Reason: reason})
		}
	}
	return out
}

func BriefJSON(v interface{}) string {
	const max = 240
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("<json err: %v>", err)
	}
	if len(b) > max {
		return string(b[:max]) + "...(+" + fmt.Sprintf("%d", len(b)-max) + "B)"
	}
	return string(b)
}

func (c *Client) Do(method, path string, body interface{}) ([]byte, int, error) {
	var bodyReader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		bodyReader = strings.NewReader(string(data))
	}
	req, err := http.NewRequest(method, APIBase+path, bodyReader)
	if err != nil {
		return nil, 0, err
	}
	instanceID := c.InstanceID
	if instanceID == "" {
		instanceID = uuid.New().String()
	}
	req.Header = headless.ChromeWindows.Headers(headless.DestEmpty)
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	req.Header.Set("Origin", Origin)
	req.Header.Set("Referer", Origin+"/")
	req.Header.Set("Client-Instance-Id", instanceID)
	if c.Cookie != "" {
		req.Header.Set("Cookie", c.Cookie)
	}
	if c.AppVersion != "" {
		req.Header.Set("X-Telemost-Client-Version", c.AppVersion)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := c.HTTP
	if client == nil {
		client = headless.ChromeWindows.HTTPClient()
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return data, resp.StatusCode, err
}

func (c *Client) RequestStates(joinURI, peerID string) error {
	confURL := url.QueryEscape(joinURI)
	body := map[string]interface{}{
		"peers":       []map[string]string{{"peer_id": peerID}},
		"permissions": map[string]interface{}{},
		"conference":  map[string]interface{}{"version": -1},
	}
	r, status, err := c.Do("POST", "/conferences/"+confURL+"/request-states", body)
	if err != nil {
		return err
	}
	if status != 200 {
		return fmt.Errorf("status %d: %s", status, string(r))
	}
	return nil
}

func jitterSize(width int, rnd *mathrand.Rand) (int, int) {
	if width == 0 {
		return 0, 0
	}
	w := width + rnd.Intn(11) - 5
	return w, w * 9 / 16
}

func slotsMessageWithSizes(key int, template [][2]int, rnd *mathrand.Rand) map[string]interface{} {
	slots := make([]map[string]interface{}, len(template))
	for i, wh := range template {
		w, h := wh[0], wh[1]
		if rnd != nil {
			w, h = jitterSize(wh[0], rnd)
		}
		slots[i] = map[string]interface{}{"width": w, "height": h}
	}
	return map[string]interface{}{
		"uid": uuid.New().String(),
		"setSlots": map[string]interface{}{
			"slots":              slots,
			"audioSlotsCount":    0,
			"key":                key,
			"shutdownAllVideo":   nil,
			"withSelfView":       true,
			"selfViewVisibility": "ON_LOADING_THEN_SHOW",
			"gridConfig":         map[string]interface{}{},
		},
	}
}

func SetSlotsMessage(key int) map[string]interface{} {
	rnd := mathrand.New(mathrand.NewSource(time.Now().UnixNano()))
	return slotsMessageWithSizes(key, StartupSlotSizes[len(StartupSlotSizes)-1], rnd)
}

func StartupSetSlotsMessage(i, key int) map[string]interface{} {
	rnd := mathrand.New(mathrand.NewSource(time.Now().UnixNano() + int64(i)))
	return slotsMessageWithSizes(key, StartupSlotSizes[i], rnd)
}

func SetSlotsOffsetMessage(offset int) map[string]interface{} {
	return map[string]interface{}{
		"uid":            uuid.New().String(),
		"setSlotsOffset": map[string]interface{}{"offset": offset},
	}
}

func SdkCodecsInfoMessage() map[string]interface{} {
	return map[string]interface{}{
		"uid": uuid.New().String(),
		"sdkCodecsInfo": map[string]interface{}{
			"vp8": map[string]interface{}{
				"supported": "CODEC_FEATURE_SUPPORTED",
				"hwDecode":  "CODEC_FEATURE_NOT_SUPPORTED",
				"hwEncode":  "CODEC_FEATURE_NOT_SUPPORTED",
				"isoString": "vp8",
			},
		},
	}
}

func UpdateMeMessage(name string, sendVideo, sendSharing bool) map[string]interface{} {
	meta := map[string]interface{}{
		"name": name, "description": "", "role": "SPEAKER",
		"sendAudio": false, "sendVideo": sendVideo,
	}
	return map[string]interface{}{
		"uid": uuid.New().String(),
		"updateMe": map[string]interface{}{
			"participantMeta":       meta,
			"participantAttributes": map[string]interface{}{"name": name, "role": "SPEAKER", "description": ""},
			"sendAudio":             false,
			"sendVideo":             sendVideo,
			"sendSharing":           sendSharing,
		},
	}
}

func DisplayVideoTrack(label string) map[string]interface{} {
	return map[string]interface{}{
		"kind": "DISPLAY_VIDEO", "label": label, "priority": 0, "dcLabel": "sharing", "mid": "",
		"codecs":  map[string]interface{}{"96": map[string]interface{}{"channels": 0, "clockRate": 90000, "mimeType": "video/VP8", "sdpFmtpLine": ""}},
		"groupId": 2, "description": "",
	}
}

func UpdatePublisherSharingTrackMessage(label string) map[string]interface{} {
	return map[string]interface{}{
		"uid": uuid.New().String(),
		"updatePublisherTrackDescription": map[string]interface{}{
			"publisherTrackDescriptions": []map[string]interface{}{DisplayVideoTrack(label)},
		},
	}
}

func UpdatePublisherTrackDescriptionMessage(pc *webrtc.PeerConnection, audioLabel, videoLabel string) map[string]interface{} {
	descs := []map[string]interface{}{}
	for _, tr := range pc.GetTransceivers() {
		sender := tr.Sender()
		if sender == nil || sender.Track() == nil {
			continue
		}
		kind := strings.ToUpper(sender.Track().Kind().String())
		mid := tr.Mid()
		label := videoLabel
		groupId := 2
		if kind == "AUDIO" {
			label = audioLabel
			groupId = 1
		}
		descs = append(descs, map[string]interface{}{
			"mid":            mid,
			"transceiverMid": mid,
			"kind":           kind,
			"priority":       0,
			"label":          label,
			"codecs":         map[string]interface{}{},
			"groupId":        groupId,
			"description":    "",
		})
	}
	return map[string]interface{}{
		"uid": uuid.New().String(),
		"updatePublisherTrackDescription": map[string]interface{}{
			"publisherTrackDescriptions": descs,
		},
	}
}
