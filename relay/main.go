package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"whitelist-bypass/relay/mobile"
	"whitelist-bypass/relay/pion"
)

type stdLogger struct{}

func (s stdLogger) OnLog(msg string) {
	log.Print(msg)
}

func main() {
	mode := flag.String("mode", "", "joiner or creator")
	wsPort := flag.Int("ws-port", 9000, "WebSocket port for browser connection")
	socksPort := flag.Int("socks-port", 1080, "SOCKS5 proxy port (joiner mode only)")
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

	startVideo := func(name string, client signalingClient, onConnected func(*pion.VP8DataTunnel)) {
		mux := http.NewServeMux()
		mux.HandleFunc("/signaling", client.HandleSignaling)
		addr := fmt.Sprintf("127.0.0.1:%d", *wsPort)
		log.Printf("%s: signaling on %s", name, addr)
		log.Fatal(http.ListenAndServe(addr, mux))
	}

	joinerCallback := func(tunnel *pion.VP8DataTunnel) {
		rb := pion.NewRelayBridge(tunnel, "joiner", log.Printf)
		rb.MarkReady()
		go rb.ListenSOCKS(fmt.Sprintf("127.0.0.1:%d", *socksPort))
	}

	creatorCallback := func(tunnel *pion.VP8DataTunnel) {
		pion.NewRelayBridge(tunnel, "creator", log.Printf)
	}

	switch *mode {
	case "dc-joiner":
		log.Fatal(mobile.StartJoiner(*wsPort, *socksPort, cb))
	case "dc-creator":
		log.Fatal(mobile.StartCreator(*wsPort, cb))
	case "vk-video-joiner":
		c := pion.NewVKClient(log.Printf)
		c.OnConnected = joinerCallback
		startVideo(*mode, c, joinerCallback)
	case "vk-video-creator":
		c := pion.NewVKClient(log.Printf)
		c.OnConnected = creatorCallback
		startVideo(*mode, c, creatorCallback)
	case "telemost-video-joiner":
		c := pion.NewTelemostClient(log.Printf)
		c.OnConnected = joinerCallback
		startVideo(*mode, c, joinerCallback)
	case "telemost-video-creator":
		c := pion.NewTelemostClient(log.Printf)
		c.OnConnected = creatorCallback
		startVideo(*mode, c, creatorCallback)
	default:
		fmt.Fprintf(os.Stderr, "Unknown mode: %s\n", *mode)
		os.Exit(1)
	}
}
