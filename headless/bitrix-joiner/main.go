package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"whitelist-bypass/relay/common"
	joiner "whitelist-bypass/relay/pion/headless-joiner-common"
	"whitelist-bypass/relay/tunnel"
)

type cliStatusEmitter struct{}

func (cliStatusEmitter) EmitStatus(status string)   { log.Printf("[status] %s", status) }
func (cliStatusEmitter) EmitStatusError(msg string) { log.Printf("[status] ERROR: %s", msg) }

func resolveHostname(hostname string) (string, error) {
	if ip := net.ParseIP(hostname); ip != nil {
		return hostname, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", hostname); err == nil && len(ips) > 0 {
		return ips[0].String(), nil
	}
	if ips, err := net.DefaultResolver.LookupIP(ctx, "ip6", hostname); err == nil && len(ips) > 0 {
		return ips[0].String(), nil
	}
	return "", fmt.Errorf("no IPs for %s", hostname)
}

func main() {
	common.MaybePrintVersion()
	link := flag.String("link", "", "Bitrix conference link https://portal/video/CODE (required)")
	displayName := flag.String("name", "Joiner", "display name in the conference")
	socksHost := flag.String("socks-host", common.SocksLocalhostIP, "SOCKS5 listen address (use 0.0.0.0 to expose on LAN)")
	socksPort := flag.Int("socks-port", 1080, "SOCKS5 listen port")
	socksUser := flag.String("socks-user", "", "SOCKS5 username (optional)")
	socksPass := flag.String("socks-pass", "", "SOCKS5 password (optional)")
	resources := flag.String("resources", "default", "resource mode: moderate, default, unlimited")
	tunnelMode := flag.String("tunnel-mode", "video", "tunnel mode: video, dc")
	vp8FPS := flag.Int("vp8-fps", 24, "VP8 frame rate (video mode only)")
	vp8Batch := flag.Int("vp8-batch", 30, "VP8 batch multiplier (video mode only)")
	dualTrack := flag.Bool("dual-track", false, "request a second screenshare download track for 2x recv throughput (video mode only)")
	reliable := flag.Bool("reliable", false, "wrap the video tunnel with KCP reliability (video mode only)")
	debugFlag := flag.Bool("debug", false, "verbose debug logging")
	flag.Parse()
	common.Debug = *debugFlag

	if *link == "" {
		log.Fatal("--link is required")
	}

	var memLimit int64
	switch *resources {
	case "moderate":
		memLimit = 64 << 20
	case "default":
		memLimit = 128 << 20
	case "unlimited":
		memLimit = 256 << 20
	default:
		log.Fatalf("[config] unknown resources mode: %s", *resources)
	}
	if memLimit > 0 {
		debug.SetMemoryLimit(memLimit)
	}

	inner := joiner.NewBitrixHeadlessJoiner(log.Printf, resolveHostname, cliStatusEmitter{}, nil)
	inner.OnConnected = func(tun tunnel.DataTunnel) {
		readBuf := common.VP8BufSize
		switch tun.(type) {
		case *tunnel.DCTunnel, *tunnel.MultiTrackKCPTunnel:
			readBuf = common.DCBufSize
		}
		bridge := tunnel.NewRelayBridgeWithAuth(tun, "joiner", readBuf, log.Printf, *socksUser, *socksPass)
		bridge.SetOnConfigAck(inner.MarkConfigAcked)
		bridge.MarkReady()
		addr := fmt.Sprintf("%s:%d", *socksHost, *socksPort)
		go func() {
			if err := bridge.ListenSOCKS(addr); err != nil {
				log.Printf("socks listen: %v", err)
			}
		}()
		fmt.Printf("\n  TUNNEL CONNECTED mode=%s\n  socks5 -> %s\n\n", *tunnelMode, addr)
	}

	params, _ := json.Marshal(struct {
		JoinLink    string `json:"joinLink"`
		DisplayName string `json:"displayName"`
		TunnelMode  string `json:"tunnelMode"`
		VP8FPS      int    `json:"vp8Fps"`
		VP8Batch    int    `json:"vp8Batch"`
		Reliable    bool   `json:"reliable"`
		DualTrack   bool   `json:"dualTrack"`
	}{
		JoinLink:    strings.TrimSpace(*link),
		DisplayName: *displayName,
		TunnelMode:  *tunnelMode,
		VP8FPS:      *vp8FPS,
		VP8Batch:    *vp8Batch,
		Reliable:    *reliable,
		DualTrack:   *dualTrack,
	})

	go inner.RunWithParams(string(params))

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	log.Printf("[main] shutting down")
	inner.Close()
}
