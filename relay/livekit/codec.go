package livekit

import (
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
)

const (
	EventOther = iota
	EventJoin
	EventAnswer
	EventOffer
	EventTrickle
	EventUpdate
	EventLeave
	EventToken
)

type TrickleEvent struct {
	Candidate webrtc.ICECandidateInit
	Target    int
}

type Event struct {
	Kind         int
	Join         *JoinResponse
	SDP          string
	Trickle      *TrickleEvent
	Participants []ParticipantInfo
	Token        string
	Leave        *LeaveInfo
}

type AddTrackParams struct {
	CID    string
	Name   string
	Type   int
	Source int
	Width  uint32
	Height uint32
}

type Codec interface {
	DialURL(serverURL, token string) (string, error)
	MessageType() int
	Accepts(messageType int) bool
	Decode(data []byte) (Event, error)
	EncodeOffer(sdp string) []byte
	EncodeAnswer(sdp string) []byte
	EncodeTrickle(candidate webrtc.ICECandidateInit, target int) []byte
	EncodeAddTrack(p AddTrackParams) []byte
	EncodePing(timestamp int64) []byte
	EncodeLeave() []byte
}

type ProtoCodec struct{}

func (ProtoCodec) DialURL(serverURL, token string) (string, error) {
	u, err := url.Parse(serverURL)
	if err != nil {
		return "", fmt.Errorf("parse url: %w", err)
	}
	u.Path = "/rtc"
	q := u.Query()
	q.Set("access_token", token)
	q.Set("protocol", ProtocolVersion)
	q.Set("sdk", SDKName)
	q.Set("version", SDKVersion)
	q.Set("auto_subscribe", "1")
	q.Set("adaptive_stream", "true")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (ProtoCodec) MessageType() int { return websocket.BinaryMessage }

func (ProtoCodec) Accepts(messageType int) bool { return messageType == websocket.BinaryMessage }

func (ProtoCodec) Decode(data []byte) (Event, error) {
	sr, err := decSignalResponse(data)
	if err != nil {
		return Event{}, err
	}
	var ev Event
	switch sr.Kind {
	case signalRespJoin:
		if sr.Join == nil {
			return ev, nil
		}
		ev.Kind = EventJoin
		ev.Join = sr.Join
	case signalRespAnswer:
		if sr.SDP == nil {
			return ev, nil
		}
		ev.Kind = EventAnswer
		ev.SDP = sr.SDP.SDP
	case signalRespOffer:
		if sr.SDP == nil {
			return ev, nil
		}
		ev.Kind = EventOffer
		ev.SDP = sr.SDP.SDP
	case signalRespTrickle:
		if sr.Trickle == nil || sr.Trickle.CandidateInit == "" {
			return ev, nil
		}
		var ic webrtc.ICECandidateInit
		if err := json.Unmarshal([]byte(sr.Trickle.CandidateInit), &ic); err != nil {
			return ev, fmt.Errorf("decode trickle candidate: %w", err)
		}
		ev.Kind = EventTrickle
		ev.Trickle = &TrickleEvent{Candidate: ic, Target: sr.Trickle.Target}
	case signalRespRefreshToken:
		if sr.Token == "" {
			return ev, nil
		}
		ev.Kind = EventToken
		ev.Token = sr.Token
	case signalRespLeave:
		ev.Kind = EventLeave
		ev.Leave = sr.Leave
	case signalRespUpdate:
		if len(sr.Participants) == 0 {
			return ev, nil
		}
		ev.Kind = EventUpdate
		ev.Participants = sr.Participants
	}
	return ev, nil
}

func (ProtoCodec) EncodeOffer(sdp string) []byte {
	return encSignalRequestOffer(sessionDescription{Type: "offer", SDP: sdp})
}

func (ProtoCodec) EncodeAnswer(sdp string) []byte {
	return encSignalRequestAnswer(sessionDescription{Type: "answer", SDP: sdp})
}

func (ProtoCodec) EncodeTrickle(candidate webrtc.ICECandidateInit, target int) []byte {
	js, _ := json.Marshal(candidate)
	return encSignalRequestTrickle(trickleMsg{CandidateInit: string(js), Target: target})
}

func (ProtoCodec) EncodeAddTrack(p AddTrackParams) []byte {
	return encSignalRequestAddTrack(p.CID, p.Name, p.Type, p.Source, p.Width, p.Height)
}

func (ProtoCodec) EncodePing(timestamp int64) []byte {
	return encSignalRequestPing(timestamp)
}

func (ProtoCodec) EncodeLeave() []byte { return encSignalRequestLeave() }

const (
	jsonTargetPublisher  = "PUBLISHER"
	jsonTargetSubscriber = "SUBSCRIBER"
)

type jsonSDP struct {
	SDP  string `json:"sdp"`
	Type string `json:"type"`
}

type jsonTrickle struct {
	CandidateInit string `json:"candidateInit"`
	Target        string `json:"target"`
}

type jsonICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username"`
	Credential string   `json:"credential"`
}

