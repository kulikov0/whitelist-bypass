package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"runtime/debug"
	"time"

	"whitelist-bypass/relay/common"
	"whitelist-bypass/relay/tunnel"
	"whitelist-bypass/relay/wbstream"
)

func main() {
	common.MaybePrintVersion()
	cookiesPath := flag.String("cookies", "", "path to cookies-wbstream.json")
	roomFlag := flag.String("room", "", "WB Stream room id, wbstream://<id>, or https://stream.wb.ru/room/<id> (empty = create new)")
	displayName := flag.String("name", "Headless", "display name in the room")
	resources := flag.String("resources", "default", "resource mode: default, moderate, unlimited, custom")
	customReadBuf := flag.Int("read-buf", 0, "DC read buffer size in bytes, used with -resources custom")
	customMemLimit := flag.Int64("mem-limit", 0, "memory limit in bytes, used with -resources custom")
	writeFile := flag.String("write-file", "", "path to file where active room id is appended")
	upstreamSocks := flag.String("upstream-socks", "", "route tunneled egress through this SOCKS5 proxy (host:port), e.g. a local VPN client")
	upstreamUser := flag.String("upstream-user", "", "upstream SOCKS5 username")
	upstreamPass := flag.String("upstream-pass", "", "upstream SOCKS5 password")
	flag.Parse()

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
		log.Fatalf("[config] unknown resources mode: %s (use moderate, default, unlimited, custom)", *resources)
	}
	if memLimit > 0 {
		debug.SetMemoryLimit(memLimit)
	}
	log.Printf("[config] resources=%s read-buf=%d mem-limit=%d", *resources, readBuf, memLimit)

	if *cookiesPath == "" {
		log.Fatalf("[auth] --cookies is required")
	}
	rawCookies := common.LoadCookies(*cookiesPath)
	deviceID := common.CookieValue(rawCookies, "__wb_device_id")
	if deviceID == "" {
		log.Fatalf("[auth] cookies file is missing __wb_device_id; re-export via creator-app's 'Export Cookies' button")
	}
	cookieHeader := common.FilterCookies(rawCookies, wbstream.WBStreamCookieAllowlist)
	bearer, err := wbstream.RefreshAccessToken(nil, cookieHeader, deviceID)
	if err != nil {
		log.Fatalf("[auth] slide-v3 refresh: %v", err)
	}
	log.Printf("[auth] bearer refreshed (len=%d)", len(bearer))
	requestedRoom := wbstream.ParseRoomID(*roomFlag)
	roomID, roomToken, accessToken, serverURL, err := wbstream.AuthAsLoggedIn(nil, cookieHeader, bearer, requestedRoom, *displayName)
	if err != nil {
		log.Fatalf("[auth] %v", err)
	}
	log.Printf("[auth] room=%s server=%s", roomID, serverURL)

	if *writeFile != "" {
		f, err := os.OpenFile(*writeFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatalf("Failed to open write-file: %v", err)
		}
		fmt.Fprintln(f, "wbstream://"+roomID)
		f.Close()
		log.Printf("[config] Wrote join link to %s", *writeFile)
	}

	obf, err := tunnel.NewTunnelObfuscator(tunnel.DeriveSecretFromJoinLink(roomID))
	if err != nil {
		log.Fatalf("[obf] init failed: %v", err)
	}
	log.Printf("[obf] key-source=%q localEpoch=0x%08x", roomID, obf.LocalEpoch())

	var activeBridge *tunnel.RelayBridge
	makeSession := func(token, access, server string) *wbstream.Session {
		sess := wbstream.NewSession(wbstream.SessionConfig{
			RoomToken:   token,
			ServerURL:   server,
			DisplayName: *displayName,
			Obfuscator:  obf,
			LogFn:       log.Printf,
			RoomID:      roomID,
			AccessToken: access,
			ReadBuf:     readBuf,
		})
		sess.OnConnected = func(tun tunnel.DataTunnel) {
			if activeBridge != nil {
				activeBridge.Reset()
			}
			bridgeReadBuf := common.VP8BufSize
			mode := "video"
			switch tun.(type) {
			case *tunnel.DCTunnel:
				bridgeReadBuf = readBuf
				mode = "dc"
			case *tunnel.KCPTunnel:
				mode = "video+kcp"
			}
			activeBridge = tunnel.NewRelayBridge(tun, "creator", bridgeReadBuf, log.Printf)
			activeBridge.SetUpstreamSocks(*upstreamSocks, *upstreamUser, *upstreamPass)
			activeBridge.SetOnPeerConfig(func(fps, batch, trackCount int) {
				sess.AdaptTrackCount(trackCount)
			})
			fmt.Printf("\n  TUNNEL CONNECTED mode=%s\n", mode)
		}
		sess.OnPeerRestart = func() {
			if activeBridge != nil {
				log.Printf("[creator] new peer detected, resetting relay bridge")
				activeBridge.Reset()
			}
		}
		return sess
	}

	fmt.Println("")
	fmt.Println("  CALL CREATED")
	fmt.Println("  join_link: wbstream://" + roomID)
	fmt.Println("")

	for {
		sess := makeSession(roomToken, accessToken, serverURL)
		if err := sess.Start(); err != nil {
			log.Printf("[session] start failed: %v, retrying in 5s", err)
			sess.Close()
			time.Sleep(5 * time.Second)
		} else {
			<-sess.Done()
			log.Printf("[session] ended, rejoining in 3s")
			sess.Close()
		}

		if activeBridge != nil {
			activeBridge.Reset()
		}
		time.Sleep(3 * time.Second)

		newBearer, refreshErr := wbstream.RefreshAccessToken(nil, cookieHeader, deviceID)
		if refreshErr != nil {
			log.Printf("[rejoin] slide-v3 refresh failed: %v, retrying in 5s", refreshErr)
			time.Sleep(5 * time.Second)
			continue
		}
		bearer = newBearer
		_, newRoomToken, newAccessToken, newServerURL, err := wbstream.AuthAsLoggedIn(nil, cookieHeader, bearer, roomID, *displayName)
		if err != nil {
			log.Printf("[rejoin] auth failed: %v, retrying in 5s", err)
			time.Sleep(5 * time.Second)
			continue
		}
		roomToken = newRoomToken
		accessToken = newAccessToken
		serverURL = newServerURL
		log.Printf("[rejoin] refreshed token for room=%s server=%s", roomID, serverURL)
	}
}
