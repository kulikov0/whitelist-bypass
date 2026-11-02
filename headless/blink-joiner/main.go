package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"syscall"

	"whitelist-bypass/relay/blink"
	"whitelist-bypass/relay/common"
	"whitelist-bypass/relay/tunnel"
	"whitelist-bypass/relay/tunnel/rtc"
)

func main() {
	common.MaybePrintVersion()
	roomFlag := flag.String("room", "", "Blink chat id, blink://<chat id> printed by the creator or group invite link (required)")
	cookiesPath := flag.String("cookies", "", "path to the joiner's own Blink login file, e.g. cookies-blink-joiner.json")
	loginFlag := flag.Bool("login", false, "log in through the Blink app, save the tokens to --cookies and exit")
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
	debugFlag := flag.Bool("debug", false, "verbose debug logging")
	flag.Parse()
	common.Debug = *debugFlag

	if *cookiesPath == "" {
		log.Fatal("--cookies is required")
	}
	if *loginFlag {
		if err := blink.LoginToFile(*cookiesPath, nil, nil, common.EmitStatus, log.Printf); err != nil {
			log.Fatalf("[login] %v", err)
		}
		return
	}
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

	credentials, err := blink.LoadCredentials(*cookiesPath, nil, log.Printf)
	if err != nil {
		log.Fatalf("[auth] %v", err)
	}
	chatID, err := credentials.ResolveChatID(*roomFlag)
	if err != nil {
		log.Fatalf("[config] --room: %v", err)
	}
	connection, err := credentials.ConnectCall(chatID)
	if err != nil {
		log.Fatalf("[call] %v", err)
	}
	log.Printf("[call] chat=%d call=%s created=%v endpoint=%s mode=%s", chatID, connection.CallID, connection.Created, connection.APIEndpoint, *tunnelMode)
	if connection.Created {
		log.Printf("[call] the chat had no active call, this joiner started a new one and the creator is not in it")
	}

	keySource := strconv.FormatInt(chatID, 10)
	obfuscator, err := tunnel.NewTunnelObfuscator(tunnel.DeriveSecretFromJoinLink(keySource))
	if err != nil {
		log.Fatalf("[obf] init failed: %v", err)
	}
	log.Printf("[obf] localEpoch=0x%08x", obfuscator.LocalEpoch())

	session := blink.NewSession(blink.SessionConfig{
		RoomToken:   connection.Token,
		ServerURL:   connection.APIEndpoint,
		TunnelMode:  *tunnelMode,
		Obfuscator:  obfuscator,
		LogFn:       log.Printf,
		VP8FPS:      *vp8FPS,
		VP8Batch:    *vp8Batch,
		ScreenShare: *dualTrack,
		IsJoiner:    true,
		Reliable:    *reliable,
	})
	session.OnConnected = func(dataTunnel tunnel.DataTunnel) {
		readBuf := common.VP8BufSize
		switch dataTunnel.(type) {
		case *rtc.DCTunnel, *rtc.MultiTrackKCPTunnel:
			readBuf = common.DCBufSize
		}
		bridge := tunnel.NewRelayBridgeWithAuth(dataTunnel, "joiner", readBuf, log.Printf, *socksUser, *socksPass)
		bridge.SetOnConfigAck(session.MarkConfigAcked)
		bridge.MarkReady()
		socksAddress := fmt.Sprintf("%s:%d", *socksHost, *socksPort)
		go func() {
			if err := bridge.ListenSOCKS(socksAddress); err != nil {
				log.Printf("socks listen: %v", err)
			}
		}()
		fmt.Printf("\n  TUNNEL CONNECTED mode=%s\n  socks5 -> %s\n\n", *tunnelMode, socksAddress)
	}

	if err := session.Start(); err != nil {
		log.Fatalf("[session] %v", err)
	}

	shutdownSignals := make(chan os.Signal, 1)
	signal.Notify(shutdownSignals, os.Interrupt, syscall.SIGTERM)
	<-shutdownSignals
	log.Printf("[main] shutting down")
	session.Close()
}
