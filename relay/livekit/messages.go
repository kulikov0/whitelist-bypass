package livekit

import "fmt"

const (
	signalReqOffer      = 1
	signalReqAnswer     = 2
	signalReqTrickle    = 3
	signalReqAddTrack   = 4
	signalReqLeave      = 8
	signalReqPingLegacy = 14
	signalReqPingReq    = 16

	signalRespJoin            = 1
	signalRespAnswer          = 2
	signalRespOffer           = 3
	signalRespTrickle         = 4
	signalRespUpdate          = 5
	signalRespTrackPublished  = 6
	signalRespLeave           = 8
	signalRespRoomUpdate      = 11
	signalRespRefreshToken    = 16
	signalRespPongResp        = 20
	signalRespRequestResponse = 22
	signalRespTrackSubscribed = 23

	sdpFieldType = 1
	sdpFieldSDP  = 2
	sdpFieldID   = 3

	trickleFieldCandidate = 1
	trickleFieldTarget    = 2
	trickleFieldFinal     = 3

	addTrackFieldCID    = 1
	addTrackFieldName   = 2
	addTrackFieldType   = 3
	addTrackFieldWidth  = 4
	addTrackFieldHeight = 5
	addTrackFieldSource = 8
	addTrackFieldLayers = 9

	videoLayerFieldQuality = 1
	videoLayerFieldWidth   = 2
	videoLayerFieldHeight  = 3

	videoQualityHigh = 2

	joinFieldRoom              = 1
	joinFieldParticipant       = 2
	joinFieldOtherParticipants = 3
	joinFieldServerVersion     = 4
	joinFieldICEServers        = 5
	joinFieldSubscriberPrimary = 6
	joinFieldServerRegion      = 9
	joinFieldPingTimeout       = 10
	joinFieldPingInterval      = 11

	iceServerFieldURLs       = 1
	iceServerFieldUsername   = 2
	iceServerFieldCredential = 3

	pingFieldTimestamp = 1
	pingFieldRTT       = 2

	dataPacketFieldKind = 1
	dataPacketFieldUser = 2

	userPacketFieldPayload = 2

	DataPacketKindReliable = 0
	DataPacketKindLossy    = 1

	leaveFieldCanReconnect = 1
	leaveFieldReason       = 2
	leaveFieldAction       = 3

	roomFieldSID  = 1
	roomFieldName = 2

	participantFieldSID      = 1
	participantFieldIdentity = 2
	participantFieldState    = 3
	participantFieldName     = 9

	trackTypeAudio = 0
	trackTypeVideo = 1
	trackTypeData  = 2

	signalTargetPublisher  = 0
	signalTargetSubscriber = 1
)

var disconnectReasonNames = map[int]string{
	0:  "UNKNOWN",
	1:  "CLIENT_INITIATED",
	2:  "DUPLICATE_IDENTITY",
	3:  "SERVER_SHUTDOWN",
	4:  "PARTICIPANT_REMOVED",
	5:  "ROOM_DELETED",
	6:  "STATE_MISMATCH",
	7:  "JOIN_FAILURE",
	8:  "MIGRATION",
	9:  "SIGNAL_CLOSE",
	10: "ROOM_CLOSED",
	11: "USER_UNAVAILABLE",
	12: "USER_REJECTED",
	13: "SIP_TRUNK_FAILURE",
	14: "CONNECTION_TIMEOUT",
	15: "MEDIA_FAILURE",
	16: "AGENT_ERROR",
}

var leaveActionNames = map[int]string{
	0: "DISCONNECT",
	1: "RESUME",
	2: "RECONNECT",
}

func DisconnectReasonName(code int) string {
	if name, ok := disconnectReasonNames[code]; ok {
		return name
	}
	return fmt.Sprintf("CODE_%d", code)
}

func LeaveActionName(code int) string {
	if name, ok := leaveActionNames[code]; ok {
		return name
	}
	return fmt.Sprintf("CODE_%d", code)
}

type LeaveInfo struct {
	Reason int
	Action int
}

func DecodeLeaveRequest(data []byte) LeaveInfo {
	r := pbReader{buf: data}
	var li LeaveInfo
	for !r.eof() {
		field, wire, err := r.tag()
		if err != nil {
			return li
		}
		switch {
		case field == leaveFieldReason && wire == wireVarint:
			v, _ := r.varint()
			li.Reason = int(v)
		case field == leaveFieldAction && wire == wireVarint:
			v, _ := r.varint()
			li.Action = int(v)
		default:
			if err := r.skipWire(wire); err != nil {
				return li
			}
		}
	}
	return li
}

