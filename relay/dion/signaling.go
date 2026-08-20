package dion

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kulikov0/headlessclient/websocket"

	"whitelist-bypass/relay/common"
)

const (
	MethodServerConnected           = "server:notify:main:connected"
	MethodServerYouJoined           = "server:you_joined"
	MethodServerSubscribeResponse   = "server:main:response:subscribe:conference"
	MethodServerSDPAnswer           = "server:sdp_answer"
	MethodServerSpeakerJoined       = "server:speaker_joined"
	MethodServerSpeakerDisconnected = "server:speaker_disconnected"
	MethodServerHeartbeat           = "server:notify:main:heartbeat"
	MethodServerSpeakersResponse    = "server:response:speakers"
	MethodServerSpeakersResponseZip = "server:response:speakers_zip"

	MethodClientSubscribeConference        = "client:main:request:subscribe:conference"
	MethodClientSDPOffer                   = "client:request:media:sdp_offer"
	MethodClientSendICECandidates          = "client:request:send_ice_candidates_zip"
	MethodClientPCICEStat                  = "client:request:pc_ice_stat"
	MethodClientTrace                      = "client:trace"
	MethodClientConfSpeakersState          = "client:request:conf_speakers_state_zip"
	MethodServerConfSpeakersState          = "server:response:conf_speakers_state_zip"
	MethodClientGetVideoFromUser           = "client:request:get_video_from_user"
	MethodClientStopVideoFromUser          = "client:request:stop_video_from_user"
	MethodServerGetVideoFromUser           = "server:response:get_video_from_user"
	MethodServerStopVideoFromUser          = "server:response:stop_video_from_user"
	MethodClientCamStateChange             = "client:request:cam_state_change"
	MethodClientMicStateChange             = "client:request:mic_state_change"
	MethodClientScreenSharingSwitchOn      = "client:request:screensharing_switch_on"
	MethodClientScreenSharingSwitchOff     = "client:request:screensharing_switch_off"
	MethodClientGetScreenSharingFromUser   = "client:request:get_screensharing_from_user"
	MethodClientStopScreenSharingFromUser  = "client:request:stop_screensharing_from_user"
	MethodClientScreensharingQualityChange = "client:request:screensharing_quality_change"
	MethodServerGetScreenSharingFromUser   = "server:response:get_screensharing_from_user"
	MethodClientClientStatZip              = "client:request:client_stat_zip"
	MethodClientKickOne                    = "client:request:kick_one"
	MethodServerKickOneResponse            = "server:response:kick_one"
	MethodServerYouKicked                  = "server:you_kicked"
	MethodServerYourCamStateChanged        = "server:response:your_cam_state_changed"
	MethodServerYourMicStateChanged        = "server:response:your_mic_state_changed"
	MethodServerSpeakerCamStateChanged     = "server:speaker_cam_state_changed"
	MethodServerSpeakerMicStateChanged     = "server:speaker_mic_state_changed"

	ProductVersion      = "6.14.0"
	SubscriptionVersion = "2.0"
)

type Frame struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type TransceiverDesc struct {
	TransceiverID string `json:"transceiver_id"`
	SessionID     string `json:"session_id"`
	Direction     string `json:"direction"`
	Ctype         string `json:"ctype"`
}

type DataChannelDesc struct {
	ID    int    `json:"id"`
	Label string `json:"label"`
}

type SDPEnvelope struct {
	Type string `json:"type"`
	SDP  string `json:"sdp"`
}

type SDPOfferParams struct {
	MicState              bool              `json:"mic_state"`
	CamState              bool              `json:"cam_state"`
	NoiseSuppressionState bool              `json:"noise_suppression_state"`
	VideoQuality          *string           `json:"video_quality"`
	ScreenSharingQuality  string            `json:"screen_sharing_quality"`
	Datachannels          []DataChannelDesc `json:"datachannels"`
	Transceivers          []TransceiverDesc `json:"transceivers"`
	Offer                 string            `json:"offer"`
}

type SDPAnswerParams struct {
	Answer       string            `json:"answer"`
	Transceivers []TransceiverDesc `json:"transceivers"`
}

type ICEServerEntry struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username"`
	Credential string   `json:"credential"`
}

