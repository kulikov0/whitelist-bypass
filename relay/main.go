package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"

	"whitelist-bypass/relay/androidbind"
	"whitelist-bypass/relay/common"
	"whitelist-bypass/relay/pion"
	"whitelist-bypass/relay/pion/android"
	"whitelist-bypass/relay/tunnel"
)

type stdLogger struct{}

func (s stdLogger) OnLog(msg string) {
	log.Print(msg)
}

func main() {
	mode := flag.String("mode", "", "joiner or creator")
	wsPort := flag.Int("ws-port", 9000, "WebSocket port for browser connection")
	socksHost := flag.String("socks-host", common.SocksLocalhostIP, "SOCKS5 listen address (joiner mode; use 0.0.0.0 to expose on LAN)")
	socksPort := flag.Int("socks-port", 1080, "SOCKS5 proxy port (joiner mode only)")
	socksUser := flag.String("socks-user", "", "SOCKS5 proxy username")
	socksPass := flag.String("socks-pass", "", "SOCKS5 proxy password")
	upstreamSocks := flag.String("upstream-socks", "", "creator mode: route tunneled egress through this SOCKS5 proxy (host:port), e.g. a local VPN client")
	upstreamUser := flag.String("upstream-user", "", "upstream SOCKS5 username")
	upstreamPass := flag.String("upstream-pass", "", "upstream SOCKS5 password")
	flag.String("local-ip", "", "local IP address (unused, passed via hook)")
	flag.Parse()

	if *mode == "" {
		fmt.Fprintf(os.Stderr, "Usage: relay --mode dc-joiner|dc-creator|vk-video-joiner|vk-video-creator|telemost-video-joiner|telemost-video-creator\n")
		os.Exit(1)
	}

	cb := stdLogger{}

	type signalingClient interface {
		HandleSignaling(http.ResponseWriter, *http.Request)
	}

	startVideo := func(name string, client signalingClient, onConnected func(tunnel.DataTunnel)) {
		mux := http.NewServeMux()
		mux.HandleFunc("/signaling", client.HandleSignaling)
		addr := fmt.Sprintf("127.0.0.1:%d", *wsPort)
		log.Printf("%s: signaling on %s", name, addr)
		log.Fatal(http.ListenAndServe(addr, mux))
	}

	startJoinerBridge := func(tun tunnel.DataTunnel, readBuf int) {
		rb := tunnel.NewRelayBridgeWithAuth(tun, "joiner", readBuf, log.Printf, *socksUser, *socksPass)
		rb.MarkReady()
		go rb.ListenSOCKS(fmt.Sprintf("%s:%d", *socksHost, *socksPort))
	}

	joinerCallback := func(tun tunnel.DataTunnel) {
		startJoinerBridge(tun, common.VP8BufSize)
	}

	creatorCallback := func(tun tunnel.DataTunnel) {
		rb := tunnel.NewRelayBridge(tun, "creator", common.VP8BufSize, log.Printf)
		rb.SetUpstreamSocks(*upstreamSocks, *upstreamUser, *upstreamPass)
	}

	newPersistentJoinerBridge := func(onConfigAck func()) func(tunnel.DataTunnel) {
		var (
			bridge   *tunnel.RelayBridge
			bridgeMu sync.Mutex
		)
		return func(tun tunnel.DataTunnel) {
			readBuf := common.VP8BufSize
			switch tun.(type) {
			case *tunnel.DCTunnel, *tunnel.MultiTrackKCPTunnel:
				readBuf = common.DCBufSize
			}
			bridgeMu.Lock()
			defer bridgeMu.Unlock()
			if bridge == nil {
				bridge = tunnel.NewRelayBridgeWithAuth(tun, "joiner", readBuf, log.Printf, *socksUser, *socksPass)
				if onConfigAck != nil {
					bridge.SetOnConfigAck(onConfigAck)
				}
				bridge.SetPersistentListener(true)
				bridge.MarkReady()
				addr := fmt.Sprintf("%s:%d", *socksHost, *socksPort)
				go func() {
					if err := bridge.ListenSOCKS(addr); err != nil {
						log.Printf("relay: SOCKS listen failed: %v", err)
					}
				}()
				return
			}
			bridge.SwapTunnel(tun)
			if onConfigAck != nil {
				bridge.SetOnConfigAck(onConfigAck)
			}
			log.Printf("relay: tunnel swapped after reconnect")
		}
	}

	switch *mode {
	case "dc-joiner":
		log.Fatal(androidbind.StartJoiner(*wsPort, *socksPort, *socksHost, *socksUser, *socksPass, cb))
	case "dc-creator":
		log.Fatal(startDCCreator(*wsPort))
	case "vk-video-joiner":
		c := pion.NewVKClient(log.Printf)
		c.OnConnected = joinerCallback
		startVideo(*mode, c, joinerCallback)
	case "vk-headless-joiner":
		c := android.NewVKHeadlessJoiner(log.Printf)
		c.OnConnected = newPersistentJoinerBridge(nil)
		c.Run()
	case "vk-video-creator":
		c := pion.NewVKClient(log.Printf)
		c.OnConnected = creatorCallback
		startVideo(*mode, c, creatorCallback)
	case "telemost-headless-joiner":
		c := android.NewTelemostHeadlessJoiner(log.Printf)
		c.OnConnected = newPersistentJoinerBridge(nil)
		c.Run()
	case "telemost-video-joiner":
		c := pion.NewTelemostClient(log.Printf)
		c.OnConnected = joinerCallback
		startVideo(*mode, c, joinerCallback)
	case "telemost-video-creator":
		c := pion.NewTelemostClient(log.Printf)
		c.OnConnected = creatorCallback
		startVideo(*mode, c, creatorCallback)
	case "wbstream-headless-joiner":
		c := android.NewWBStreamHeadlessJoiner(log.Printf)
		c.OnConnected = newPersistentJoinerBridge(c.MarkConfigAcked)
		c.Run()
	case "dion-headless-joiner":
		c := android.NewDionHeadlessJoiner(log.Printf)
		c.OnConnected = newPersistentJoinerBridge(nil)
		c.Run()
	default:
		fmt.Fprintf(os.Stderr, "Unknown mode: %s\n", *mode)
		os.Exit(1)
	}
}
