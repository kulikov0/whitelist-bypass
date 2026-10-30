package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"runtime/debug"
	"strconv"
	"time"

	"whitelist-bypass/relay/blink"
	"whitelist-bypass/relay/common"
	"whitelist-bypass/relay/tunnel"
	"whitelist-bypass/relay/tunnel/rtc"
)

func main() {
	common.MaybePrintVersion()
	cookiesPath := flag.String("cookies", "", "path to cookies-blink.json with the Blink login tokens")
	loginFlag := flag.Bool("login", false, "log in through the Blink app, save the tokens to --cookies and exit")
	roomFlag := flag.String("room", "", "Blink chat id, blink://<chat id> or group invite link to rejoin (empty = create new room)")
	resources := flag.String("resources", "default", "resource mode: default, moderate, unlimited, custom")
	customReadBuf := flag.Int("read-buf", 0, "DC read buffer size in bytes, used with -resources custom")
	customMemLimit := flag.Int64("mem-limit", 0, "memory limit in bytes, used with -resources custom")
	writeFile := flag.String("write-file", "", "path to file where the join link is appended")
	upstreamSocks := flag.String("upstream-socks", "", "route tunneled egress through this SOCKS5 proxy (host:port), e.g. a local VPN client")
	upstreamUser := flag.String("upstream-user", "", "upstream SOCKS5 username")
	upstreamPass := flag.String("upstream-pass", "", "upstream SOCKS5 password")
	debugFlag := flag.Bool("debug", false, "verbose debug logging")
	allowPrivate := flag.Bool("allow-private-dst", false, "let the joiner reach private/internal addresses through this creator")
	allowLoopback := flag.Bool("allow-loopback", false, "let the joiner reach the creator's own loopback through this creator")
	flag.Parse()
	common.Debug = *debugFlag
	common.AllowPrivateDst = *allowPrivate
	common.AllowLoopbackDst = *allowLoopback

	var readBuf int
	var memLimit int64
	switch *resources {
	case "moderate":
		readBuf = 16384
		memLimit = 64 << 20
	case "default":
		readBuf = common.DCBufSize
		memLimit = 128 << 20
	case "unlimited":
		readBuf = common.RTPBufSize
		memLimit = 256 << 20
	case "custom":
		readBuf = *customReadBuf
		if readBuf == 0 {
			readBuf = common.RTPBufSize
		}
		memLimit = *customMemLimit
		if memLimit == 0 {
			memLimit = 256 << 20
		}
	default:
		log.Fatalf("[config] unknown resources mode: %s, use moderate, default, unlimited or custom", *resources)
	}
	if memLimit > 0 {
		debug.SetMemoryLimit(memLimit)
	}
	log.Printf("[config] resources=%s read-buf=%d mem-limit=%d", *resources, readBuf, memLimit)

	if *cookiesPath == "" {
		log.Fatalf("[auth] --cookies is required")
	}
	if *loginFlag {
		if err := blink.LoginToFile(*cookiesPath, nil, nil, common.EmitStatus, log.Printf); err != nil {
			log.Fatalf("[login] %v", err)
		}
		return
	}

	credentials, err := blink.LoadCredentials(*cookiesPath, nil, log.Printf)
	if err != nil {
		common.EmitAuthError(common.AuthErrorSessionExpired)
		log.Fatalf("[auth] %v", err)
	}
	var chatID int64
	if *roomFlag != "" {
		chatID, err = credentials.ResolveChatID(*roomFlag)
		if err != nil {
			common.EmitAuthErrorFor(err)
			log.Fatalf("[config] --room: %v", err)
		}
	} else {
		chatName := blink.RandomChatName()
		chatID, err = credentials.CreateChat(chatName)
		if err != nil {
			common.EmitAuthErrorFor(err)
			log.Fatalf("[room] %v", err)
		}
		log.Printf("[room] created chat=%d name=%q", chatID, chatName)
	}
	connection, err := credentials.ConnectCall(chatID)
	if err != nil {
		common.EmitAuthErrorFor(err)
		log.Fatalf("[call] %v", err)
	}
	log.Printf("[call] chat=%d call=%s created=%v endpoint=%s", chatID, connection.CallID, connection.Created, connection.APIEndpoint)

	joinLink := blink.JoinLink(chatID)
	if *writeFile != "" {
		file, err := os.OpenFile(*writeFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatalf("Failed to open write-file: %v", err)
		}
		fmt.Fprintln(file, joinLink)
		file.Close()
		log.Printf("[config] Wrote join link to %s", *writeFile)
	}

	keySource := strconv.FormatInt(chatID, 10)
	obfuscator, err := tunnel.NewTunnelObfuscator(tunnel.DeriveSecretFromJoinLink(keySource))
	if err != nil {
		log.Fatalf("[obf] init failed: %v", err)
	}
	log.Printf("[obf] key-source=%q localEpoch=0x%08x", keySource, obfuscator.LocalEpoch())

	var activeBridge *tunnel.RelayBridge
	makeSession := func(current blink.CallConnection) *blink.Session {
		session := blink.NewSession(blink.SessionConfig{
			RoomToken:  current.Token,
			ServerURL:  current.APIEndpoint,
			Obfuscator: obfuscator,
			LogFn:      log.Printf,
			ReadBuf:    readBuf,
		})
		session.OnConnected = func(dataTunnel tunnel.DataTunnel) {
			if activeBridge != nil {
				activeBridge.Reset()
			}
			bridgeReadBuf := common.VP8BufSize
			mode := "video"
			switch dataTunnel.(type) {
			case *rtc.DCTunnel:
				bridgeReadBuf = readBuf
				mode = "dc"
			case *rtc.MultiTrackKCPTunnel:
				bridgeReadBuf = readBuf
				mode = "video+kcp"
			}
			activeBridge = tunnel.NewRelayBridge(dataTunnel, "creator", bridgeReadBuf, log.Printf)
			activeBridge.SetUpstreamSocks(*upstreamSocks, *upstreamUser, *upstreamPass)
			activeBridge.SetOnPeerConfig(func(fps, batch, trackCount int) {
				session.AdaptTrackCount(trackCount)
			})
			fmt.Printf("\n  TUNNEL CONNECTED mode=%s\n", mode)
		}
		session.OnPeerRestart = func() {
			if activeBridge != nil {
				log.Printf("[creator] new peer detected, resetting relay bridge")
				activeBridge.Reset()
			}
		}
		return session
	}

	fmt.Println("")
	fmt.Println("  CALL CREATED")
	fmt.Println("  join_link: " + joinLink)
	fmt.Println("")

	for {
		session := makeSession(connection)
		if err := session.Start(); err != nil {
			log.Printf("[session] start failed: %v, retrying in 5s", err)
			session.Close()
			time.Sleep(5 * time.Second)
		} else {
			<-session.Done()
			log.Printf("[session] ended, rejoining in 3s")
			session.Close()
		}

		if activeBridge != nil {
			activeBridge.Reset()
		}
		time.Sleep(3 * time.Second)

		next, err := credentials.ConnectCall(chatID)
		if err != nil {
			log.Printf("[rejoin] calls connect failed: %v, retrying in 5s", err)
			time.Sleep(5 * time.Second)
			continue
		}
		connection = next
		log.Printf("[rejoin] chat=%d call=%s created=%v", chatID, connection.CallID, connection.Created)
	}
}