type YouJoinedParams struct {
	IcePolicy       string           `json:"ice_policy"`
	IceServers      []ICEServerEntry `json:"ice_servers"`
	Event           json.RawMessage  `json:"event"`
	EventParams     json.RawMessage  `json:"event_params"`
	PreferredCodecs json.RawMessage  `json:"preferred_codecs"`
}

type SpeakerJoinedParams struct {
	SessionID string          `json:"session_id"`
	UserID    string          `json:"user_id,omitempty"`
	Name      string          `json:"name,omitempty"`
	CamState  bool            `json:"cam_state"`
	MicState  bool            `json:"mic_state"`
	Extra     json.RawMessage `json:"-"`
}

type SpeakerCamStateChangedParams struct {
	SessionID string `json:"session_id"`
	CamState  bool   `json:"cam_state"`
}

type SpeakerMicStateChangedParams struct {
	SessionID string `json:"session_id"`
	MicState  bool   `json:"mic_state"`
}

type SpeakerEntry struct {
	SessionID   string `json:"session_id"`
	UserID      string `json:"user_id"`
	Name        string `json:"name"`
	MicState    bool   `json:"mic_state"`
	CamState    bool   `json:"cam_state"`
	Role        string `json:"role"`
	WebinarRole string `json:"webinar_role"`
	IsGuest     bool   `json:"is_guest"`
}

type ConfSpeakersStateResponse struct {
	SpeakersCount        int            `json:"speakers_count"`
	WebinarSpeakersCount int            `json:"webinar_speakers_count"`
	Speakers             []SpeakerEntry `json:"speakers"`
}

type ConfSpeakersStateRequest struct {
	SessionIDs        []string                 `json:"session_ids"`
	TileParams        ConfSpeakersTileParams   `json:"tile_params"`
	InputVideoQuality string                   `json:"input_video_quality"`
	ScreenParams      ConfSpeakersScreenParams `json:"screen_params"`
}

type ConfSpeakersTileParams struct {
	Mode          string                   `json:"mode"`
	MosaicParams  ConfSpeakersMosaicParams `json:"mosaic_params"`
	IsModeBlocked bool                     `json:"is_mode_blocked"`
}

type ConfSpeakersMosaicParams struct {
	MaxTilesCount int `json:"max_tiles_count"`
}

type ConfSpeakersScreenParams struct {
	Height int `json:"height"`
	Width  int `json:"width"`
}

type SpeakerDisconnectedParams struct {
	SessionID string `json:"session_id"`
}

type ICECandidateJSON struct {
	Candidate        string  `json:"candidate"`
	SDPMid           *string `json:"sdpMid"`
	SDPMLineIndex    *uint16 `json:"sdpMLineIndex"`
	UsernameFragment string  `json:"usernameFragment,omitempty"`
}

func EncodeICECandidate(candidate ICECandidateJSON) (string, error) {
	plain, err := json.Marshal(candidate)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(plain), nil
}

func DecodeICECandidate(encoded string) (ICECandidateJSON, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return ICECandidateJSON{}, fmt.Errorf("base64: %w", err)
	}
	var out ICECandidateJSON
	if err := json.Unmarshal(raw, &out); err != nil {
		return ICECandidateJSON{}, fmt.Errorf("unmarshal: %w", err)
	}
	return out, nil
}

type SignalingClient struct {
	conn      *websocket.Conn
	writeMu   sync.Mutex
	closed    atomic.Bool
	logFn     func(string, ...any)
	sessionID string
	eventID   string

	OnYouJoined                        func(YouJoinedParams)
	OnSubscribeResponse                func()
	OnSDPAnswer                        func(answerSDP string, transceivers []TransceiverDesc)
	OnSpeakerJoined                    func(SpeakerJoinedParams)
	OnSpeakerDisconnected              func(SpeakerDisconnectedParams)
	OnConfSpeakersState                func(ConfSpeakersStateResponse)
	OnSpeakerCamStateChanged           func(SpeakerCamStateChangedParams)
	OnSpeakerMicStateChanged           func(SpeakerMicStateChangedParams)
	OnGetVideoFromUserResponse         func(resp GetVideoFromUserResponse, errCode int, errMessage string)
	OnGetScreenSharingFromUserResponse func(resp GetScreenSharingFromUserResponse, errCode int, errMessage string)
	OnHeartbeat                        func()
	OnUnknown                          func(method string, params json.RawMessage)
	OnDataChannelMessage               func(method string, params json.RawMessage)
}