type jsonJoin struct {
	RoomID       string          `json:"roomId"`
	PingInterval int32           `json:"pingInterval"`
	ICEServers   []jsonICEServer `json:"iceServers"`
	Local        struct {
		SID    string `json:"sid"`
		Name   string `json:"name"`
		UserID string `json:"userId"`
	} `json:"localParticipant"`
}

type jsonVideoLayer struct {
	Quality string `json:"quality"`
	Width   uint32 `json:"width"`
	Height  uint32 `json:"height"`
}

type jsonAddTrack struct {
	CID    string           `json:"cid"`
	Name   string           `json:"name"`
	Type   string           `json:"type"`
	Source string           `json:"source"`
	Width  uint32           `json:"width"`
	Height uint32           `json:"height"`
	Layers []jsonVideoLayer `json:"layers"`
}

type JSONCodec struct{}

func (JSONCodec) DialURL(serverURL, _ string) (string, error) { return serverURL, nil }

func (JSONCodec) MessageType() int { return websocket.TextMessage }

func (JSONCodec) Accepts(messageType int) bool {
	return messageType == websocket.TextMessage || messageType == websocket.BinaryMessage
}

func (JSONCodec) Decode(data []byte) (Event, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		return Event{}, err
	}
	var ev Event
	for kind, raw := range envelope {
		switch kind {
		case "joinResponse":
			var ji jsonJoin
			if err := json.Unmarshal(raw, &ji); err != nil {
				return ev, fmt.Errorf("joinResponse: %w", err)
			}
			join := JoinResponse{
				RoomName:        ji.RoomID,
				ParticipantSID:  ji.Local.SID,
				ParticipantID:   ji.Local.Name,
				LocalUserID:     ji.Local.UserID,
				PingIntervalSec: ji.PingInterval,
			}
			for _, is := range ji.ICEServers {
				join.ICEServers = append(join.ICEServers, ICEServer{
					URLs:       is.URLs,
					Username:   is.Username,
					Credential: is.Credential,
				})
			}
			ev.Kind = EventJoin
			ev.Join = &join
			return ev, nil
		case "offer", "answer":
			var m jsonSDP
			if err := json.Unmarshal(raw, &m); err != nil {
				return ev, fmt.Errorf("%s: %w", kind, err)
			}
			if kind == "offer" {
				ev.Kind = EventOffer
			} else {
				ev.Kind = EventAnswer
			}
			ev.SDP = m.SDP
			return ev, nil
		case "trickle":
			var m jsonTrickle
			if err := json.Unmarshal(raw, &m); err != nil {
				return ev, fmt.Errorf("trickle: %w", err)
			}
			if m.CandidateInit == "" {
				return ev, nil
			}
			var ic webrtc.ICECandidateInit
			if err := json.Unmarshal([]byte(m.CandidateInit), &ic); err != nil {
				return ev, fmt.Errorf("decode trickle candidate: %w", err)
			}
			target := TargetPublisher
			if m.Target == jsonTargetSubscriber {
				target = TargetSubscriber
			}
			ev.Kind = EventTrickle
			ev.Trickle = &TrickleEvent{Candidate: ic, Target: target}
			return ev, nil
		}
	}
	return ev, nil
}

func (JSONCodec) encode(envelope string, payload any) []byte {
	body, err := json.Marshal(map[string]any{envelope: payload})
	if err != nil {
		return nil
	}
	return body
}

func (c JSONCodec) EncodeOffer(sdp string) []byte {
	return c.encode("offer", jsonSDP{SDP: sdp, Type: "offer"})
}

func (c JSONCodec) EncodeAnswer(sdp string) []byte {
	return c.encode("answer", jsonSDP{SDP: sdp, Type: "answer"})
}

func (c JSONCodec) EncodeTrickle(candidate webrtc.ICECandidateInit, target int) []byte {
	js, err := json.Marshal(candidate)
	if err != nil {
		return nil
	}
	wire := jsonTrickle{CandidateInit: string(js), Target: jsonTargetPublisher}
	if target == TargetSubscriber {
		wire.Target = jsonTargetSubscriber
	}
	return c.encode("trickle", wire)
}

func (c JSONCodec) EncodeAddTrack(p AddTrackParams) []byte {
	kind := "VIDEO"
	if p.Type == TrackTypeAudio {
		kind = "AUDIO"
	}
	source := "CAMERA"
	if p.Source == TrackSourceScreenShare {
		source = "SCREEN_SHARE"
	}
	return c.encode("addTrack", jsonAddTrack{
		CID:    p.CID,
		Name:   p.Name,
		Type:   kind,
		Source: source,
		Width:  p.Width,
		Height: p.Height,
		Layers: []jsonVideoLayer{{Quality: "HIGH", Width: p.Width, Height: p.Height}},
	})
}

func (c JSONCodec) EncodePing(timestamp int64) []byte {
	return c.encode("pingReq", map[string]any{"timestamp": timestamp, "rtt": 0})
}

func (JSONCodec) EncodeLeave() []byte { return nil }
