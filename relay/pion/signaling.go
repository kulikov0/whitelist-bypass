package pion

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
	"whitelist-bypass/relay/common"
)

type SignalingMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
	ID   int             `json:"id,omitempty"`
	Role string          `json:"role,omitempty"`
}

type ICEServerConfig struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

type SDPMessage struct {
	Type string `json:"type"`
	SDP  string `json:"sdp"`
}

type ICECandidateMessage struct {
	Candidate     string `json:"candidate"`
	SDPMid        string `json:"sdpMid"`
	SDPMLineIndex uint16 `json:"sdpMLineIndex"`
}

var WsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

var iceLogFn func(string, ...any)

func ParseICEServers(data json.RawMessage) ([]webrtc.ICEServer, error) {
	var servers []ICEServerConfig
	if err := json.Unmarshal(data, &servers); err != nil {
		return nil, err
	}
	iceServers := make([]webrtc.ICEServer, len(servers))
	for i, s := range servers {
		urls := make([]string, len(s.URLs))
		for j, u := range s.URLs {
			fixed := common.FixICEURL(u)
			if iceLogFn != nil && fixed != u {
				iceLogFn("ice: fix URL %q -> %q", u, fixed)
			}
			urls[j] = fixed
		}
		if iceLogFn != nil {
			iceLogFn("ice: server %d: urls=%v", i, urls)
		}
		iceServers[i] = webrtc.ICEServer{
			URLs: urls, Username: s.Username, Credential: s.Credential,
		}
	}
	return iceServers, nil
}

func NewPionAPI(localIP string) *webrtc.API {
	se := webrtc.SettingEngine{}
	se.SetNet(&common.AndroidNet{LocalIP: localIP})
	// Telemost SFU is DTLS-active for subscribers; Pion must answer passive.
	se.SetAnsweringDTLSRole(webrtc.DTLSRoleServer)
	return webrtc.NewAPI(webrtc.WithSettingEngine(se))
}

type WSHelper struct {
	wsConn *websocket.Conn
	mu     sync.Mutex
}

func (h *WSHelper) SetConn(ws *websocket.Conn) {
	h.mu.Lock()
	h.wsConn = ws
	h.mu.Unlock()
}

func (h *WSHelper) SendToHook(msgType string, data any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.wsConn == nil {
		return
	}
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return
	}
	msg := SignalingMessage{Type: msgType, Data: dataBytes}
	msgBytes, _ := json.Marshal(msg)
	h.wsConn.WriteMessage(websocket.TextMessage, msgBytes)
}

func (h *WSHelper) SendToHookWithRole(msgType string, data any, role string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.wsConn == nil {
		return
	}
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return
	}
	msg := SignalingMessage{Type: msgType, Data: dataBytes, Role: role}
	msgBytes, _ := json.Marshal(msg)
	h.wsConn.WriteMessage(websocket.TextMessage, msgBytes)
}

func (h *WSHelper) SendResponse(id int, data any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.wsConn == nil {
		return
	}
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return
	}
	msg := SignalingMessage{Type: "response", Data: dataBytes, ID: id}
	msgBytes, _ := json.Marshal(msg)
	h.wsConn.WriteMessage(websocket.TextMessage, msgBytes)
}

func (h *WSHelper) ReadMessages(handler func([]byte), onDisconnect func()) {
	for {
		_, msg, err := h.wsConn.ReadMessage()
		if err != nil {
			onDisconnect()
			return
		}
		handler(msg)
	}
}

func AddTunnelTracks(pc *webrtc.PeerConnection, logFn func(string, ...any), prefix string, dualTrack bool) []*webrtc.TrackLocalStaticSample {
	trackVideo, _ := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8},
		"video", "tunnel-video",
	)
	
	audioTrack, _ := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio", "tunnel-audio",
	)
	audioSender, audioErr := pc.AddTrack(audioTrack)
	videoSender, videoErr := pc.AddTrack(trackVideo)
	
	logFn("%s: AddTrack audio: sender=%v err=%v", prefix, audioSender != nil, audioErr)
	logFn("%s: AddTrack video: sender=%v err=%v", prefix, videoSender != nil, videoErr)
	
	tracks := []*webrtc.TrackLocalStaticSample{trackVideo}

	if dualTrack {
		trackScreen, _ := webrtc.NewTrackLocalStaticSample(
			webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8},
			"video", "tunnel-screen",
		)
		screenSender, screenErr := pc.AddTrack(trackScreen)
		logFn("%s: AddTrack screen: sender=%v err=%v", prefix, screenSender != nil, screenErr)
		tracks = append(tracks, trackScreen)
	}

	logFn("%s: senders count: %d", prefix, len(pc.GetSenders()))
	return tracks
}

func ParseSDPType(t string) webrtc.SDPType {
	if t == "offer" {
		return webrtc.SDPTypeOffer
	}
	return webrtc.SDPTypeAnswer
}

func ReadTrack(track *webrtc.TrackRemote, handler func([]byte), logFn func(string, ...any), prefix string) {
	if track.Codec().MimeType != webrtc.MimeTypeVP8 {
		buf := make([]byte, common.UDPBufSize)
		for {
			if _, _, err := track.Read(buf); err != nil {
				return
			}
		}
	}

	var vp8Pkt codecs.VP8Packet
	var frameBuf []byte
	var lastSeq uint16
	var haveLastSeq bool
	frameValid := false
	recvCount := 0
	buf := make([]byte, common.RTPBufSize)
	for {
		n, _, err := track.Read(buf)
		if err != nil {
			return
		}
		pkt := &rtp.Packet{}
		if pkt.Unmarshal(buf[:n]) != nil {
			continue
		}
		if haveLastSeq && pkt.SequenceNumber != lastSeq+1 {
			frameValid = false
			frameBuf = frameBuf[:0]
		}
		lastSeq = pkt.SequenceNumber
		haveLastSeq = true

		vp8Payload, err := vp8Pkt.Unmarshal(pkt.Payload)
		if err != nil {
			frameValid = false
			frameBuf = frameBuf[:0]
			continue
		}
		if vp8Pkt.S == 1 {
			frameBuf = frameBuf[:0]
			frameValid = true
		}
		if !frameValid {
			continue
		}
		frameBuf = append(frameBuf, vp8Payload...)
		if !pkt.Marker {
			continue
		}
		recvCount++
		if recvCount <= 3 || recvCount%200 == 0 {
			logFn("%s: recv vp8 frame #%d %d bytes", prefix, recvCount, len(frameBuf))
		}
		if handler != nil {
			frame := make([]byte, len(frameBuf))
			copy(frame, frameBuf)
			handler(frame)
		}
		frameBuf = frameBuf[:0]
		frameValid = false
	}
}
