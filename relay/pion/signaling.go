package pion

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
	"whitelist-bypass/relay/common"
	"whitelist-bypass/relay/tunnel"
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

// Wraps bare IPv6 addresses in brackets for TURN/STUN URLs.
// Tries host:port split first (more common), then full address.
func fixICEURL(url string) string {
	idx := strings.Index(url, ":")
	if idx < 0 {
		return url
	}
	scheme := url[:idx]
	if scheme != "turn" && scheme != "stun" && scheme != "turns" && scheme != "stuns" {
		return url
	}
	rest := url[idx+1:]
	if strings.HasPrefix(rest, "[") {
		return url
	}
	if strings.Count(rest, ":") <= 1 {
		return url
	}
	params := ""
	if qm := strings.Index(rest, "?"); qm >= 0 {
		params = rest[qm:]
		rest = rest[:qm]
	}
	// Try splitting last segment as port first (host:port is more common)
	lastColon := strings.LastIndex(rest, ":")
	if lastColon > 0 {
		host := rest[:lastColon]
		port := rest[lastColon+1:]
		if net.ParseIP(host) != nil {
			return scheme + ":[" + host + "]:" + port + params
		}
	}
	// Fallback: whole thing is an IPv6 address with no port
	if net.ParseIP(rest) != nil {
		return scheme + ":[" + rest + "]" + params
	}
	return url
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
			fixed := fixICEURL(u)
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
	se.SetNet(&AndroidNet{LocalIP: localIP})
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

func AddTunnelTracks(pc *webrtc.PeerConnection, logFn func(string, ...any), prefix string) *webrtc.TrackLocalStaticSample {
	sampleTrack, _ := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8},
		"video", "tunnel-video",
	)
	audioTrack, _ := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio", "tunnel-audio",
	)
	audioSender, audioErr := pc.AddTrack(audioTrack)
	videoSender, videoErr := pc.AddTrack(sampleTrack)
	logFn("%s: AddTrack audio: sender=%v err=%v", prefix, audioSender != nil, audioErr)
	logFn("%s: AddTrack video: sender=%v err=%v", prefix, videoSender != nil, videoErr)
	logFn("%s: senders count: %d", prefix, len(pc.GetSenders()))
	return sampleTrack
}

func ParseSDPType(t string) webrtc.SDPType {
	if t == "offer" {
		return webrtc.SDPTypeOffer
	}
	return webrtc.SDPTypeAnswer
}

func ReadTrack(track *webrtc.TrackRemote, tun *tunnel.VP8DataTunnel, logFn func(string, ...any), prefix string) {
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
	dataCount := 0
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
		vp8Payload, err := vp8Pkt.Unmarshal(pkt.Payload)
		if err != nil {
			continue
		}
		if vp8Pkt.S == 1 {
			frameBuf = frameBuf[:0]
		}
		frameBuf = append(frameBuf, vp8Payload...)
		if pkt.Marker {
			recvCount++
			if recvCount <= 3 || recvCount%25 == 0 {
				if len(frameBuf) > 0 {
					logFn("%s: recv frame #%d %d bytes, first=0x%02x", prefix, recvCount, len(frameBuf), frameBuf[0])
				}
			}
			data := tunnel.ExtractDataFromPayload(frameBuf)
			if data != nil {
				dataCount++
				if dataCount <= 5 || dataCount%100 == 0 {
					logFn("%s: TUNNEL DATA #%d: %d bytes", prefix, dataCount, len(data))
				}
				if tun != nil && tun.OnData != nil {
					tun.OnData(data)
				}
			}
		}
	}
}
