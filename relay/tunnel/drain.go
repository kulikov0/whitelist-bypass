package tunnel

import (
	"github.com/pion/webrtc/v4"
	"whitelist-bypass/relay/common"
)

func DrainSenderRTCP(sender *webrtc.RTPSender) {
	if sender == nil {
		return
	}
	buf := make([]byte, 1500)
	for {
		if _, _, err := sender.Read(buf); err != nil {
			return
		}
	}
}

func DrainTrack(track *webrtc.TrackRemote) {
	if track == nil {
		return
	}
	buf := make([]byte, common.UDPBufSize)
	for {
		if _, _, err := track.Read(buf); err != nil {
			return
		}
	}
}