type SignalingDialOptions struct {
	UserAgent      string
	Origin         string
	LogFn          func(string, ...any)
	NetDialContext func(ctx context.Context, network, addr string) (net.Conn, error)
}

func DialSignaling(wssURL string, opts SignalingDialOptions) (*SignalingClient, error) {
	if !strings.Contains(wssURL, "socket_version=") {
		joiner := "&"
		if !strings.Contains(wssURL, "?") {
			joiner = "?"
		}
		wssURL = wssURL + joiner + "socket_version=2.0"
	}
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	if opts.NetDialContext != nil {
		dialer.NetDialContext = opts.NetDialContext
	}
	headers := http.Header{}
	if opts.UserAgent != "" {
		headers.Set("User-Agent", opts.UserAgent)
	}
	if opts.Origin != "" {
		headers.Set("Origin", opts.Origin)
	} else {
		headers.Set("Origin", Origin)
	}
	logFn := opts.LogFn
	if logFn == nil {
		logFn = func(string, ...any) {}
	}
	conn, resp, err := dialer.Dial(wssURL, headers)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		return nil, fmt.Errorf("ws dial: %w status=%d url=%s", err, status, wssURL)
	}
	if resp != nil {
		logFn("dion: ws dial status=%d", resp.StatusCode)
	}
	return &SignalingClient{conn: conn, logFn: logFn}, nil
}

func (c *SignalingClient) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	common.CloseWS(c.conn)
	return nil
}

func (c *SignalingClient) sendFrame(method string, params any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closed.Load() {
		return fmt.Errorf("signaling closed")
	}
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal frame: %w", err)
	}
	return c.conn.WriteMessage(websocket.TextMessage, raw)
}

func (c *SignalingClient) WaitConnected(timeout time.Duration) error {
	c.conn.SetReadDeadline(time.Now().Add(timeout))
	_, raw, err := c.conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read connected: %w", err)
	}
	var frame Frame
	if err := json.Unmarshal(raw, &frame); err != nil {
		return fmt.Errorf("decode connected: %w", err)
	}
	if frame.Method != MethodServerConnected {
		return fmt.Errorf("expected %s, got %s", MethodServerConnected, frame.Method)
	}
	c.logFn("dion: signaling connected")
	return nil
}

func (c *SignalingClient) Subscribe(eventID, sessionID string) error {
	c.eventID = eventID
	c.sessionID = sessionID
	return c.sendFrame(MethodClientSubscribeConference, map[string]any{
		"event_id":             eventID,
		"conf_user_session_id": sessionID,
		"main_user_session_id": nil,
		"product_version":      ProductVersion,
		"subscription_version": SubscriptionVersion,
	})
}

func (c *SignalingClient) SendTrace(deviceInfo map[string]any) error {
	data, err := json.Marshal(deviceInfo)
	if err != nil {
		return fmt.Errorf("marshal trace: %w", err)
	}
	return c.sendFrame(MethodClientTrace, map[string]any{"data": string(data)})
}

func (c *SignalingClient) SendSDPOffer(params SDPOfferParams) error {
	return c.sendFrame(MethodClientSDPOffer, params)
}

func (c *SignalingClient) SendConfSpeakersState(request ConfSpeakersStateRequest) error {
	encoded, err := ZipEncode(request)
	if err != nil {
		return fmt.Errorf("zip conf_speakers_state: %w", err)
	}
	return c.sendFrame(MethodClientConfSpeakersState, encoded)
}

type GetVideoFromUserRequest struct {
	SessionID     string `json:"session_id"`
	TransceiverID string `json:"transceiver_id"`
	UserID        string `json:"user_id"`
	Username      string `json:"username"`
}

type GetVideoFromUserResponse struct {
	SessionID     string `json:"session_id"`
	TransceiverID string `json:"transceiver_id"`
}

