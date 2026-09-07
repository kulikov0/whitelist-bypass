package android

import (
	"log"
	"strings"

	"whitelist-bypass/relay/common"
	joiner "whitelist-bypass/relay/pion/headless-joiner-common"
	"whitelist-bypass/relay/tunnel"
)

type BitrixHeadlessJoiner struct {
	inner       *joiner.BitrixHeadlessJoiner
	OnConnected func(tunnel.DataTunnel)
}

func NewBitrixHeadlessJoiner(logFn func(string, ...any)) *BitrixHeadlessJoiner {
	if logFn == nil {
		logFn = log.Printf
	}
	inner := joiner.NewBitrixHeadlessJoiner(logFn, RequestResolve, StatusEmitter{}, PCConfigurer{})
	wrapper := &BitrixHeadlessJoiner{inner: inner}
	inner.OnConnected = func(tun tunnel.DataTunnel) {
		if wrapper.OnConnected != nil {
			wrapper.OnConnected(tun)
		}
	}
	return wrapper
}

func (j *BitrixHeadlessJoiner) MarkConfigAcked() { j.inner.MarkConfigAcked() }

func (j *BitrixHeadlessJoiner) Run() {
	j.inner.Status.EmitStatus(common.StatusReady)
	for {
		line, err := ReadStdinLine()
		if err != nil {
			log.Printf("bitrix-joiner: stdin closed: %v", err)
			return
		}
		if strings.HasPrefix(line, "JOIN:") {
			j.inner.RunWithParams(strings.TrimPrefix(line, "JOIN:"))
			return
		}
	}
}
