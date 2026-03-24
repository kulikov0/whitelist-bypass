package mobile

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	msgConnect    byte = 0x01
	msgConnectOK  byte = 0x02
	msgConnectErr byte = 0x03
	msgData       byte = 0x04
	msgClose      byte = 0x05
	msgUDP        byte = 0x06
	msgUDPReply   byte = 0x07
)

const readBufSize = 65536


var framePool = sync.Pool{
	New: func() any {
		buf := make([]byte, 5+readBufSize)
		return &buf
	},
}

func encodeFrameInto(buf []byte, connID uint32, msgType byte, payload []byte) int {
	binary.BigEndian.PutUint32(buf[0:4], connID)
	buf[4] = msgType
	copy(buf[5:], payload)
	return 5 + len(payload)
}

func decodeFrame(data []byte) (connID uint32, msgType byte, payload []byte) {
	if len(data) < 5 {
		return 0, 0, nil
	}
	connID = binary.BigEndian.Uint32(data[0:4])
	msgType = data[4]
	payload = data[5:]
	return
}

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
	ws   *websocket.Conn
	ch   chan []byte
	done chan struct{}
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
	cp := make([]byte, len(msg))
	copy(cp, msg)
	select {
	case w.ch <- cp:
	default:
	}
}

func (w *wsWriter) close() {
	close(w.ch)
	<-w.done
}


func StartJoiner(wsPort, socksPort int, cb LogCallback) error {
	logCb = cb
	j := &joinerRelay{
		conns: sync.Map{},
		ready: make(chan struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", j.handleWS)

	go func() {
		addr := fmt.Sprintf("127.0.0.1:%d", wsPort)
		logMsg("joiner: WebSocket on %s", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			logMsg("joiner: ws server error: %v", err)
		}
	}()
	addr := fmt.Sprintf("127.0.0.1:%d", socksPort)
	logMsg("joiner: SOCKS5 on %s", addr)
	return j.listenSOCKS(addr)
}

func StartCreator(wsPort int, cb LogCallback) error {
	logCb = cb
	c := &creatorRelay{
		conns: sync.Map{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", c.handleWS)

	addr := fmt.Sprintf("127.0.0.1:%d", wsPort)
	logMsg("creator: WebSocket on %s", addr)
	return http.ListenAndServe(addr, mux)
}

type joinerRelay struct {
	writer     *wsWriter
	conns      sync.Map
	udpClients sync.Map
	nextID     atomic.Uint32
	ready      chan struct{}
	once       sync.Once
}

type socksConn struct {
	id   uint32
	conn net.Conn
	j    *joinerRelay
	rdy  chan error
}

type udpClient struct {
	udpConn     *net.UDPConn
	clientAddr  *net.UDPAddr
	socksHeader []byte
}

func (j *joinerRelay) handleUDPAssociate(tcpConn net.Conn) {
	udpAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		tcpConn.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		tcpConn.Close()
		return
	}
	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		tcpConn.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		tcpConn.Close()
		return
	}
	localAddr := udpConn.LocalAddr().(*net.UDPAddr)
	reply := []byte{0x05, 0x00, 0x00, 0x01, 127, 0, 0, 1, 0, 0}
	binary.BigEndian.PutUint16(reply[8:10], uint16(localAddr.Port))
	tcpConn.Write(reply)
	logMsg("joiner: UDP ASSOCIATE on port %d", localAddr.Port)

	go func() {
		buf := make([]byte, 1)
		tcpConn.Read(buf)
		udpConn.Close()
	}()

	go func() {
		defer udpConn.Close()
		defer tcpConn.Close()
		buf := make([]byte, readBufSize)
		var clientAddr *net.UDPAddr
		for {
			n, addr, err := udpConn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if n < 10 {
				continue
			}
			clientAddr = addr
			frag := buf[2]
			if frag != 0 {
				continue
			}
			atyp := buf[3]
			var dstAddr string
			var headerLen int
			switch atyp {
			case 0x01:
				if n < 10 {
					continue
				}
				dstAddr = fmt.Sprintf("%d.%d.%d.%d:%d", buf[4], buf[5], buf[6], buf[7],
					binary.BigEndian.Uint16(buf[8:10]))
				headerLen = 10
			case 0x03:
				dlen := int(buf[4])
				if n < 5+dlen+2 {
					continue
				}
				dstAddr = fmt.Sprintf("%s:%d", string(buf[5:5+dlen]),
					binary.BigEndian.Uint16(buf[5+dlen:7+dlen]))
				headerLen = 5 + dlen + 2
			case 0x04:
				if n < 22 {
					continue
				}
				ip := net.IP(buf[4:20])
				dstAddr = fmt.Sprintf("[%s]:%d", ip.String(),
					binary.BigEndian.Uint16(buf[20:22]))
				headerLen = 22
			default:
				continue
			}
			id := j.nextID.Add(1)
			payload := make([]byte, len(dstAddr)+1+n-headerLen)
			payload[0] = byte(len(dstAddr))
			copy(payload[1:], dstAddr)
			copy(payload[1+len(dstAddr):], buf[headerLen:n])
			j.udpClients.Store(id, &udpClient{udpConn: udpConn, clientAddr: clientAddr, socksHeader: buf[:headerLen]})
			j.send(id, msgUDP, payload)
		}
	}()
}

func (j *joinerRelay) handleWS(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logMsg("joiner: ws upgrade error: %v", err)
		return
	}
	j.writer = newWSWriter(ws)
	j.once.Do(func() { close(j.ready) })
	logMsg("joiner: browser connected via WebSocket")
	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			logMsg("joiner: ws read error: %v", err)
			return
		}
		connID, msgType, payload := decodeFrame(msg)
		j.handleMessage(connID, msgType, payload)
	}
}

