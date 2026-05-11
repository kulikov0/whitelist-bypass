// Command windows-joiner is the engine behind the Windows joiner GUI.
// It runs a headless WB Stream or Telemost joiner exactly like the
// Linux CLIs in headless/{wbstream,telemost}-joiner, but additionally
// brings up a wintun adapter so every IP packet on the host is steered
// through the resulting SOCKS5 proxy.
//
// It must run with administrator rights (the embedded manifest asks
// for them); creating wintun adapters and editing the route table
// both require elevation.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"

	joinerCommon "whitelist-bypass/relay/pion/headless-joiner-common"
	"whitelist-bypass/relay/common"
	"whitelist-bypass/relay/pion"
	"whitelist-bypass/relay/tunnel"
	"whitelist-bypass/relay/wbstream"
	"whitelist-bypass/relay/wintunnel"
)

type statusEmitter struct{}

func (statusEmitter) EmitStatus(status string) {
	log.Printf("[status] %s", status)
	// CAPTCHA:url is fired by the VK auth path when an interactive
	// captcha is required. The Electron wrapper watches stdout for
	// this exact prefix and opens a BrowserWindow at the URL.
	if strings.HasPrefix(status, "CAPTCHA:") {
		fmt.Printf("STATUS:%s\n", status)
	}
}
func (statusEmitter) EmitStatusError(msg string) { log.Printf("[status] ERROR: %s", msg) }

type fileCacheStore struct{ dir string }

func newFileCacheStore() *fileCacheStore {
	dir, _ := os.UserCacheDir()
	if dir == "" {
		dir = os.TempDir()
	}
	cacheDir := filepath.Join(dir, "whitelist-bypass")
	os.MkdirAll(cacheDir, 0755)
	return &fileCacheStore{dir: cacheDir}
}

func (c *fileCacheStore) Save(key, value string) {
	os.WriteFile(filepath.Join(c.dir, key), []byte(value), 0644)
}

func (c *fileCacheStore) Load(key string) string {
	data, err := os.ReadFile(filepath.Join(c.dir, key))
	if err != nil {
		return ""
	}
	return string(data)
}

const (
	tunAdapter = "WhitelistBypass"
	tunIP      = "10.99.0.2"
	tunMask    = "255.255.255.0"
	tunPeer    = "10.99.0.1"
	tunMTU     = 1500
)

