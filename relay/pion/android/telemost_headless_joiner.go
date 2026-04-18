package android

import (
	"log"
	"strings"

	"whitelist-bypass/relay/common"
	"whitelist-bypass/relay/pion"
	joiner "whitelist-bypass/relay/pion/headless-joiner-common"
	"whitelist-bypass/relay/tunnel"
)

type TelemostHeadlessJoiner struct {
	inner       *joiner.TelemostHeadlessJoiner
	OnConnected func(tunnel.DataTunnel)
}

func NewTelemostHeadlessJoiner(logFn func(string, ...any)) *TelemostHeadlessJoiner {
	if logFn == nil {
		logFn = log.Printf
	}
	inner := joiner.NewTelemostHeadlessJoiner(logFn, RequestResolve, StatusEmitter{}, PCConfigurer{}, pion.AddTunnelTracks, pion.ReadTrack)
	wrapper := &TelemostHeadlessJoiner{inner: inner}
	inner.OnConnected = func(tun tunnel.DataTunnel) {
		if wrapper.OnConnected != nil {
			wrapper.OnConnected(tun)
		}
	}
	return wrapper
}

func (j *TelemostHeadlessJoiner) Run() {
	j.inner.Status.EmitStatus(common.StatusReady)
	for {
		line, err := ReadStdinLine()
		if err != nil {
			log.Printf("telemost-joiner: stdin closed: %v", err)
			return
		}
		if strings.HasPrefix(line, "JOIN:") {
			j.inner.RunWithParams(strings.TrimPrefix(line, "JOIN:"))
			return
		}
	}
}
