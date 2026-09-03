package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"whitelist-bypass/relay/common"
	"whitelist-bypass/relay/dion"
	"whitelist-bypass/relay/tunnel"
)

func main() {
	common.MaybePrintVersion()
	cookiesPath := flag.String("cookies", "", "path to cookies-dion.json (exported from creator-app)")
	roomFlag := flag.String("room", "", "event slug or https://dion.vc/event/<slug> to rejoin (empty = create new room)")
	displayName := flag.String("name", "Headless", "display name in the room")
	resources := flag.String("resources", "default", "resource mode: moderate, default, unlimited")
	writeFile := flag.String("write-file", "", "path to file where active slug is appended")
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
	log.Printf("[config] resources=%s name=%q", *resources, *displayName)

	if *cookiesPath == "" {
		log.Fatalf("[FATAL] --cookies is required")
	}

	auth, err := dion.NewSession(nil)
	if err != nil {
		log.Fatalf("[FATAL] NewSession: %v", err)
	}
	if err := auth.LoadCookiesFromFile(*cookiesPath); err != nil {
		log.Fatalf("[FATAL] LoadCookiesFromFile: %v", err)
	}
	if !auth.HasCredentials() && !auth.HasRefreshCookie() {
		log.Fatalf("[FATAL] %s has neither cookies nor credentials, add \"email\" and \"password\" fields to it", *cookiesPath)
	}
	if auth.HasCredentials() {
		log.Printf("[auth] credentials present, session will re-login automatically when the refresh token dies")
	}
	if err := auth.EnsureValidToken(); err != nil {
		log.Fatalf("[FATAL] EnsureValidToken: %v", err)
	}

	requestedRoom := dion.ParseRoom(*roomFlag)
	var event *dion.EventInfo
	if requestedRoom != "" {
		event, err = auth.GetEventBySlug(requestedRoom)
		if err != nil {
			log.Fatalf("[FATAL] GetEventBySlug(%s): %v", requestedRoom, err)
		}
		log.Printf("[room] rejoined room=%s id=%s", event.Slug, event.ID)
	} else {
		event, err = auth.CreateRoom()
		if err != nil {
			log.Fatalf("[FATAL] CreateRoom: %v", err)
		}
		log.Printf("[room] created room=%s id=%s", event.Slug, event.ID)
	}

	joinLink := "dion://" + event.Slug
	if *writeFile != "" {
		fileHandle, err := os.OpenFile(*writeFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatalf("[FATAL] open write-file: %v", err)
		}
		fmt.Fprintln(fileHandle, joinLink)
		fileHandle.Close()
		log.Printf("[config] wrote join link to %s", *writeFile)
	}

	obf, err := tunnel.NewTunnelObfuscator(tunnel.DeriveSecretFromJoinLink(event.Slug))
	if err != nil {
		log.Fatalf("[FATAL] tunnel obfuscator: %v", err)
	}
	log.Printf("[obf] key-source=%q localEpoch=0x%08x", event.Slug, obf.LocalEpoch())

	fmt.Println("")
	fmt.Println("  CALL CREATED")
	fmt.Println("  join_link: " + joinLink)
	fmt.Println("")

	shutdownChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, os.Interrupt, syscall.SIGTERM)

	var activeBridge *tunnel.RelayBridge
	for {
		call := dion.NewCall(dion.CallConfig{
			Auth:        auth,
			Event:       event,
			Obfuscator:  obf,
			DisplayName: *displayName,
			LogFn:       log.Printf,
			Role:        dion.RoleCreator,
		})
		call.OnConnected = func(tun tunnel.DataTunnel) {
			if activeBridge != nil {
				activeBridge.Reset()
			}
			activeBridge = tunnel.NewRelayBridge(tun, "creator", common.VP8BufSize, log.Printf)
			activeBridge.SetUpstreamSocks(*upstreamSocks, *upstreamUser, *upstreamPass)
			activeBridge.MarkReady()
			fmt.Println("")
			fmt.Println("  TUNNEL CONNECTED")
			fmt.Println("")
		}
		call.OnPeerRestart = func() {
			if activeBridge != nil {
				log.Printf("[call] new peer subscribed, resetting relay bridge")
				activeBridge.Reset()
			}
		}

		runDone := make(chan error, 1)
		go func() {
			if err := call.Start(); err != nil {
				runDone <- err
				return
			}
			<-call.Done()
			runDone <- nil
		}()

		select {
		case err := <-runDone:
			if err != nil {
				log.Printf("[call] start failed: %v", err)
			} else {
				log.Printf("[call] ended")
			}
		case <-shutdownChan:
			log.Printf("[shutdown] signal received, exiting")
			call.Close()
			if activeBridge != nil {
				activeBridge.Reset()
			}
			return
		}

		call.Close()
		if activeBridge != nil {
			activeBridge.Reset()
		}

		if err := auth.EnsureValidToken(); err != nil {
			log.Printf("[rejoin] EnsureValidToken failed: %v", err)
		}
		select {
		case <-time.After(3 * time.Second):
		case <-shutdownChan:
			log.Printf("[shutdown] signal received during cooldown, exiting")
			return
		}
	}
}
