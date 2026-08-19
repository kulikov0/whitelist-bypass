package main

import "github.com/kulikov0/headlessclient/webrtc"

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