func main() {
	platform := flag.String("platform", "", "wbstream | telemost | vk (required)")
	link := flag.String("link", "", "WB Stream room link, Telemost join URI, or VK call link (required)")
	displayName := flag.String("name", "Joiner", "display name in the room")
	socksPort := flag.Int("socks-port", 1080, "local SOCKS5 port")
	socksUser := flag.String("socks-user", "", "optional SOCKS5 username")
	socksPass := flag.String("socks-pass", "", "optional SOCKS5 password")
	resources := flag.String("resources", "default", "moderate | default | unlimited")
	tunnelMode := flag.String("tunnel-mode", "video", "tunnel mode for WB Stream: video | dc")
	vp8FPS := flag.Int("vp8-fps", 24, "VP8 frame rate")
	vp8Batch := flag.Int("vp8-batch", 30, "VP8 batch multiplier")
	dns := flag.String("dns", "1.1.1.1,8.8.8.8", "comma-separated DNS servers for the tunnel adapter")
	noTun := flag.Bool("no-tun", false, "expose SOCKS5 only, do not bring up the wintun adapter")
	flag.Parse()

	if *platform == "" || *link == "" {
		log.Fatal("--platform and --link are required")
	}

	switch *resources {
	case "moderate":
		debug.SetMemoryLimit(64 << 20)
	case "default":
		debug.SetMemoryLimit(128 << 20)
	case "unlimited":
		debug.SetMemoryLimit(256 << 20)
	default:
		log.Fatalf("[config] unknown resources mode: %s", *resources)
	}
	common.MaskingEnabled = true

	// One wintunnel.Tunnel covers both platforms. Created up-front so
	// signaling-host bypass routes can be installed before any platform
	// code touches the network.
	var tun *wintunnel.Tunnel
	if !*noTun {
		cfg := wintunnel.Config{
			AdapterName: tunAdapter,
			TunnelIP:    tunIP,
			TunnelMask:  tunMask,
			TunnelPeer:  tunPeer,
			MTU:         tunMTU,
			DNSServers:  splitCSV(*dns),
			SocksHost:   "127.0.0.1",
			SocksPort:   *socksPort,
			SocksUser:   *socksUser,
			SocksPass:   *socksPass,
			LogFn:       log.Printf,
		}
		var err error
		tun, err = wintunnel.New(cfg)
		if err != nil {
			log.Fatalf("[wintunnel] init: %v", err)
		}
	}

	// Add bypass routes for the signaling hosts before any traffic
	// from the joiner reaches them. These are needed even before
	// engine.Start, because the joiner opens its WebSocket as soon
	// as we call Start() below.
	bypassHosts := signalingHosts(*platform, *link)
	preResolved := map[string][]net.IP{}
	for _, h := range bypassHosts {
		ips, err := net.LookupIP(h)
		if err != nil {
			log.Printf("[bypass] resolve %s: %v (will rely on candidate hook)", h, err)
			continue
		}
		preResolved[h] = ips
		log.Printf("[bypass] %s -> %v (pre-tun)", h, ips)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	tunReady := make(chan struct{})
	var tunOnce sync.Once
	bringUpTun := func() {
		tunOnce.Do(func() {
			if tun == nil {
				close(tunReady)
				return
			}
			if err := tun.Start(); err != nil {
				log.Fatalf("[wintunnel] start: %v", err)
			}
			// Now that the tunnel knows the original gateway,
			// pin /32 bypass routes for the signaling hosts.
			for host, ips := range preResolved {
				for _, ip := range ips {
					if err := tun.AddBypassIP(ip); err != nil {
						log.Printf("[bypass] %s ip %s: %v", host, ip, err)
					}
				}
			}
			fmt.Printf("\n  TUNNEL ACTIVE on adapter %q (DNS=%s)\n  all traffic now egresses via %s\n\n",
				tunAdapter, *dns, *platform)
			close(tunReady)
		})
	}

	addCandidate := func(target int, candidateOrSDP string) {
		if tun == nil {
			return
		}
		if err := tun.AddBypassFromCandidate(candidateOrSDP); err != nil {
			log.Printf("[bypass] candidate: %v", err)
		}
		// SDP path: scan every candidate line in the blob
		if strings.Contains(candidateOrSDP, "a=candidate:") {
			for _, line := range strings.Split(candidateOrSDP, "\n") {
				line = strings.TrimRight(line, "\r")
				if strings.HasPrefix(line, "a=candidate:") {
					if err := tun.AddBypassFromCandidate(line); err != nil {
						log.Printf("[bypass] sdp candidate: %v", err)
					}
				}
			}
		}
	}

	onConnected := func(t tunnel.DataTunnel) {
		readBuf := common.VP8BufSize
		if _, ok := t.(*tunnel.DCTunnel); ok {
			readBuf = common.DCBufSize
		}
		bridge := tunnel.NewRelayBridgeWithAuth(t, "joiner", readBuf, log.Printf, *socksUser, *socksPass)
		bridge.MarkReady()
		addr := fmt.Sprintf("127.0.0.1:%d", *socksPort)
		go func() {
			if err := bridge.ListenSOCKS(addr); err != nil {
				log.Printf("[socks] listen: %v", err)
			}
		}()
		log.Printf("[socks] listening on %s", addr)
		// SOCKS5 is up; bring up wintun so the OS starts steering
		// traffic into it. Doing this after the joiner has connected
		// also means we already have remote candidates and bypass
		// routes are in place.
		bringUpTun()
	}

	switch strings.ToLower(*platform) {
	case "wbstream", "wb":
		runWBStream(*link, *displayName, *tunnelMode, *vp8FPS, *vp8Batch,
			onConnected, addCandidate)
	case "telemost", "tm":
		runTelemost(*link, *displayName, *vp8FPS, *vp8Batch,
			onConnected, addCandidate)
	case "vk":
		runVK(*link, *displayName, *tunnelMode, *vp8FPS, *vp8Batch,
			onConnected, addCandidate)
	default:
		log.Fatalf("[config] unknown --platform %q", *platform)
	}

	<-sig
	log.Printf("[main] shutting down")
	if tun != nil {
		tun.Stop()
	}
	// Give in-flight goroutines a beat to drain before the process exits.
	time.Sleep(200 * time.Millisecond)
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func signalingHosts(platform, link string) []string {
	switch strings.ToLower(platform) {
	case "wbstream", "wb":
		return []string{"stream.wb.ru", "wbstream01-el.wb.ru"}
	case "telemost", "tm":
		hosts := []string{"telemost.yandex.ru", "telemost-api.yandex.ru"}
		if u, err := url.Parse(strings.TrimSpace(link)); err == nil && u.Host != "" {
			hosts = append(hosts, u.Host)
		}
		return hosts
	case "vk":
		hosts := []string{"vk.com", "login.vk.com", "api.vk.com", "ok.ru", "cloud-api.yandex.ru"}
		if u, err := url.Parse(strings.TrimSpace(link)); err == nil && u.Host != "" {
			hosts = append(hosts, u.Host)
		}
		return hosts
	}
	return nil
}

func runWBStream(link, name, mode string, fps, batch int,
	onConnected func(tunnel.DataTunnel),
	onCandidate func(int, string),
) {
	id := strings.TrimPrefix(strings.TrimSpace(link), "wbstream://")
	roomID, roomToken, _, err := wbstream.AuthAndGetToken(nil, id, name)
	if err != nil {
		log.Fatalf("[wb] auth: %v", err)
	}
	log.Printf("[wb] room=%s mode=%s", roomID, mode)

	obf, err := tunnel.NewTunnelObfuscator(tunnel.DeriveSecretFromJoinLink(roomID))
	if err != nil {
		log.Fatalf("[wb] obfuscator: %v", err)
	}

	sess := wbstream.NewSession(wbstream.SessionConfig{
		RoomToken:   roomToken,
		DisplayName: name,
		TunnelMode:  mode,
		Obfuscator:  obf,
		LogFn:       log.Printf,
		VP8FPS:      fps,
		VP8Batch:    batch,
	})
	sess.OnConnected = onConnected
	sess.OnRemoteCandidate = onCandidate

	if err := sess.Start(); err != nil {
		log.Fatalf("[wb] session: %v", err)
	}
}

func runTelemost(link, name string, fps, batch int,
	onConnected func(tunnel.DataTunnel),
	onCandidate func(int, string),
) {
	inner := joinerCommon.NewTelemostHeadlessJoiner(
		log.Printf,
		resolveHostname,
		statusEmitter{},
		nil,
		pion.AddTunnelTracks,
		pion.ReadTrack,
	)
	inner.OnConnected = onConnected
	inner.OnRemoteCandidate = onCandidate

	params, _ := json.Marshal(struct {
		JoinLink    string `json:"joinLink"`
		DisplayName string `json:"displayName"`
		VP8FPS      int    `json:"vp8Fps"`
		VP8Batch    int    `json:"vp8Batch"`
	}{
		JoinLink:    strings.TrimSpace(link),
		DisplayName: name,
		VP8FPS:      fps,
		VP8Batch:    batch,
	})
	go inner.RunWithParams(string(params))
}

func runVK(link, name, mode string, fps, batch int,
	onConnected func(tunnel.DataTunnel),
	onCandidate func(int, string),
) {
	emitter := statusEmitter{}
	statusFn := func(s string) { emitter.EmitStatus(s) }

	authJSON, err := joinerCommon.RunVKAuth(strings.TrimSpace(link), name,
		log.Printf, statusFn, newFileCacheStore(), resolveHostname)
	if err != nil {
		log.Fatalf("[vk] auth: %v", err)
	}

	var authParams map[string]interface{}
	if json.Unmarshal([]byte(authJSON), &authParams) != nil {
		log.Fatalf("[vk] auth response not JSON: %s", authJSON)
	}
	authParams["tunnelMode"] = mode
	authParams["vp8Fps"] = fps
	authParams["vp8Batch"] = batch
	patched, err := json.Marshal(authParams)
	if err != nil {
		log.Fatalf("[vk] auth marshal: %v", err)
	}

	inner := joinerCommon.NewVKHeadlessJoiner(
		log.Printf,
		resolveHostname,
		emitter,
		nil,
		pion.AddTunnelTracks,
		pion.ReadTrack,
	)
	inner.OnConnected = onConnected
	inner.OnRemoteCandidate = onCandidate
	go inner.RunWithParams(string(patched))
}

func resolveHostname(hostname string) (string, error) {
	if ip := net.ParseIP(hostname); ip != nil {
		return hostname, nil
	}
	ips, err := net.LookupIP(hostname)
	if err != nil {
		return "", err
	}
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			return v4.String(), nil
		}
	}
	for _, ip := range ips {
		return ip.String(), nil
	}
	return "", fmt.Errorf("no IPs for %s", hostname)
}
