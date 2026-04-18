package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"whitelist-bypass/relay/common"
)

const (
	dcMsgConnect    byte = 0x01
	dcMsgConnectOK  byte = 0x02
	dcMsgConnectErr byte = 0x03
	dcMsgData       byte = 0x04
	dcMsgClose      byte = 0x05
	dcMsgUDP        byte = 0x06
	dcMsgUDPReply   byte = 0x07
)

const dcReadBufSize = 65536

var dcFramePool = sync.Pool{
	New: func() any {
		buf := make([]byte, 5+dcReadBufSize)
		return &buf
	},
}

var dcUpgrader = websocket.Upgrader{
	CheckOrigin:     func(r *http.Request) bool { return true },
	ReadBufferSize:  dcReadBufSize,
	WriteBufferSize: dcReadBufSize,
}

type dcWSWriter struct {
	ws   *websocket.Conn
	ch   chan []byte
	done chan struct{}
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
	cp := make([]byte, len(msg))
	copy(cp, msg)
	select {
	case w.ch <- cp:
	default:
	}
}

func (w *dcWSWriter) close() {
	close(w.ch)
	<-w.done
}

type dcCreatorRelay struct {
	writer *dcWSWriter
	conns  sync.Map
}

func startDCCreator(wsPort int) error {
	c := &dcCreatorRelay{conns: sync.Map{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", c.handleWS)

	wsAddr := fmt.Sprintf("127.0.0.1:%d", wsPort)
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
	c.writer = newDCWSWriter(ws)
	log.Printf("dc-creator: browser connected via WebSocket")
	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			log.Printf("dc-creator: ws read error: %v", err)
			return
		}
		if len(msg) < 5 {
			continue
		}
		connID := binary.BigEndian.Uint32(msg[0:4])
		msgType := msg[4]
		payload := msg[5:]
		c.handleMessage(connID, msgType, payload)
	}
}

func (c *dcCreatorRelay) handleMessage(connID uint32, msgType byte, payload []byte) {
	switch msgType {
	case dcMsgConnect:
		go c.connect(connID, string(payload))
	case dcMsgUDP:
		go c.handleUDP(connID, payload)
	case dcMsgData:
		if val, ok := c.conns.Load(connID); ok {
			if conn, ok := val.(net.Conn); ok {
				conn.Write(payload)
			}
		}
	case dcMsgClose:
		if val, ok := c.conns.LoadAndDelete(connID); ok {
			if conn, ok := val.(net.Conn); ok {
				conn.Close()
			}
		}
	}
}

func (c *dcCreatorRelay) send(connID uint32, msgType byte, payload []byte) {
	w := c.writer
	if w == nil {
		return
	}
	bufp := dcFramePool.Get().(*[]byte)
	buf := *bufp
	binary.BigEndian.PutUint32(buf[0:4], connID)
	buf[4] = msgType
	copy(buf[5:], payload)
	n := 5 + len(payload)
	w.send(buf[:n])
	dcFramePool.Put(bufp)
}

func (c *dcCreatorRelay) handleUDP(connID uint32, payload []byte) {
	if len(payload) < 2 {
		return
	}
	addrLen := int(payload[0])
	if len(payload) < 1+addrLen {
		return
	}
	addr := string(payload[1 : 1+addrLen])
	data := payload[1+addrLen:]

	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		log.Printf("dc-creator: UDP resolve %s failed: %s", common.MaskAddr(addr), common.MaskError(err))
		return
	}
	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		log.Printf("dc-creator: UDP dial %s failed: %s", common.MaskAddr(addr), common.MaskError(err))
		return
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	conn.Write(data)
	buf := make([]byte, common.UDPBufSize)
	n, err := conn.Read(buf)
	if err != nil {
		return
	}
	c.send(connID, dcMsgUDPReply, buf[:n])
}

func (c *dcCreatorRelay) connect(connID uint32, addr string) {
	log.Printf("dc-creator: CONNECT %d -> %s", connID, common.MaskAddr(addr))
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		log.Printf("dc-creator: CONNECT %d failed: %s", connID, common.MaskError(err))
		c.send(connID, dcMsgConnectErr, []byte(common.MaskError(err)))
		return
	}
	c.conns.Store(connID, conn)
	c.send(connID, dcMsgConnectOK, nil)
	log.Printf("dc-creator: CONNECTED %d -> %s", connID, common.MaskAddr(addr))
	buf := make([]byte, dcReadBufSize)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			c.send(connID, dcMsgData, buf[:n])
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("dc-creator: conn %d read error: %s", connID, common.MaskError(err))
			}
			break
		}
	}
	c.send(connID, dcMsgClose, nil)
	c.conns.Delete(connID)
}