type sessionDescription struct {
	Type string
	SDP  string
	ID   uint32
}

type trickleMsg struct {
	CandidateInit string
	Target        int
	Final         bool
}

type iceServer struct {
	URLs       []string
	Username   string
	Credential string
}

type joinResponse struct {
	RoomSID           string
	RoomName          string
	ParticipantSID    string
	ParticipantID     string
	LocalUserID       string
	ServerVersion     string
	ServerRegion      string
	ICEServers        []iceServer
	SubscriberPrimary bool
	PingTimeoutSec    int32
	PingIntervalSec   int32
}

type signalResponse struct {
	Kind         int
	Join         *joinResponse
	SDP          *sessionDescription
	Trickle      *trickleMsg
	Token        string
	PongTime     int64
	Leave        *LeaveInfo
	Participants []ParticipantInfo
}

func encSessionDescription(sd sessionDescription) []byte {
	w := pbWriter{}
	if sd.Type != "" {
		w.string(sdpFieldType, sd.Type)
	}
	if sd.SDP != "" {
		w.string(sdpFieldSDP, sd.SDP)
	}
	if sd.ID != 0 {
		w.uint32(sdpFieldID, sd.ID)
	}
	return w.buf
}

func encTrickle(m trickleMsg) []byte {
	w := pbWriter{}
	w.string(trickleFieldCandidate, m.CandidateInit)
	w.int32(trickleFieldTarget, int32(m.Target))
	if m.Final {
		w.bool(trickleFieldFinal, true)
	}
	return w.buf
}

const (
	trackSourceCamera      = 1
	trackSourceScreenShare = 3
)

func encVideoLayer(quality, width, height uint32) []byte {
	w := pbWriter{}
	if quality != 0 {
		w.uint32(videoLayerFieldQuality, quality)
	}
	if width != 0 {
		w.uint32(videoLayerFieldWidth, width)
	}
	if height != 0 {
		w.uint32(videoLayerFieldHeight, height)
	}
	return w.buf
}

func encAddTrack(cid, name string, trackType, source int, width, height uint32) []byte {
	w := pbWriter{}
	w.string(addTrackFieldCID, cid)
	w.string(addTrackFieldName, name)
	w.int32(addTrackFieldType, int32(trackType))
	if width != 0 {
		w.uint32(addTrackFieldWidth, width)
	}
	if height != 0 {
		w.uint32(addTrackFieldHeight, height)
	}
	w.int32(addTrackFieldSource, int32(source))
	if trackType == trackTypeVideo {
		w.message(addTrackFieldLayers, encVideoLayer(videoQualityHigh, width, height))
	}
	return w.buf
}

func encPing(timestamp int64) []byte {
	w := pbWriter{}
	w.int64(pingFieldTimestamp, timestamp)
	return w.buf
}

func encUserPacket(payload []byte) []byte {
	w := pbWriter{}
	w.bytes(userPacketFieldPayload, payload)
	return w.buf
}

func EncodeDataPacketUser(payload []byte, kind int) []byte {
	w := pbWriter{}
	if kind != 0 {
		w.int32(dataPacketFieldKind, int32(kind))
	}
	w.message(dataPacketFieldUser, encUserPacket(payload))
	return w.buf
}

func DecodeDataPacketUser(data []byte) ([]byte, bool) {
	r := pbReader{buf: data}
	for !r.eof() {
		field, wire, err := r.tag()
		if err != nil {
			return nil, false
		}
		if field == dataPacketFieldUser && wire == wireBytes {
			inner, err := r.bytes()
			if err != nil {
				return nil, false
			}
			ur := pbReader{buf: inner}
			for !ur.eof() {
				ufield, uwire, uerr := ur.tag()
				if uerr != nil {
					return nil, false
				}
				if ufield == userPacketFieldPayload && uwire == wireBytes {
					payload, perr := ur.bytes()
					if perr != nil {
						return nil, false
					}
					out := make([]byte, len(payload))
					copy(out, payload)
					return out, true
				}
				if err := ur.skipWire(uwire); err != nil {
					return nil, false
				}
			}
			return nil, false
		}
		if err := r.skipWire(wire); err != nil {
			return nil, false
		}
	}
	return nil, false
}

func encSignalRequestOffer(sd sessionDescription) []byte {
	w := pbWriter{}
	w.message(signalReqOffer, encSessionDescription(sd))
	return w.buf
}

func encSignalRequestAnswer(sd sessionDescription) []byte {
	w := pbWriter{}
	w.message(signalReqAnswer, encSessionDescription(sd))
	return w.buf
}

func encSignalRequestTrickle(m trickleMsg) []byte {
	w := pbWriter{}
	w.message(signalReqTrickle, encTrickle(m))
	return w.buf
}