func (j *joinerRelay) handleMessage(connID uint32, msgType byte, payload []byte) {
	val, ok := j.conns.Load(connID)
	if !ok {
		return
	}
	if msgType == msgUDPReply {
		uval, ok := j.udpClients.Load(connID)
		if !ok {
			return
		}
		uc := uval.(*udpClient)
		reply := make([]byte, len(uc.socksHeader)+len(payload))
		copy(reply, uc.socksHeader)
		copy(reply[len(uc.socksHeader):], payload)
		uc.udpConn.WriteToUDP(reply, uc.clientAddr)
		j.udpClients.Delete(connID)
		return
	}
	sc := val.(*socksConn)
	switch msgType {
	case msgConnectOK:
		sc.rdy <- nil
	case msgConnectErr:
		sc.rdy <- fmt.Errorf("%s", payload)
	case msgData:
		sc.conn.Write(payload)
	case msgClose:
		sc.conn.Close()
		j.conns.Delete(connID)
	}
}

func (j *joinerRelay) send(connID uint32, msgType byte, payload []byte) {
	w := j.writer
	if w == nil {
		return
	}
	bufp := framePool.Get().(*[]byte)
	buf := *bufp
	n := encodeFrameInto(buf, connID, msgType, payload)
	w.send(buf[:n])
	framePool.Put(bufp)
}

func (j *joinerRelay) listenSOCKS(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			logMsg("joiner: accept error: %v", err)
			continue
		}
		go j.handleSOCKS(conn)
	}
}

