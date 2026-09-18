package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"

	"github.com/kulikov0/headless-client/websocket"
	"whitelist-bypass/relay/common"
	"whitelist-bypass/relay/tunnel"
)

const dcReadBufSize = 65536

var dcUpgrader = websocket.Upgrader{
	CheckOrigin:     func(r *http.Request) bool { return true },
	ReadBufferSize:  dcReadBufSize,
	WriteBufferSize: dcReadBufSize,
}

type dcWSWriter struct {
	ws     *websocket.Conn
	ch     chan []byte
	done   chan struct{}
	mu     sync.Mutex
	closed bool
}

func newDCWSWriter(ws *websocket.Conn) *dcWSWriter {
	w := &dcWSWriter{
		ws:   ws,
		ch:   make(chan []byte, 1024),
		done: make(chan struct{}),
	}
	go w.loop()
	return w
}

func (w *dcWSWriter) loop() {
	defer close(w.done)
	for msg := range w.ch {
		if err := w.ws.WriteMessage(websocket.BinaryMessage, msg); err != nil {
			log.Printf("dc-creator: ws write error: %v", err)
			return
		}
	}
}

func (w *dcWSWriter) send(msg []byte) {
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

func (w *dcWSWriter) close() {
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

type dcCreatorRelay struct {
	wsTunnel *tunnel.WSTunnel
	bridge   *tunnel.RelayBridge
	writerMu sync.Mutex
	writer   *dcWSWriter
}

func startDCCreator(wsPort int, upstreamSocks, upstreamUser, upstreamPass string) error {
	wsTunnel := tunnel.NewWSTunnel()
	bridge := tunnel.NewRelayBridge(wsTunnel, "creator", dcReadBufSize, log.Printf)
	bridge.SetUpstreamSocks(upstreamSocks, upstreamUser, upstreamPass)
	bridge.SetPersistentListener(true)
	c := &dcCreatorRelay{wsTunnel: wsTunnel, bridge: bridge}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", c.handleWS)

	wsAddr := fmt.Sprintf("%s:%d", common.SocksLocalhostIP, wsPort)
	ln, err := net.Listen("tcp", wsAddr)
	if err != nil {
		return fmt.Errorf("dc-creator: ws listen %s: %w", wsAddr, err)
	}
	log.Printf("dc-creator: WebSocket on %s", wsAddr)
	return http.Serve(ln, mux)
}

func (c *dcCreatorRelay) handleWS(w http.ResponseWriter, r *http.Request) {
	ws, err := dcUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("dc-creator: ws upgrade error: %v", err)
		return
	}
	writer := newDCWSWriter(ws)
	c.attachWriter(writer)
	log.Printf("dc-creator: browser connected via WebSocket")
	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			log.Printf("dc-creator: ws read error: %v", err)
			break
		}
		c.wsTunnel.Deliver(msg)
	}
	c.detachWriter(writer)
}

func (c *dcCreatorRelay) attachWriter(writer *dcWSWriter) {
	c.writerMu.Lock()
	previous := c.writer
	c.writer = writer
	c.wsTunnel.SetSendFn(writer.send)
	c.writerMu.Unlock()
	if previous != nil {
		previous.close()
	}
}

func (c *dcCreatorRelay) detachWriter(writer *dcWSWriter) {
	writer.close()
	c.writerMu.Lock()
	defer c.writerMu.Unlock()
	if c.writer != writer {
		return
	}
	c.writer = nil
	c.wsTunnel.SetSendFn(nil)
	c.wsTunnel.NotifyClose()
}
