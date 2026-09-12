package bitrix

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/gorilla/websocket"

	"whitelist-bypass/relay/common"
)

const (
	pullWireVarint  = 0
	pullWireFixed64 = 1
	pullWireBytes   = 2
	pullWireFixed32 = 5
	pullMaxDepth    = 8
)

type PullClient struct {
	cfg    PullConfigResult
	ua     string
	origin string
	logFn  func(string, ...any)

	mu     sync.Mutex
	ws     *websocket.Conn
	closed bool

	onUsersAnswered func([]string)
	onUserLeave     func(string)
}

func NewPullClient(cfg PullConfigResult, ua, origin string, logFn func(string, ...any)) *PullClient {
	return &PullClient{cfg: cfg, ua: ua, origin: origin, logFn: logFn}
}

func (p *PullClient) SetOnUsersAnswered(fn func([]string)) { p.onUsersAnswered = fn }

func (p *PullClient) SetOnUserLeave(fn func(string)) { p.onUserLeave = fn }

func (p *PullClient) dialURL() string {
	q := url.Values{}
	q.Set("CHANNEL_ID", p.cfg.ChannelID)
	q.Set("binaryMode", "true")
	if p.cfg.Hostname != "" {
		q.Set("hostname", p.cfg.Hostname)
	}
	if p.cfg.Revision > 0 {
		q.Set("revision", fmt.Sprintf("%d", p.cfg.Revision))
	}
	sep := "?"
	if strings.Contains(p.cfg.WebSocket, "?") {
		sep = "&"
	}
	return p.cfg.WebSocket + sep + q.Encode()
}

func (p *PullClient) connect() error {
	headers := http.Header{}
	if p.ua != "" {
		headers.Set("User-Agent", p.ua)
	}
	if p.origin != "" {
		headers.Set("Origin", p.origin)
	}
	conn, resp, err := websocket.DefaultDialer.Dial(p.dialURL(), headers)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("subws2 dial: %w, status %d", err, resp.StatusCode)
		}
		return fmt.Errorf("subws2 dial: %w", err)
	}
	p.mu.Lock()
	p.ws = conn
	p.mu.Unlock()
	p.logFn("[bx] subws2 connected")
	return nil
}

func (p *PullClient) Run() error {
	if err := p.connect(); err != nil {
		return err
	}
	for {
		p.mu.Lock()
		ws := p.ws
		closed := p.closed
		p.mu.Unlock()
		if closed || ws == nil {
			return nil
		}
		_, data, err := ws.ReadMessage()
		if err != nil {
			p.mu.Lock()
			closed = p.closed
			p.mu.Unlock()
			if closed {
				return nil
			}
			return fmt.Errorf("subws2 read: %w", err)
		}
		p.handleFrame(data)
	}
}

func (p *PullClient) Close() {
	p.mu.Lock()
	p.closed = true
	ws := p.ws
	p.ws = nil
	p.mu.Unlock()
	if ws != nil {
		common.CloseWS(ws)
	}
}

func (p *PullClient) handleFrame(frame []byte) {
	for _, body := range extractPullBodies(frame) {
		var msg struct {
			ModuleID string `json:"module_id"`
			Command  string `json:"command"`
			Params   struct {
				Senders []struct {
					SenderID json.Number `json:"senderId"`
				} `json:"senders"`
				UserID json.Number `json:"userId"`
			} `json:"params"`
		}
		if err := json.Unmarshal(body, &msg); err != nil {
			continue
		}
		switch {
		case msg.ModuleID == "call" && strings.Contains(msg.Command, "usersAnswered"):
			var ids []string
			for _, s := range msg.Params.Senders {
				if id := s.SenderID.String(); id != "" {
					ids = append(ids, id)
				}
			}
			if len(ids) > 0 && p.onUsersAnswered != nil {
				p.onUsersAnswered(ids)
			}
		case msg.ModuleID == "im" && msg.Command == "chatUserLeave":
			if uid := msg.Params.UserID.String(); uid != "" && p.onUserLeave != nil {
				p.onUserLeave(uid)
			}
		}
	}
}

func extractPullBodies(buf []byte) [][]byte {
	return collectPullBodies(buf, nil, 0)
}

func collectPullBodies(buf []byte, out [][]byte, depth int) [][]byte {
	if depth > pullMaxDepth {
		return out
	}
	for len(buf) > 0 {
		value, rest, ok := nextPullField(buf)
		if !ok {
			return out
		}
		buf = rest
		switch {
		case len(value) == 0:
		case value[0] == '{':
			out = append(out, value)
		default:
			out = collectPullBodies(value, out, depth+1)
		}
	}
	return out
}

func nextPullField(buf []byte) (value, rest []byte, ok bool) {
	tag, n := binary.Uvarint(buf)
	if n <= 0 {
		return nil, nil, false
	}
	buf = buf[n:]
	switch tag & 7 {
	case pullWireVarint:
		_, m := binary.Uvarint(buf)
		if m <= 0 {
			return nil, nil, false
		}
		return nil, buf[m:], true
	case pullWireFixed64:
		return skipPullFixed(buf, 8)
	case pullWireFixed32:
		return skipPullFixed(buf, 4)
	case pullWireBytes:
		l, m := binary.Uvarint(buf)
		if m <= 0 || l > uint64(len(buf)-m) {
			return nil, nil, false
		}
		end := m + int(l)
		return buf[m:end], buf[end:], true
	}
	return nil, nil, false
}

func skipPullFixed(buf []byte, size int) (value, rest []byte, ok bool) {
	if len(buf) < size {
		return nil, nil, false
	}
	return nil, buf[size:], true
}
