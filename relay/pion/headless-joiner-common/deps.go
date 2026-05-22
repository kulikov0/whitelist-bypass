package joiner

import (
	"github.com/pion/webrtc/v4"
)

type ResolveFunc func(hostname string) (string, error)

type StatusEmitter interface {
	EmitStatus(status string)
	EmitStatusError(msg string)
}

type PeerConnectionConfigurer interface {
	ConfigureSettingEngine(settingEngine *webrtc.SettingEngine)
}

type CacheStore interface {
	Save(key string, value string)
	Load(key string) string
}

type AddTunnelTracksFunc func(pc *webrtc.PeerConnection, logFn func(string, ...any), prefix string, dualTrack bool) []*webrtc.TrackLocalStaticSample
type ReadTrackFunc func(track *webrtc.TrackRemote, handler func([]byte), logFn func(string, ...any), prefix string)
