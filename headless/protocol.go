package main

import "github.com/pion/webrtc/v4"

// Tunnel frame protocol constants shared between DC and video relay paths.
// Frame format: connID(4) + msgType(1) + payload
const (
	msgConnect    byte = 0x01
	msgConnectOK  byte = 0x02
	msgConnectErr byte = 0x03
	msgData       byte = 0x04
	msgClose      byte = 0x05
	msgUDP        byte = 0x06
	msgUDPReply   byte = 0x07
)

// Relay is the common interface for DCRelay and VideoRelay.
type Relay interface {
	Init(iceServers []webrtc.ICEServer) error
	CreateOffer() (webrtc.SessionDescription, error)
	CreateAnswer() (webrtc.SessionDescription, error)
	SetRemoteDescription(sdpType webrtc.SDPType, sdp string) error
	AddICECandidate(candidate webrtc.ICECandidateInit) error
	OnICECandidate(fn func(*webrtc.ICECandidate))
	OnConnectionStateChange(fn func(webrtc.PeerConnectionState))
	Close()
}