func (c *SignalingClient) SendGetVideoFromUser(request GetVideoFromUserRequest) error {
	return c.sendFrame(MethodClientGetVideoFromUser, request)
}

func (c *SignalingClient) SendStopVideoFromUser(request GetVideoFromUserRequest) error {
	return c.sendFrame(MethodClientStopVideoFromUser, request)
}

func (c *SignalingClient) SendCamStateChange(state bool) error {
	return c.sendFrame(MethodClientCamStateChange, map[string]any{"state": state})
}

func (c *SignalingClient) SendMicStateChange(state bool) error {
	return c.sendFrame(MethodClientMicStateChange, map[string]any{"state": state})
}

func (c *SignalingClient) SendScreenSharingSwitchOn() error {
	return c.sendFrame(MethodClientScreenSharingSwitchOn, map[string]any{})
}

func (c *SignalingClient) SendScreenSharingSwitchOff() error {
	return c.sendFrame(MethodClientScreenSharingSwitchOff, map[string]any{})
}

type GetScreenSharingFromUserRequest struct {
	SessionID     string `json:"session_id"`
	TransceiverID string `json:"transceiver_id"`
	UserID        string `json:"user_id"`
}

type GetScreenSharingFromUserResponse struct {
	SessionID     string `json:"session_id"`
	TransceiverID string `json:"transceiver_id"`
}

func (c *SignalingClient) SendGetScreenSharingFromUser(request GetScreenSharingFromUserRequest) error {
	return c.sendFrame(MethodClientGetScreenSharingFromUser, request)
}

// SendScreensharingQualityChange flips the SFU's screenshare quality budget
// between the two known values ("default" / "good"). Per the SPA bundle this
// is the same RPC the "Max quality screen sharing" toggle calls; the wire
// field name is just "quality".
func (c *SignalingClient) SendScreensharingQualityChange(quality string) error {
	return c.sendFrame(MethodClientScreensharingQualityChange, map[string]any{"quality": quality})
}

func (c *SignalingClient) SendKickOne(sessionID string) error {
	return c.sendFrame(MethodClientKickOne, map[string]any{"session_id": sessionID})
}

func (c *SignalingClient) SendPCIceStat() error {
	return c.sendFrame(MethodClientPCICEStat, map[string]any{"device": "web"})
}

type ClientStatVideoIn struct {
	BytesReceived            int64                `json:"bytes_received"`
	Codec                    string               `json:"codec"`
	IsEnabled                bool                 `json:"is_enabled"`
	JitterBufferDelay        float64              `json:"jitter_buffer_delay"`
	JitterBufferEmittedCount int                  `json:"jitter_buffer_emitted_count"`
	Jitter                   float64              `json:"jitter"`
	Mid                      int                  `json:"mid"`
	PacketsLost              int                  `json:"packets_lost"`
	PacketsReceived          int                  `json:"packets_received"`
	Framerate                int                  `json:"framerate"`
	FreezeCount              int                  `json:"freeze_count"`
	Resolution               ClientStatResolution `json:"resolution"`
	Rid                      string               `json:"rid"`
	TotalFreezesDuration     int                  `json:"total_freezes_duration"`
	SessionID                string               `json:"session_id"`
}

type ClientStatVideoOut struct {
	Mid                  int                   `json:"mid"`
	BytesSent            int64                 `json:"bytes_sent"`
	Codec                string                `json:"codec"`
	IsEnabled            bool                  `json:"is_enabled"`
	PacketsSent          int                   `json:"packets_sent"`
	RemoteStats          ClientStatRemoteStats `json:"remote_stats"`
	TargetBitrate        int                   `json:"target_bitrate"`
	Framerate            int                   `json:"framerate"`
	FreezeCount          int                   `json:"freeze_count"`
	Resolution           ClientStatResolution  `json:"resolution"`
	Rid                  string                `json:"rid"`
	TotalFreezesDuration int                   `json:"total_freezes_duration"`
	SessionID            string                `json:"session_id"`
	ScalabilityMode      string                `json:"scalability_mode"`
}

type ClientStatResolution struct {
	Height int `json:"height"`
	Width  int `json:"width"`
}

