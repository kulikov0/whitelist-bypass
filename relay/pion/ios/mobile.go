package ios

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"

	"whitelist-bypass/relay/common"
	"whitelist-bypass/relay/tunnel"
)

type HeadlessCallback interface {
	OnLog(msg string)
	OnStatus(status string)
	ResolveHost(hostname string) string
	SaveCache(key string, value string)
	LoadCache(key string) string
	ClearCache(key string)
}

type joinerHandle interface {
	Close()
}

var activeHeadless struct {
	sync.Mutex
	joiner   joinerHandle
	callback HeadlessCallback
	socksLn  net.Listener
	stopped  bool
	platform string
}

func makeOnConnected(socksPort int, socksUser, socksPass string, logFn func(string, ...any), callback HeadlessCallback) func(tunnel.DataTunnel) {
	return func(tun tunnel.DataTunnel) {
		activeHeadless.Lock()
		if activeHeadless.stopped {
			activeHeadless.Unlock()
			return
		}
		activeHeadless.Unlock()

		readBuf := common.VP8BufSize
		if _, ok := tun.(*tunnel.DCTunnel); ok {
			readBuf = common.DCBufSize
		}
		bridge := tunnel.NewRelayBridgeWithAuth(tun, "joiner", readBuf, logFn, socksUser, socksPass)
		bridge.MarkReady()

		socksAddr := fmt.Sprintf("127.0.0.1:%d", socksPort)
		logFn("ios: SOCKS5 proxy starting on %s", socksAddr)
		go func() {
			if err := bridge.ListenSOCKS(socksAddr); err != nil {
				logFn("ios: SOCKS5 listen error: %v", err)
				callback.OnStatus("ERROR:socks listen: " + err.Error())
			}
		}()
	}
}

func makeHelpers(callback HeadlessCallback) (func(string, ...any), ResolveFunc, StatusFunc) {
	logFn := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		callback.OnLog(msg)
	}
	resolveFn := func(hostname string) (string, error) {
		result := callback.ResolveHost(hostname)
		if result == "" {
			return "", fmt.Errorf("empty resolve for %s", hostname)
		}
		return result, nil
	}
	statusFn := func(status string) {
		callback.OnStatus(status)
	}
	return logFn, resolveFn, statusFn
}

func init() {
	common.MaskingEnabled = true
}

func StartTelemostHeadless(socksPort int, socksUser, socksPass string, callback HeadlessCallback) {
	StopHeadless()

	activeHeadless.Lock()
	activeHeadless.callback = callback
	activeHeadless.stopped = false
	activeHeadless.platform = "telemost"
	activeHeadless.Unlock()

	logFn, resolveFn, statusFn := makeHelpers(callback)
	joiner := NewTelemostHeadlessJoiner(logFn, resolveFn, statusFn)
	joiner.OnConnected = makeOnConnected(socksPort, socksUser, socksPass, logFn, callback)

	activeHeadless.Lock()
	activeHeadless.joiner = joiner
	activeHeadless.Unlock()

	callback.OnStatus(common.StatusReady)
}

func StartVKHeadless(socksPort int, socksUser, socksPass string, joinLink, displayName, tunnelMode string, callback HeadlessCallback) {
	StopHeadless()

	activeHeadless.Lock()
	activeHeadless.callback = callback
	activeHeadless.stopped = false
	activeHeadless.platform = "vk"
	activeHeadless.Unlock()

	logFn, resolveFn, statusFn := makeHelpers(callback)
	joiner := NewVKHeadlessJoiner(logFn, resolveFn, statusFn)
	joiner.OnConnected = makeOnConnected(socksPort, socksUser, socksPass, logFn, callback)

	activeHeadless.Lock()
	activeHeadless.joiner = joiner
	activeHeadless.Unlock()

	go func() {
		authJSON, err := RunVKAuth(joinLink, displayName, logFn, statusFn, callback)
		if err != nil {
			logFn("vk-auth: failed: %v", err)
			callback.OnStatus("ERROR:" + err.Error())
			return
		}
		var params map[string]interface{}
		if json.Unmarshal([]byte(authJSON), &params) == nil {
			params["tunnelMode"] = tunnelMode
			if patched, err := json.Marshal(params); err == nil {
				authJSON = string(patched)
			}
		}
		logFn("vk-auth: sending join params to relay (mode=%s)", tunnelMode)
		joiner.RunWithParams(authJSON)
	}()
}

func SendJoinParams(jsonParams string) {
	activeHeadless.Lock()
	joiner := activeHeadless.joiner
	platform := activeHeadless.platform
	activeHeadless.Unlock()

	if joiner == nil {
		return
	}

	switch platform {
	case "telemost":
		if tm, ok := joiner.(*TelemostHeadlessJoiner); ok {
			go tm.RunWithParams(jsonParams)
		}
	case "vk":
		if vk, ok := joiner.(*VKHeadlessJoiner); ok {
			go vk.RunWithParams(jsonParams)
		}
	}
}

func StopHeadless() {
	activeHeadless.Lock()
	activeHeadless.stopped = true
	joiner := activeHeadless.joiner
	socksLn := activeHeadless.socksLn
	activeHeadless.joiner = nil
	activeHeadless.socksLn = nil
	activeHeadless.callback = nil
	activeHeadless.platform = ""
	activeHeadless.Unlock()

	if joiner != nil {
		joiner.Close()
	}
	if socksLn != nil {
		socksLn.Close()
	}
}
