package main

import (
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"

	headless "github.com/kulikov0/headless-client"
	"whitelist-bypass/relay/bitrix"
	"whitelist-bypass/relay/common"
	"whitelist-bypass/relay/tunnel"
)

func main() {
	common.MaybePrintVersion()
	cookiesPath := flag.String("cookies", "", "path to cookies-bitrix.json (exported from creator-app: email/password/portal/cookies)")
	roomFlag := flag.String("room", "", "conference link https://portal/video/CODE or alias to rejoin as host (empty = create new room)")
	resources := flag.String("resources", "default", "resource mode: moderate, default, unlimited")
	writeFile := flag.String("write-file", "", "path to file where the active guest link is appended")
	upstreamSocks := flag.String("upstream-socks", "", "route tunneled egress through this SOCKS5 proxy (host:port), e.g. a local VPN client")
	upstreamUser := flag.String("upstream-user", "", "upstream SOCKS5 username")
	upstreamPass := flag.String("upstream-pass", "", "upstream SOCKS5 password")
	debugFlag := flag.Bool("debug", false, "verbose debug logging")
	allowPrivate := flag.Bool("allow-private-dst", false, "let the joiner reach private/internal addresses through this creator")
	flag.Parse()
	common.Debug = *debugFlag
	common.AllowPrivateDst = *allowPrivate

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
	log.Printf("[config] resources=%s", *resources)

	if *cookiesPath == "" {
		log.Fatalf("[FATAL] --cookies is required")
	}

	creds, err := bitrix.LoadCredentials(*cookiesPath)
	if err != nil {
		log.Fatalf("[FATAL] LoadCredentials: %v", err)
	}

	portal := creds.Portal
	alias := ""
	if *roomFlag != "" {
		if strings.Contains(*roomFlag, "://") {
			u, perr := url.Parse(*roomFlag)
			if perr != nil {
				log.Fatalf("[FATAL] bad room link: %v", perr)
			}
			portal = u.Scheme + "://" + u.Host
			alias = strings.TrimPrefix(u.Path, "/video/")
		} else {
			alias = *roomFlag
		}
	}
	if portal == "" {
		log.Fatalf("[FATAL] no portal: set \"portal\" in %s or pass a full --room link", *cookiesPath)
	}

	userAgent := headless.ChromeWindows.UserAgent()
	c, err := bitrix.NewClient(portal, userAgent)
	if err != nil {
		log.Fatalf("[FATAL] NewClient: %v", err)
	}
	if err := c.LoadSession(*cookiesPath); err != nil {
		common.EmitAuthError(common.AuthErrorSessionExpired)
		log.Fatalf("[FATAL] LoadSession: %v", err)
	}
	if err := c.EnsureLogin(); err != nil {
		if strings.Contains(err.Error(), "login rejected") {
			common.EmitAuthError(common.AuthErrorInvalidCredentials)
		} else {
			common.EmitAuthError(common.AuthErrorSessionExpired)
		}
		log.Fatalf("[FATAL] EnsureLogin: %v", err)
	}
	log.Printf("[auth] ready")

	var res bitrix.JoinResult
	joinLink := ""
	if alias != "" {
		log.Printf("[room] rejoining as HOST alias=%s", alias)
		res, err = c.JoinAsHost(alias)
		if err != nil {
			log.Fatalf("[FATAL] JoinAsHost(%s): %v", alias, err)
		}
		joinLink = portal + "/video/" + alias
	} else {
		res, joinLink, err = c.CreateAndJoin()
		if err != nil {
			log.Fatalf("[FATAL] CreateAndJoin: %v", err)
		}
		if u, perr := url.Parse(joinLink); perr == nil {
			alias = strings.TrimPrefix(u.Path, "/video/")
		}
		log.Printf("[room] created alias=%s", alias)
	}
	if alias == "" {
		log.Fatalf("[FATAL] could not resolve conference alias")
	}
	log.Printf("[room] roomId=%s mediaServer=%s", res.RoomID, res.MediaServerURL)

	if *writeFile != "" {
		fileHandle, ferr := os.OpenFile(*writeFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if ferr != nil {
			log.Fatalf("[FATAL] open write-file: %v", ferr)
		}
		fmt.Fprintln(fileHandle, joinLink)
		fileHandle.Close()
		log.Printf("[config] wrote join link to %s", *writeFile)
	}

	fmt.Println("")
	fmt.Println("  CALL CREATED")
	fmt.Println("  join_link: " + joinLink)
	fmt.Println("")

	var once sync.Once
	connected := make(chan struct{})
	sig, err := bitrix.ConnectSignal(bitrix.SignalConfig{
		SignalURL: bitrix.SignalURL(res),
		Origin:    portal,
		UserAgent: userAgent,
		LogFn:     log.Printf,
		OnConnected: func() {
			once.Do(func() { close(connected) })
		},
	})
	if err != nil {
		log.Fatalf("[FATAL] signal connect: %v", err)
	}
	defer sig.Close()

	ms, err := bitrix.NewMediaSession(bitrix.MediaParams{
		Signal: sig,
		Alias:  alias,
		Mode:   bitrix.TunnelModeAuto,
		FPS:    24,
		Batch:  30,
		LogFn:  log.Printf,
	})
	if err != nil {
		log.Fatalf("[FATAL] media session: %v", err)
	}
	ms.OnConnected = func(tun tunnel.DataTunnel) {
		readBuf := common.VP8BufSize
		switch tun.(type) {
		case *tunnel.DCTunnel, *tunnel.MultiTrackKCPTunnel:
			readBuf = common.DCBufSize
		}
		rb := tunnel.NewRelayBridge(tun, "creator", readBuf, log.Printf)
		rb.SetUpstreamSocks(*upstreamSocks, *upstreamUser, *upstreamPass)
		rb.MarkReady()
		log.Printf("[bx] creator egress ready transport=%T upstream=%q", tun, *upstreamSocks)
		fmt.Println("")
		fmt.Println("  TUNNEL CONNECTED")
		fmt.Println("")
	}

	go func() {
		<-connected
		if err := ms.Start(); err != nil {
			log.Printf("[bx] media start failed: %v", err)
		}
	}()

	go func() {
		if err := sig.Run(); err != nil {
			log.Printf("[signal] run ended: %v", err)
		}
	}()

	log.Printf("[bx] role=creator alias=%s waiting for media...", alias)

	shutdownChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, os.Interrupt, syscall.SIGTERM)
	<-shutdownChan
	log.Printf("[shutdown] signal received, exiting")
	ms.Stop()
}