type ClientStatRemoteStats struct {
	Jitter              float64 `json:"jitter"`
	FractionPacketsLost float64 `json:"fraction_packets_lost"`
	PacketsLost         int     `json:"packets_lost"`
	RTT                 float64 `json:"rtt"`
}

type ClientStatAudioIn struct {
	BytesReceived            int64   `json:"bytes_received"`
	Codec                    string  `json:"codec"`
	IsEnabled                bool    `json:"is_enabled"`
	JitterBufferDelay        float64 `json:"jitter_buffer_delay"`
	JitterBufferEmittedCount int     `json:"jitter_buffer_emitted_count"`
	Jitter                   float64 `json:"jitter"`
	Mid                      int     `json:"mid"`
	PacketsLost              int     `json:"packets_lost"`
	PacketsReceived          int     `json:"packets_received"`
}

type ClientStatConnection struct {
	BytesReceived int64   `json:"bytes_received"`
	BytesSent     int64   `json:"bytes_sent"`
	CurrentRTT    float64 `json:"current_rtt"`
}

type ClientStatReport struct {
	ReportTimeUnixMS int64                `json:"report_time_unix_ms"`
	Connection       ClientStatConnection `json:"connection"`
	Audio            struct {
		In ClientStatAudioIn `json:"in"`
	} `json:"audio"`
	Video struct {
		In    []ClientStatVideoIn  `json:"in"`
		OutV2 []ClientStatVideoOut `json:"out_v2"`
		Out   ClientStatVideoOut   `json:"out"`
	} `json:"video"`
	Screensharing struct{} `json:"screensharing"`
}

func (c *SignalingClient) SendClientStatZip(report ClientStatReport) error {
	encoded, err := ZipEncode(report)
	if err != nil {
		return fmt.Errorf("zip client_stat: %w", err)
	}
	return c.sendFrame(MethodClientClientStatZip, encoded)
}

func DefaultConfSpeakersStateRequest() ConfSpeakersStateRequest {
	return ConfSpeakersStateRequest{
		SessionIDs: []string{},
		TileParams: ConfSpeakersTileParams{
			Mode:          "mosaic",
			MosaicParams:  ConfSpeakersMosaicParams{MaxTilesCount: 9},
			IsModeBlocked: false,
		},
		InputVideoQuality: "auto",
		ScreenParams:      ConfSpeakersScreenParams{Height: 720, Width: 1280},
	}
}

func (c *SignalingClient) SendICECandidates(candidates []ICECandidateJSON) error {
	encoded := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		raw, err := EncodeICECandidate(candidate)
		if err != nil {
			return fmt.Errorf("encode candidate: %w", err)
		}
		encoded = append(encoded, raw)
	}
	zipped, err := ZipEncode(map[string]any{"candidates": encoded})
	if err != nil {
		return fmt.Errorf("zip candidates: %w", err)
	}
	return c.sendFrame(MethodClientSendICECandidates, zipped)
}

func (c *SignalingClient) ReadLoop() error {
	for {
		if c.closed.Load() {
			return nil
		}
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			if c.closed.Load() {
				return nil
			}
			return fmt.Errorf("ws read: %w", err)
		}
		var frame Frame
		if err := json.Unmarshal(raw, &frame); err != nil {
			if common.Debug {
				c.logFn("dion: drop non-json frame: %v", err)
			}
			continue
		}
		if frame.Error != nil {
			if common.Debug {
				c.logFn("dion: <- %s ERROR code=%d message=%q", frame.Method, frame.Error.Code, frame.Error.Message)
			}
		}
		c.dispatch(frame)
	}
}