func (j *joinerRelay) handleSOCKS(conn net.Conn) {
	<-j.ready
	buf := make([]byte, 258)
	n, err := conn.Read(buf)
	if err != nil || n < 2 || buf[0] != 0x05 {
		conn.Close()
		return
	}
	conn.Write([]byte{0x05, 0x00})
	n, err = conn.Read(buf)
	if err != nil || n < 7 || buf[0] != 0x05 {
		conn.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		conn.Close()
		return
	}
	cmd := buf[1]
	if cmd == 0x03 {
		j.handleUDPAssociate(conn)
		return
	}
	if cmd != 0x01 {
		conn.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		conn.Close()
		return
	}
	var host string
	switch buf[3] {
	case 0x01:
		if n < 10 {
			conn.Close()
			return
		}
		host = fmt.Sprintf("%d.%d.%d.%d:%d", buf[4], buf[5], buf[6], buf[7],
			binary.BigEndian.Uint16(buf[8:10]))
	case 0x03:
		dlen := int(buf[4])
		if n < 5+dlen+2 {
			conn.Close()
			return
		}
		host = fmt.Sprintf("%s:%d", string(buf[5:5+dlen]),
			binary.BigEndian.Uint16(buf[5+dlen:7+dlen]))
	case 0x04:
		if n < 22 {
			conn.Close()
			return
		}
		ip := net.IP(buf[4:20])
		host = fmt.Sprintf("[%s]:%d", ip.String(),
			binary.BigEndian.Uint16(buf[20:22]))
	default:
		conn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		conn.Close()
		return
	}
	id := j.nextID.Add(1)
	sc := &socksConn{id: id, conn: conn, j: j, rdy: make(chan error, 1)}
	j.conns.Store(id, sc)
	logMsg("joiner: CONNECT %d -> %s", id, maskAddr(host))
	j.send(id, msgConnect, []byte(host))
	if err := <-sc.rdy; err != nil {
		logMsg("joiner: CONNECT %d failed: %v", id, err)
		conn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		conn.Close()
		j.conns.Delete(id)
		return
	}
	conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	logMsg("joiner: CONNECTED %d -> %s", id, maskAddr(host))
	go func() {
		buf := make([]byte, readBufSize)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				j.send(id, msgData, buf[:n])
			}
			if err != nil {
				j.send(id, msgClose, nil)
				j.conns.Delete(id)
				return
			}
		}
	}()
}

type creatorRelay struct {
	writer *wsWriter
	conns  sync.Map
}

func (c *creatorRelay) handleWS(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logMsg("creator: ws upgrade error: %v", err)
		return
	}
	c.writer = newWSWriter(ws)
	logMsg("creator: browser connected via WebSocket")
	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			logMsg("creator: ws read error: %v", err)
			return
		}
		connID, msgType, payload := decodeFrame(msg)
		c.handleMessage(connID, msgType, payload)
	}
}

func (c *creatorRelay) handleMessage(connID uint32, msgType byte, payload []byte) {
	switch msgType {
	case msgConnect:
		go c.connect(connID, string(payload))
	case msgUDP:
		go c.handleUDP(connID, payload)
	case msgData:
		val, ok := c.conns.Load(connID)
		if ok {
			val.(net.Conn).Write(payload)
		}
	case msgClose:
		val, ok := c.conns.LoadAndDelete(connID)
		if ok {
			val.(net.Conn).Close()
		}
	}
}

func (c *creatorRelay) send(connID uint32, msgType byte, payload []byte) {
	w := c.writer
	if w == nil {
		return
	}
	bufp := framePool.Get().(*[]byte)
	buf := *bufp
	n := encodeFrameInto(buf, connID, msgType, payload)
	w.send(buf[:n])
	framePool.Put(bufp)
}

func (c *creatorRelay) handleUDP(connID uint32, payload []byte) {
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
		logMsg("creator: UDP resolve %s failed: %v", maskAddr(addr), err)
		return
	}
	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		logMsg("creator: UDP dial %s failed: %v", maskAddr(addr), err)
		return
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	_, err = conn.Write(data)
	if err != nil {
		return
	}
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return
	}
	c.send(connID, msgUDPReply, buf[:n])
}

func (c *creatorRelay) connect(connID uint32, addr string) {
	logMsg("creator: CONNECT %d -> %s", connID, maskAddr(addr))
	conn, err := net.DialTimeout("tcp", addr, 10e9)
	if err != nil {
		logMsg("creator: CONNECT %d failed: %v", connID, err)
		c.send(connID, msgConnectErr, []byte(err.Error()))
		return
	}
	c.conns.Store(connID, conn)
	c.send(connID, msgConnectOK, nil)
	logMsg("creator: CONNECTED %d -> %s", connID, maskAddr(addr))
	buf := make([]byte, readBufSize)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			c.send(connID, msgData, buf[:n])
		}
		if err != nil {
			if err != io.EOF {
				logMsg("creator: conn %d read error: %v", connID, err)
			}
			break
		}
	}
	c.send(connID, msgClose, nil)
	c.conns.Delete(connID)
}