func encSignalRequestAddTrack(cid, name string, trackType, source int, width, height uint32) []byte {
	w := pbWriter{}
	w.message(signalReqAddTrack, encAddTrack(cid, name, trackType, source, width, height))
	return w.buf
}

func encSignalRequestLeave() []byte {
	w := pbWriter{}
	w.message(signalReqLeave, []byte{})
	return w.buf
}

func encSignalRequestPing(timestamp int64) []byte {
	w := pbWriter{}
	w.int64(signalReqPingLegacy, timestamp)
	w.message(signalReqPingReq, encPing(timestamp))
	return w.buf
}

func decSessionDescription(data []byte) (sessionDescription, error) {
	r := pbReader{buf: data}
	var sd sessionDescription
	for !r.eof() {
		field, wire, err := r.tag()
		if err != nil {
			return sd, err
		}
		switch {
		case field == sdpFieldType && wire == wireBytes:
			b, err := r.bytes()
			if err != nil {
				return sd, err
			}
			sd.Type = string(b)
		case field == sdpFieldSDP && wire == wireBytes:
			b, err := r.bytes()
			if err != nil {
				return sd, err
			}
			sd.SDP = string(b)
		case field == sdpFieldID && wire == wireVarint:
			v, err := r.varint()
			if err != nil {
				return sd, err
			}
			sd.ID = uint32(v)
		default:
			if err := r.skipWire(wire); err != nil {
				return sd, err
			}
		}
	}
	return sd, nil
}

func decTrickle(data []byte) (trickleMsg, error) {
	r := pbReader{buf: data}
	var m trickleMsg
	for !r.eof() {
		field, wire, err := r.tag()
		if err != nil {
			return m, err
		}
		switch {
		case field == trickleFieldCandidate && wire == wireBytes:
			b, err := r.bytes()
			if err != nil {
				return m, err
			}
			m.CandidateInit = string(b)
		case field == trickleFieldTarget && wire == wireVarint:
			v, err := r.varint()
			if err != nil {
				return m, err
			}
			m.Target = int(v)
		case field == trickleFieldFinal && wire == wireVarint:
			v, err := r.varint()
			if err != nil {
				return m, err
			}
			m.Final = v != 0
		default:
			if err := r.skipWire(wire); err != nil {
				return m, err
			}
		}
	}
	return m, nil
}

func decICEServer(data []byte) (iceServer, error) {
	r := pbReader{buf: data}
	var s iceServer
	for !r.eof() {
		field, wire, err := r.tag()
		if err != nil {
			return s, err
		}
		switch {
		case field == iceServerFieldURLs && wire == wireBytes:
			b, err := r.bytes()
			if err != nil {
				return s, err
			}
			s.URLs = append(s.URLs, string(b))
		case field == iceServerFieldUsername && wire == wireBytes:
			b, err := r.bytes()
			if err != nil {
				return s, err
			}
			s.Username = string(b)
		case field == iceServerFieldCredential && wire == wireBytes:
			b, err := r.bytes()
			if err != nil {
				return s, err
			}
			s.Credential = string(b)
		default:
			if err := r.skipWire(wire); err != nil {
				return s, err
			}
		}
	}
	return s, nil
}

func decRoom(data []byte) (string, string, error) {
	r := pbReader{buf: data}
	var sid, name string
	for !r.eof() {
		field, wire, err := r.tag()
		if err != nil {
			return "", "", err
		}
		switch {
		case field == roomFieldSID && wire == wireBytes:
			b, err := r.bytes()
			if err != nil {
				return "", "", err
			}
			sid = string(b)
		case field == roomFieldName && wire == wireBytes:
			b, err := r.bytes()
			if err != nil {
				return "", "", err
			}
			name = string(b)
		default:
			if err := r.skipWire(wire); err != nil {
				return "", "", err
			}
		}
	}
	return sid, name, nil
}

func decParticipant(data []byte) (string, string, error) {
	info := DecodeParticipantInfo(data)
	return info.SID, info.Identity, nil
}

type ParticipantInfo struct {
	SID      string
	Identity string
	State    int32
	Name     string
}

const (
	ParticipantStateJoining      int32 = 0
	ParticipantStateJoined       int32 = 1
	ParticipantStateActive       int32 = 2
	ParticipantStateDisconnected int32 = 3
)

