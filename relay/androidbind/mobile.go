package androidbind

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"

	"github.com/kulikov0/headless-client/websocket"
	_ "golang.org/x/mobile/bind"
	"whitelist-bypass/relay/common"
	"whitelist-bypass/relay/tunnel"
)

const readBufSize = 65536

var upgrader = websocket.Upgrader{
	CheckOrigin:     func(r *http.Request) bool { return true },
	ReadBufferSize:  readBufSize,
	WriteBufferSize: readBufSize,
}

type LogCallback interface {
	OnLog(msg string)
}

var logCb LogCallback

func logMsg(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if logCb != nil {
		logCb.OnLog(msg)
	} else {
		log.Print(msg)
	}
}

type wsWriter struct {
	ws     *websocket.Conn
	ch     chan []byte
	done   chan struct{}
	mu     sync.Mutex
	closed bool
}

func newWSWriter(ws *websocket.Conn) *wsWriter {
	w := &wsWriter{
		ws:   ws,
		ch:   make(chan []byte, 1024),
		done: make(chan struct{}),
	}
	go w.loop()
	return w
}

func (w *wsWriter) loop() {
	defer close(w.done)
	for msg := range w.ch {
		if err := w.ws.WriteMessage(websocket.BinaryMessage, msg); err != nil {
			logMsg("ws write error: %v", err)
			return
		}
	}
}

func (w *wsWriter) send(msg []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	select {
	case w.ch <- msg:
	default:
	}
}

func (w *wsWriter) close() {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		<-w.done
		return
	}
	w.closed = true
	close(w.ch)
	w.mu.Unlock()
	<-w.done
}

var activeJoiner struct {
	sync.Mutex
	j         *joinerRelay
	ws        *http.Server
	wsPort    int
	socksPort int
}

func ActiveWsPort() int    { return activeJoiner.wsPort }
func ActiveSocksPort() int { return activeJoiner.socksPort }

func SetDebug(enabled bool) { common.Debug = enabled }

func StopJoiner() {
	activeJoiner.Lock()
	defer activeJoiner.Unlock()
	if activeJoiner.ws != nil {
		activeJoiner.ws.Close()
		activeJoiner.ws = nil
	}
	if activeJoiner.j != nil {
		activeJoiner.j.close()
		activeJoiner.j = nil
	}
	logMsg("dc-joiner: stopped")
}

func StartJoiner(wsPort, socksPort int, socksHost, socksUser, socksPass string, cb LogCallback) error {
	StopJoiner()
	logCb = cb

	wsTunnel := tunnel.NewWSTunnel()
	bridge := tunnel.NewRelayBridgeWithAuth(wsTunnel, "joiner", readBufSize, logMsg, socksUser, socksPass)
	bridge.SetPersistentListener(true)
	j := &joinerRelay{wsTunnel: wsTunnel, bridge: bridge}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", j.handleWS)

	wsAddr := fmt.Sprintf("%s:%d", common.SocksLocalhostIP, wsPort)
	wsLn, err := net.Listen("tcp", wsAddr)
	if err != nil {
		return fmt.Errorf("dc-joiner: ws listen %s: %w", wsAddr, err)
	}
	wsSrv := &http.Server{Handler: mux}
	go func() {
		logMsg("dc-joiner: WebSocket on %s", wsAddr)
		if err := wsSrv.Serve(wsLn); err != nil && err != http.ErrServerClosed {
			logMsg("dc-joiner: ws server error: %v", err)
		}
	}()

	if socksHost == "" {
		socksHost = common.SocksLocalhostIP
	}
	socksAddr := fmt.Sprintf("%s:%d", socksHost, socksPort)

	activeJoiner.Lock()
	activeJoiner.j = j
	activeJoiner.ws = wsSrv
	activeJoiner.wsPort = wsPort
	activeJoiner.socksPort = socksPort
	activeJoiner.Unlock()

	return bridge.ListenSOCKS(socksAddr)
}

type joinerRelay struct {
	wsTunnel *tunnel.WSTunnel
	bridge   *tunnel.RelayBridge
	writerMu sync.Mutex
	writer   *wsWriter
}

func (j *joinerRelay) handleWS(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logMsg("dc-joiner: ws upgrade error: %v", err)
		return
	}
	writer := newWSWriter(ws)
	j.attachWriter(writer)
	j.bridge.MarkReady()
	logMsg("dc-joiner: browser connected via WebSocket")
	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			logMsg("dc-joiner: ws read error: %v", err)
			break
		}
		j.wsTunnel.Deliver(msg)
	}
	j.detachWriter(writer)
}

func (j *joinerRelay) attachWriter(writer *wsWriter) {
	j.writerMu.Lock()
	previous := j.writer
	j.writer = writer
	j.wsTunnel.SetSendFn(writer.send)
	j.writerMu.Unlock()
	if previous != nil {
		previous.close()
	}
}

func (j *joinerRelay) detachWriter(writer *wsWriter) {
	writer.close()
	j.writerMu.Lock()
	defer j.writerMu.Unlock()
	if j.writer != writer {
		return
	}
	j.writer = nil
	j.wsTunnel.SetSendFn(nil)
	j.wsTunnel.NotifyClose()
}

func (j *joinerRelay) close() {
	j.bridge.Close()
	j.writerMu.Lock()
	writer := j.writer
	j.writer = nil
	j.wsTunnel.SetSendFn(nil)
	j.writerMu.Unlock()
	if writer != nil {
		writer.close()
	}
}
