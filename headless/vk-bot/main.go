package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"whitelist-bypass/relay/common"
)

const (
	vkAPIBase    = "https://api.vk.com/method"
	vkAPIVersion = "5.199"
	pollWait     = 25
	retryDelay   = 3 * time.Second
	spawnTimeout = 60 * time.Second
)

var idCounter atomic.Uint32

func newSessionID() string {
	return fmt.Sprintf("%04x", idCounter.Add(1)&0xffff)
}

func resolveBin(dir, name string) (string, error) {
	exact := filepath.Join(dir, name)
	if _, err := os.Stat(exact); err == nil {
		return exact, nil
	}
	matches, _ := filepath.Glob(filepath.Join(dir, name+"*"))
	for _, m := range matches {
		info, err := os.Stat(m)
		if err == nil && !info.IsDir() {
			return m, nil
		}
	}
	return "", fmt.Errorf("binary %s not found in %s", name, dir)
}

type session struct {
	id       string
	platform string
	link     string
	cmd      *exec.Cmd
	started  time.Time
	logPath  string
}

type bot struct {
	token       string
	groupID     string
	userIDs     []string
	binsDir     string
	vkCookies   string
	tmCookies   string
	wbCookies   string
	dionCookies string
	sessionsDir string
	resources   string

	upstreamSocks string
	upstreamUser  string
	upstreamPass  string

	server, key, ts string

	mu           sync.Mutex
	sessions     map[string]*session
	awaitingJoin map[int64]bool
}