func (c *SignalingClient) dispatch(frame Frame) {
	switch frame.Method {
	case MethodServerConnected:
		c.logFn("dion: late server:notify:main:connected")
	case MethodServerYouJoined:
		var youJoined YouJoinedParams
		if err := json.Unmarshal(frame.Params, &youJoined); err != nil {
			c.logFn("dion: decode you_joined: %v", err)
			return
		}
		if c.OnYouJoined != nil {
			c.OnYouJoined(youJoined)
		}
	case MethodServerSubscribeResponse:
		if c.OnSubscribeResponse != nil {
			c.OnSubscribeResponse()
		}
	case MethodServerSDPAnswer:
		var answer SDPAnswerParams
		if err := json.Unmarshal(frame.Params, &answer); err != nil {
			c.logFn("dion: decode sdp_answer: %v", err)
			return
		}
		var inner SDPEnvelope
		if err := ZipDecode(answer.Answer, &inner); err != nil {
			c.logFn("dion: decode sdp_answer envelope: %v", err)
			return
		}
		if c.OnSDPAnswer != nil {
			c.OnSDPAnswer(inner.SDP, answer.Transceivers)
		}
	case MethodServerSpeakerJoined:
		var joined SpeakerJoinedParams
		if err := json.Unmarshal(frame.Params, &joined); err != nil {
			c.logFn("dion: decode speaker_joined: %v", err)
			return
		}
		joined.Extra = frame.Params
		if c.OnSpeakerJoined != nil {
			c.OnSpeakerJoined(joined)
		}
	case MethodServerSpeakerDisconnected:
		var left SpeakerDisconnectedParams
		if err := json.Unmarshal(frame.Params, &left); err != nil {
			c.logFn("dion: decode speaker_disconnected: %v", err)
			return
		}
		if c.OnSpeakerDisconnected != nil {
			c.OnSpeakerDisconnected(left)
		}
	case MethodServerSpeakerCamStateChanged:
		var changed SpeakerCamStateChangedParams
		if err := json.Unmarshal(frame.Params, &changed); err != nil {
			c.logFn("dion: decode speaker_cam_state_changed: %v", err)
			return
		}
		if c.OnSpeakerCamStateChanged != nil {
			c.OnSpeakerCamStateChanged(changed)
		}
	case MethodServerSpeakerMicStateChanged:
		var changed SpeakerMicStateChangedParams
		if err := json.Unmarshal(frame.Params, &changed); err != nil {
			c.logFn("dion: decode speaker_mic_state_changed: %v", err)
			return
		}
		if c.OnSpeakerMicStateChanged != nil {
			c.OnSpeakerMicStateChanged(changed)
		}
	case MethodServerConfSpeakersState:
		var encoded string
		if err := json.Unmarshal(frame.Params, &encoded); err != nil {
			c.logFn("dion: decode conf_speakers_state envelope: %v", err)
			return
		}
		var response ConfSpeakersStateResponse
		if err := ZipDecode(encoded, &response); err != nil {
			c.logFn("dion: decode conf_speakers_state body: %v", err)
			return
		}
		if c.OnConfSpeakersState != nil {
			c.OnConfSpeakersState(response)
		}
	case MethodServerHeartbeat:
		if c.OnHeartbeat != nil {
			c.OnHeartbeat()
		}
	case MethodServerGetVideoFromUser:
		var resp GetVideoFromUserResponse
		_ = json.Unmarshal(frame.Params, &resp)
		errCode := 0
		errMsg := ""
		if frame.Error != nil {
			errCode = frame.Error.Code
			errMsg = frame.Error.Message
		}
		if c.OnGetVideoFromUserResponse != nil {
			c.OnGetVideoFromUserResponse(resp, errCode, errMsg)
		}
	case MethodServerGetScreenSharingFromUser:
		var resp GetScreenSharingFromUserResponse
		_ = json.Unmarshal(frame.Params, &resp)
		errCode := 0
		errMsg := ""
		if frame.Error != nil {
			errCode = frame.Error.Code
			errMsg = frame.Error.Message
		}
		if c.OnGetScreenSharingFromUserResponse != nil {
			c.OnGetScreenSharingFromUserResponse(resp, errCode, errMsg)
		}
	default:
		if c.OnUnknown != nil {
			c.OnUnknown(frame.Method, frame.Params)
		}
	}
}

func BuildSDPOfferEnvelope(offerSDP string) (string, error) {
	return ZipEncode(SDPEnvelope{Type: "offer", SDP: offerSDP})
}

func DecodeSDPAnswerInner(answerZipped string) (string, error) {
	var inner SDPEnvelope
	if err := ZipDecode(answerZipped, &inner); err != nil {
		return "", err
	}
	return inner.SDP, nil
}
