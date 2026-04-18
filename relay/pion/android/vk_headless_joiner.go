package android

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"

	"whitelist-bypass/relay/common"
	"whitelist-bypass/relay/pion"
	joiner "whitelist-bypass/relay/pion/headless-joiner-common"
	"whitelist-bypass/relay/tunnel"
)

type fileCacheStore struct {
	dir string
}

func newFileCacheStore() *fileCacheStore {
	dir, _ := os.UserCacheDir()
	if dir == "" {
		dir = os.TempDir()
	}
	cacheDir := filepath.Join(dir, "whitelist-bypass")
	os.MkdirAll(cacheDir, 0755)
	return &fileCacheStore{dir: cacheDir}
}

func (c *fileCacheStore) Save(key string, value string) {
	os.WriteFile(filepath.Join(c.dir, key), []byte(value), 0644)
}

func (c *fileCacheStore) Load(key string) string {
	data, err := os.ReadFile(filepath.Join(c.dir, key))
	if err != nil {
		return ""
	}
	return string(data)
}

type VKHeadlessJoiner struct {
	inner       *joiner.VKHeadlessJoiner
	OnConnected func(tunnel.DataTunnel)
}

func NewVKHeadlessJoiner(logFn func(string, ...any)) *VKHeadlessJoiner {
	if logFn == nil {
		logFn = log.Printf
	}
	inner := joiner.NewVKHeadlessJoiner(logFn, RequestResolve, StatusEmitter{}, PCConfigurer{}, pion.AddTunnelTracks, pion.ReadTrack)
	wrapper := &VKHeadlessJoiner{inner: inner}
	inner.OnConnected = func(tun tunnel.DataTunnel) {
		if wrapper.OnConnected != nil {
			wrapper.OnConnected(tun)
		}
	}
	return wrapper
}

func (h *VKHeadlessJoiner) Run() {
	h.inner.Status.EmitStatus(common.StatusReady)
	for {
		line, err := ReadStdinLine()
		if err != nil {
			log.Printf("headless: stdin closed: %v", err)
			return
		}
		if strings.HasPrefix(line, "JOIN:") {
			h.inner.RunWithParams(strings.TrimPrefix(line, "JOIN:"))
			return
		}
		if strings.HasPrefix(line, "AUTH:") {
			h.runWithAuth(strings.TrimPrefix(line, "AUTH:"))
			return
		}
	}
}

func (h *VKHeadlessJoiner) runWithAuth(jsonParams string) {
	var params struct {
		JoinLink    string `json:"joinLink"`
		DisplayName string `json:"displayName"`
		TunnelMode  string `json:"tunnelMode"`
	}
	if err := json.Unmarshal([]byte(jsonParams), &params); err != nil {
		log.Printf("headless: failed to parse auth params: %v", err)
		h.inner.Status.EmitStatusError("bad params: " + err.Error())
		return
	}
	if params.TunnelMode == "" {
		params.TunnelMode = "video"
	}

	statusFn := func(status string) { h.inner.Status.EmitStatus(status) }

	authJSON, err := joiner.RunVKAuth(params.JoinLink, params.DisplayName, log.Printf, statusFn, newFileCacheStore(), RequestResolve)
	if err != nil {
		log.Printf("vk-auth: failed: %v", err)
		h.inner.Status.EmitStatusError(err.Error())
		return
	}

	var authParams map[string]interface{}
	if json.Unmarshal([]byte(authJSON), &authParams) == nil {
		authParams["tunnelMode"] = params.TunnelMode
		if patched, err := json.Marshal(authParams); err == nil {
			authJSON = string(patched)
		}
	}
	log.Printf("vk-auth: sending join params to relay (mode=%s)", params.TunnelMode)
	h.inner.RunWithParams(authJSON)
}