func (b *bot) api(method string, params url.Values) (json.RawMessage, error) {
	params.Set("v", vkAPIVersion)
	params.Set("access_token", b.token)
	resp, err := http.Get(vkAPIBase + "/" + method + "?" + params.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Response json.RawMessage `json:"response"`
		Error    struct {
			Code int    `json:"error_code"`
			Msg  string `json:"error_msg"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.Error.Code != 0 {
		return nil, fmt.Errorf("vk api %s: %d %s", method, out.Error.Code, out.Error.Msg)
	}
	return out.Response, nil
}

func (b *bot) getLongPollServer() error {
	p := url.Values{}
	p.Set("group_id", b.groupID)
	raw, err := b.api("groups.getLongPollServer", p)
	if err != nil {
		return err
	}
	var lp struct{ Server, Key, Ts string }
	if err := json.Unmarshal(raw, &lp); err != nil {
		return err
	}
	b.server, b.key, b.ts = lp.Server, lp.Key, lp.Ts
	return nil
}

func (b *bot) sendMessage(peerID int64, text, kb string) {
	p := url.Values{}
	p.Set("peer_id", fmt.Sprint(peerID))
	p.Set("message", text)
	p.Set("random_id", fmt.Sprint(time.Now().UnixNano()))
	if kb != "" {
		p.Set("keyboard", kb)
	}
	if _, err := b.api("messages.send", p); err != nil {
		log.Printf("[bot] send to %d: %v", peerID, err)
	}
}

func (b *bot) poll() error {
	u := fmt.Sprintf("%s?act=a_check&key=%s&ts=%s&wait=%d", b.server, b.key, b.ts, pollWait)
	resp, err := http.Get(u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var data struct {
		Ts      string `json:"ts"`
		Failed  int    `json:"failed"`
		Updates []struct {
			Type   string `json:"type"`
			Object struct {
				Message struct {
					Text    string `json:"text"`
					FromID  int64  `json:"from_id"`
					PeerID  int64  `json:"peer_id"`
					Payload string `json:"payload"`
				} `json:"message"`
			} `json:"object"`
		} `json:"updates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return err
	}
	if data.Failed != 0 {
		return fmt.Errorf("longpoll failed=%d", data.Failed)
	}
	if data.Ts != "" {
		b.ts = data.Ts
	}
	for _, u := range data.Updates {
		if u.Type != "message_new" {
			continue
		}
		m := u.Object.Message
		b.handleMessage(m.PeerID, m.FromID, strings.TrimSpace(m.Text), m.Payload)
	}
	return nil
}

func (b *bot) handleMessage(peerID, fromID int64, text, payload string) {
	if len(b.userIDs) > 0 {
		from := fmt.Sprint(fromID)
		allowed := false
		for _, id := range b.userIDs {
			if id == from {
				allowed = true
				break
			}
		}
		if !allowed {
			return
		}
	}
	log.Printf("[bot] msg from=%d peer=%d text=%q payload=%q", fromID, peerID, text, payload)

	if payload != "" {
		var p struct {
			Cmd string `json:"cmd"`
			ID  string `json:"id"`
		}
		if err := json.Unmarshal([]byte(payload), &p); err == nil && p.Cmd != "" {
			if p.Cmd == "join-prompt" {
				b.mu.Lock()
				b.awaitingJoin[peerID] = true
				b.mu.Unlock()
				b.sendMessage(peerID, "Paste a join link", waitingKeyboard())
				return
			}
			b.mu.Lock()
			delete(b.awaitingJoin, peerID)
			b.mu.Unlock()
			switch p.Cmd {
			case "vk", "tm", "wb", "dion":
				b.handleSpawn(peerID, p.Cmd, "")
				return
			case "list":
				b.handleList(peerID)
				return
			case "menu":
				b.showMenu(peerID)
				return
			case "close":
				if p.ID != "" {
					b.handleClose(peerID, p.ID)
					return
				}
			}
		}
	}

	b.mu.Lock()
	wasAwaiting := b.awaitingJoin[peerID]
	b.mu.Unlock()
	if platform, target, ok := detectJoinLink(text); ok {
		b.mu.Lock()
		delete(b.awaitingJoin, peerID)
		b.mu.Unlock()
		b.handleSpawn(peerID, platform, target)
		return
	}
	if wasAwaiting {
		b.sendMessage(peerID, "Couldn't detect a VK / Telemost / WBStream / DION link. Paste a join link or press Back.", waitingKeyboard())
		return
	}

	switch {
	case text == "/start" || text == "start":
		b.showMenu(peerID)
	case strings.HasPrefix(text, "/vk"):
		b.handleSpawn(peerID, "vk", "")
	case strings.HasPrefix(text, "/tm"):
		b.handleSpawn(peerID, "tm", "")
	case strings.HasPrefix(text, "/wb"):
		b.handleSpawn(peerID, "wb", "")
	case strings.HasPrefix(text, "/dion"):
		b.handleSpawn(peerID, "dion", "")
	case text == "/list":
		b.handleList(peerID)
	case strings.HasPrefix(text, "/close "):
		id := strings.TrimSpace(strings.TrimPrefix(text, "/close "))
		b.handleClose(peerID, id)
	}
}

func detectJoinLink(text string) (platform, target string, ok bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", "", false
	}
	lower := strings.ToLower(trimmed)
	switch {
	case strings.HasPrefix(lower, "wbstream://"), strings.Contains(lower, "stream.wb.ru"):
		return "wb", trimmed, true
	case strings.Contains(lower, "telemost.yandex"):
		return "tm", trimmed, true
	case strings.HasPrefix(lower, "dion://"), strings.Contains(lower, "dion.vc"):
		return "dion", trimmed, true
	case strings.Contains(lower, "vk.com/call/join"):
		return "vk", trimmed, true
	}
	return "", "", false
}

func (b *bot) showMenu(peerID int64) {
	b.sendMessage(peerID, "Pick a platform or list active sessions.", mainMenuKeyboard())
}

func formatSession(s *session) string {
	uptime := time.Since(s.started).Round(time.Second)
	return fmt.Sprintf("[%s] %s up %s\n%s", s.id, s.platform, uptime, s.link)
}

func (b *bot) handleSpawn(peerID int64, platform, joinTarget string) {
	if joinTarget != "" {
		b.sendMessage(peerID, fmt.Sprintf("Joining %s...", platform), "")
	} else {
		b.sendMessage(peerID, fmt.Sprintf("Starting %s...", platform), "")
	}
	sess, err := b.spawn(platform, joinTarget)
	if err != nil {
		b.sendMessage(peerID, fmt.Sprintf("%s failed: %v", platform, err), mainMenuKeyboard())
		return
	}
	reply := sess.link
	if joinTarget != "" {
		reply = "Joined successfully"
	}
	b.sendMessage(peerID, reply, mainMenuKeyboard())
}

