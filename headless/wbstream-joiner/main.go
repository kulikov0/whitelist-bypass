package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"whitelist-bypass/relay/common"
	"whitelist-bypass/relay/tunnel"
	"whitelist-bypass/relay/wbstream"
)

func main() {
	common.MaybePrintVersion()
	roomFlag := flag.String("room", "", "WB Stream room id, wbstream://<id>, or https://stream.wb.ru/room/<id> (required)")
	displayName := flag.String("name", "Joiner", "display name in the room")
	socksHost := flag.String("socks-host", common.SocksLocalhostIP, "SOCKS5 listen address (use 0.0.0.0 to expose on LAN)")
	socksPort := flag.Int("socks-port", 1080, "SOCKS5 listen port")
	socksUser := flag.String("socks-user", "", "SOCKS5 username (optional)")
	socksPass := flag.String("socks-pass", "", "SOCKS5 password (optional)")
	resources := flag.String("resources", "default", "resource mode: moderate, default, unlimited")
	tunnelMode := flag.String("tunnel-mode", "video", "tunnel mode: video, dc")
	vp8FPS := flag.Int("vp8-fps", 24, "VP8 frame rate (video mode only)")
	vp8Batch := flag.Int("vp8-batch", 30, "VP8 batch multiplier (video mode only)")
	dualTrack := flag.Bool("dual-track", false, "publish a second VP8 track as ScreenShare and shard outbound across both (video mode only)")
	reliable := flag.Bool("reliable", false, "wrap the video tunnel with KCP reliability (video mode only)")
	flag.Parse()

	if *roomFlag == "" {
		log.Fatal("--room is required")
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

	roomID := wbstream.ParseRoomID(*roomFlag)
	id, roomToken, _, serverURL, err := wbstream.AuthAndGetToken(nil, roomID, *displayName)
	if err != nil {
		log.Fatalf("[auth] %v", err)
	}
	log.Printf("[auth] room=%s server=%s mode=%s", id, serverURL, *tunnelMode)

	obf, err := tunnel.NewTunnelObfuscator(tunnel.DeriveSecretFromJoinLink(id))
	if err != nil {
		log.Fatalf("[obf] init failed: %v", err)
	}
	log.Printf("[obf] localEpoch=0x%08x", obf.LocalEpoch())

	sess := wbstream.NewSession(wbstream.SessionConfig{
		RoomToken:   roomToken,
		ServerURL:   serverURL,
		DisplayName: *displayName,
		TunnelMode:  *tunnelMode,
		Obfuscator:  obf,
		LogFn:       log.Printf,
		VP8FPS:      *vp8FPS,
		VP8Batch:    *vp8Batch,
		ScreenShare: *dualTrack,
		IsJoiner:    true,
		Reliable:    *reliable,
	})
	sess.OnConnected = func(tun tunnel.DataTunnel) {
		readBuf := common.VP8BufSize
		switch tun.(type) {
		case *tunnel.DCTunnel, *tunnel.MultiTrackKCPTunnel:
			readBuf = common.DCBufSize
		}
		bridge := tunnel.NewRelayBridgeWithAuth(tun, "joiner", readBuf, log.Printf, *socksUser, *socksPass)
		bridge.SetOnConfigAck(sess.MarkConfigAcked)
		bridge.MarkReady()
		addr := fmt.Sprintf("%s:%d", *socksHost, *socksPort)
		go func() {
			if err := bridge.ListenSOCKS(addr); err != nil {
				log.Printf("socks listen: %v", err)
			}
		}()
		fmt.Printf("\n  TUNNEL CONNECTED mode=%s\n  socks5 -> %s\n\n", *tunnelMode, addr)
	}

	if err := sess.Start(); err != nil {
		log.Fatalf("[session] %v", err)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	log.Printf("[main] shutting down")
	sess.Close()
}