func DecodeParticipantInfo(data []byte) ParticipantInfo {
	r := pbReader{buf: data}
	var info ParticipantInfo
	for !r.eof() {
		field, wire, err := r.tag()
		if err != nil {
			return info
		}
		switch {
		case field == participantFieldSID && wire == wireBytes:
			b, err := r.bytes()
			if err != nil {
				return info
			}
			info.SID = string(b)
		case field == participantFieldIdentity && wire == wireBytes:
			b, err := r.bytes()
			if err != nil {
				return info
			}
			info.Identity = string(b)
		case field == participantFieldState && wire == wireVarint:
			v, err := r.varint()
			if err != nil {
				return info
			}
			info.State = int32(v)
		case field == participantFieldName && wire == wireBytes:
			b, err := r.bytes()
			if err != nil {
				return info
			}
			info.Name = string(b)
		default:
			if err := r.skipWire(wire); err != nil {
				return info
			}
		}
	}
	return info
}

func DecodeParticipantUpdate(data []byte) []ParticipantInfo {
	r := pbReader{buf: data}
	var out []ParticipantInfo
	for !r.eof() {
		field, wire, err := r.tag()
		if err != nil {
			return out
		}
		if field == 1 && wire == wireBytes {
			b, err := r.bytes()
			if err != nil {
				return out
			}
			out = append(out, DecodeParticipantInfo(b))
		} else {
			if err := r.skipWire(wire); err != nil {
				return out
			}
		}
	}
	return out
}

func decJoinResponse(data []byte) (joinResponse, error) {
	r := pbReader{buf: data}
	var jr joinResponse
	for !r.eof() {
		field, wire, err := r.tag()
		if err != nil {
			return jr, err
		}
		switch {
		case field == joinFieldRoom && wire == wireBytes:
			b, err := r.bytes()
			if err != nil {
				return jr, err
			}
			sid, name, _ := decRoom(b)
			jr.RoomSID = sid
			jr.RoomName = name
		case field == joinFieldParticipant && wire == wireBytes:
			b, err := r.bytes()
			if err != nil {
				return jr, err
			}
			sid, identity, _ := decParticipant(b)
			jr.ParticipantSID = sid
			jr.ParticipantID = identity
		case field == joinFieldServerVersion && wire == wireBytes:
			b, err := r.bytes()
			if err != nil {
				return jr, err
			}
			jr.ServerVersion = string(b)
		case field == joinFieldICEServers && wire == wireBytes:
			b, err := r.bytes()
			if err != nil {
				return jr, err
			}
			s, _ := decICEServer(b)
			jr.ICEServers = append(jr.ICEServers, s)
		case field == joinFieldSubscriberPrimary && wire == wireVarint:
			v, err := r.varint()
			if err != nil {
				return jr, err
			}
			jr.SubscriberPrimary = v != 0
		case field == joinFieldServerRegion && wire == wireBytes:
			b, err := r.bytes()
			if err != nil {
				return jr, err
			}
			jr.ServerRegion = string(b)
		case field == joinFieldPingTimeout && wire == wireVarint:
			v, err := r.varint()
			if err != nil {
				return jr, err
			}
			jr.PingTimeoutSec = int32(v)
		case field == joinFieldPingInterval && wire == wireVarint:
			v, err := r.varint()
			if err != nil {
				return jr, err
			}
			jr.PingIntervalSec = int32(v)
		case field == joinFieldOtherParticipants && wire == wireBytes:
			if _, err := r.bytes(); err != nil {
				return jr, err
			}
		default:
			if err := r.skipWire(wire); err != nil {
				return jr, err
			}
		}
	}
	return jr, nil
}

func decSignalResponse(data []byte) (signalResponse, error) {
	r := pbReader{buf: data}
	var sr signalResponse
	for !r.eof() {
		field, wire, err := r.tag()
		if err != nil {
			return sr, err
		}
		if wire != wireBytes {
			if err := r.skipWire(wire); err != nil {
				return sr, err
			}
			continue
		}
		inner, err := r.bytes()
		if err != nil {
			return sr, err
		}
		sr.Kind = int(field)
		switch field {
		case signalRespJoin:
			jr, err := decJoinResponse(inner)
			if err != nil {
				return sr, err
			}
			sr.Join = &jr
		case signalRespAnswer, signalRespOffer:
			sd, err := decSessionDescription(inner)
			if err != nil {
				return sr, err
			}
			sr.SDP = &sd
		case signalRespTrickle:
			tm, err := decTrickle(inner)
			if err != nil {
				return sr, err
			}
			sr.Trickle = &tm
		case signalRespRefreshToken:
			sr.Token = string(inner)
		case signalRespLeave:
			li := DecodeLeaveRequest(inner)
			sr.Leave = &li
		case signalRespUpdate:
			sr.Participants = DecodeParticipantUpdate(inner)
		}
		return sr, nil
	}
	return sr, nil
}