func (b *bot) spawn(platform, joinTarget string) (*session, error) {
	var binName, cookies, joinFlag string
	switch platform {
	case "vk":
		binName = "headless-vk-creator"
		cookies = b.vkCookies
		joinFlag = "--vk-link"
	case "tm":
		binName = "headless-telemost-creator"
		cookies = b.tmCookies
		joinFlag = "--tm-link"
	case "wb":
		binName = "headless-wbstream-creator"
		cookies = b.wbCookies
		joinFlag = "--room"
	case "dion":
		binName = "headless-dion-creator"
		cookies = b.dionCookies
		joinFlag = "--room"
	default:
		return nil, fmt.Errorf("unknown platform: %s", platform)
	}
	bin, err := resolveBin(b.binsDir, binName)
	if err != nil {
		return nil, err
	}
	if cookies == "" {
		return nil, fmt.Errorf("cookies required for %s", platform)
	}

	id := newSessionID()
	linkFile, err := os.CreateTemp("", "vkbot-link-*.txt")
	if err != nil {
		return nil, err
	}
	linkFile.Close()
	os.Remove(linkFile.Name())

	var logF *os.File
	logPath := ""
	if b.sessionsDir != "" {
		if err := os.MkdirAll(b.sessionsDir, 0755); err != nil {
			return nil, err
		}
		logPath = filepath.Join(b.sessionsDir, fmt.Sprintf("%s-%s.log", platform, id))
		f, err := os.Create(logPath)
		if err != nil {
			return nil, err
		}
		logF = f
	}

	args := []string{"--write-file", linkFile.Name(), "--resources", b.resources, "--cookies", cookies}
	if joinTarget != "" {
		args = append(args, joinFlag, joinTarget)
	}
	if b.upstreamSocks != "" {
		args = append(args, "--upstream-socks", b.upstreamSocks)
		if b.upstreamUser != "" {
			args = append(args, "--upstream-user", b.upstreamUser)
		}
		if b.upstreamPass != "" {
			args = append(args, "--upstream-pass", b.upstreamPass)
		}
	}
	cmd := exec.Command(bin, args...)
	if logF != nil {
		cmd.Stdout = logF
		cmd.Stderr = logF
	}
	if err := cmd.Start(); err != nil {
		if logF != nil {
			logF.Close()
		}
		return nil, err
	}
	if logPath != "" {
		log.Printf("[bot] spawned %s id=%s pid=%d log=%s", platform, id, cmd.Process.Pid, logPath)
	} else {
		log.Printf("[bot] spawned %s id=%s pid=%d", platform, id, cmd.Process.Pid)
	}

	link, err := waitForLink(linkFile.Name(), spawnTimeout)
	if err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		if logF != nil {
			logF.Close()
		}
		return nil, err
	}

	sess := &session{
		id: id, platform: platform, link: link, cmd: cmd,
		started: time.Now(), logPath: logPath,
	}
	b.mu.Lock()
	b.sessions[id] = sess
	b.mu.Unlock()
	go b.watchSession(sess, logF)
	return sess, nil
}

func waitForLink(path string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			line := strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0])
			if line != "" {
				return line, nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return "", fmt.Errorf("creator did not write link within %s", timeout)
}

func (b *bot) watchSession(sess *session, logF *os.File) {
	sess.cmd.Wait()
	if logF != nil {
		logF.Close()
	}
	b.mu.Lock()
	delete(b.sessions, sess.id)
	b.mu.Unlock()
	log.Printf("[bot] session %s exited", sess.id)
}

func (b *bot) handleList(peerID int64) {
	b.mu.Lock()
	list := make([]*session, 0, len(b.sessions))
	for _, s := range b.sessions {
		list = append(list, s)
	}
	b.mu.Unlock()
	if len(list) == 0 {
		b.sendMessage(peerID, "No active sessions", mainMenuKeyboard())
		return
	}
	var lines []string
	for _, s := range list {
		lines = append(lines, formatSession(s))
	}
	b.sendMessage(peerID, strings.Join(lines, "\n\n"), sessionsKeyboard(list))
}

func (b *bot) handleClose(peerID int64, id string) {
	b.mu.Lock()
	sess, ok := b.sessions[id]
	b.mu.Unlock()
	if !ok {
		b.sendMessage(peerID, fmt.Sprintf("Session %s not found", id), mainMenuKeyboard())
		return
	}
	sess.cmd.Process.Signal(syscall.SIGTERM)
	b.sendMessage(peerID, fmt.Sprintf("Session %s closed", id), mainMenuKeyboard())
}

func (b *bot) run() error {
	if err := b.getLongPollServer(); err != nil {
		return fmt.Errorf("getLongPollServer: %w", err)
	}
	log.Printf("[bot] longpoll server=%s ts=%s", b.server, b.ts)
	for {
		if err := b.poll(); err != nil {
			log.Printf("[bot] poll: %v", err)
			time.Sleep(retryDelay)
			if err := b.getLongPollServer(); err != nil {
				log.Printf("[bot] reconnect: %v", err)
			}
		}
	}
}

func main() {
	common.MaybePrintVersion()
	token := flag.String("token", "", "VK community access token (required)")
	groupID := flag.String("group-id", "", "VK community ID, digits only (required)")
	userID := flag.String("user-id", "", "comma-separated VK user IDs allowed to issue commands (empty = anyone)")
	binsDir := flag.String("bins-dir", "", "directory containing headless-*-creator binaries (required)")
	vkCookies := flag.String("vk-cookies", "", "path to VK cookies JSON")
	tmCookies := flag.String("tm-cookies", "", "path to Yandex cookies JSON for Telemost")
	wbCookies := flag.String("wb-cookies", "", "path to WB Stream cookies JSON")
	dionCookies := flag.String("dion-cookies", "", "path to DION cookies JSON")
	sessionsDir := flag.String("sessions-dir", "", "directory for per-session creator logs (optional)")
	resources := flag.String("resources", "default", "resource mode forwarded to spawned creators: default, moderate, unlimited")
	upstreamSocks := flag.String("upstream-socks", "", "forward to spawned creators: route tunneled egress through this SOCKS5 proxy (host:port), e.g. a local VPN client")
	upstreamUser := flag.String("upstream-user", "", "upstream SOCKS5 username forwarded to spawned creators")
	upstreamPass := flag.String("upstream-pass", "", "upstream SOCKS5 password forwarded to spawned creators")
	flag.Parse()

	if *token == "" {
		*token = os.Getenv("VK_TOKEN")
	}
	if *upstreamPass == "" {
		*upstreamPass = os.Getenv("UPSTREAM_PASS")
	}

	switch *resources {
	case "default", "moderate", "unlimited":
	default:
		log.Fatalf("--resources must be one of default|moderate|unlimited (got %q); 'custom' is not supported because it needs per-binary tuning flags", *resources)
	}

	if *token == "" || *groupID == "" || *binsDir == "" {
		log.Fatal("--token, --group-id, --bins-dir are required")
	}

	var allowedUsers []string
	for _, id := range strings.Split(*userID, ",") {
		trimmed := strings.TrimSpace(id)
		if trimmed != "" {
			allowedUsers = append(allowedUsers, trimmed)
		}
	}

	b := &bot{
		token:         *token,
		groupID:       *groupID,
		userIDs:       allowedUsers,
		binsDir:       *binsDir,
		vkCookies:     *vkCookies,
		tmCookies:     *tmCookies,
		wbCookies:     *wbCookies,
		dionCookies:   *dionCookies,
		sessionsDir:   *sessionsDir,
		resources:     *resources,
		upstreamSocks: *upstreamSocks,
		upstreamUser:  *upstreamUser,
		upstreamPass:  *upstreamPass,
		sessions:      map[string]*session{},
		awaitingJoin:  map[int64]bool{},
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		b.mu.Lock()
		log.Printf("[bot] shutting down, killing %d sessions", len(b.sessions))
		for _, s := range b.sessions {
			s.cmd.Process.Signal(syscall.SIGTERM)
		}
		b.mu.Unlock()
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}()

	if err := b.run(); err != nil {
		log.Fatalf("[bot] %v", err)
	}
}
