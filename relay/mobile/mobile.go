package mobile

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"whitelist-bypass/relay/socks"

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

func isClosedConnError(err error) bool {
	return errors.Is(err, net.ErrClosed)
}

func closeWriteConn(conn net.Conn) {
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.CloseWrite()
		return
	}
	_ = conn.Close()
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

var activeJoiner struct {
	sync.Mutex
	j         *joinerRelay
	ws        *http.Server
	socksLn   net.Listener
	wsPort    int
	socksPort int
}

func ActiveWsPort() int    { return activeJoiner.wsPort }
func ActiveSocksPort() int { return activeJoiner.socksPort }

func StopJoiner() {
	activeJoiner.Lock()
	defer activeJoiner.Unlock()
	if activeJoiner.socksLn != nil {
		activeJoiner.socksLn.Close()
		activeJoiner.socksLn = nil
	}
	if activeJoiner.ws != nil {
		activeJoiner.ws.Close()
		activeJoiner.ws = nil
	}
	if activeJoiner.j != nil {
		activeJoiner.j.closeAll()
		activeJoiner.j = nil
	}
	logMsg("dc-joiner: stopped")
}

func listenWithRetry(port int, maxAttempts int) (net.Listener, int, error) {
	for i := 0; i < maxAttempts; i++ {
		addr := fmt.Sprintf("127.0.0.1:%d", port+i)
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			return ln, port + i, nil
		}
	}
	return nil, 0, fmt.Errorf("no free port found starting from %d", port)
}

func StartJoiner(wsPort, socksPort int, cb LogCallback) error {
	StopJoiner()
	logCb = cb
	j := &joinerRelay{
		conns: sync.Map{},
		ready: make(chan struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", j.handleWS)

	wsLn, actualWsPort, err := listenWithRetry(wsPort, 10)
	if err != nil {
		return fmt.Errorf("dc-joiner: %w", err)
	}
	wsSrv := &http.Server{Handler: mux}
	go func() {
		logMsg("dc-joiner: WebSocket on 127.0.0.1:%d", actualWsPort)
		if err := wsSrv.Serve(wsLn); err != nil && err != http.ErrServerClosed {
			logMsg("dc-joiner: ws server error: %v", err)
		}
	}()

	socksLn, actualSocksPort, err := listenWithRetry(socksPort, 10)
	if err != nil {
		wsSrv.Close()
		return fmt.Errorf("dc-joiner: %w", err)
	}
	logMsg("dc-joiner: SOCKS5 on 127.0.0.1:%d", actualSocksPort)

	activeJoiner.Lock()
	activeJoiner.j = j
	activeJoiner.ws = wsSrv
	activeJoiner.socksLn = socksLn
	activeJoiner.wsPort = actualWsPort
	activeJoiner.socksPort = actualSocksPort
	activeJoiner.Unlock()

	return j.listenSOCKS(socksLn)
}

func StartCreator(wsPort int, cb LogCallback) error {
	logCb = cb
	c := &creatorRelay{
		conns: sync.Map{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", c.handleWS)

	ln, actualPort, err := listenWithRetry(wsPort, 10)
	if err != nil {
		return fmt.Errorf("dc-creator: %w", err)
	}
	logMsg("dc-creator: WebSocket on 127.0.0.1:%d", actualPort)
	return http.Serve(ln, mux)
}

type joinerRelay struct {
	writer     *wsWriter
	conns      sync.Map
	udpClients sync.Map
	udpFlows   sync.Map
	nextID     atomic.Uint32
	ready      chan struct{}
	once       sync.Once
}

func (j *joinerRelay) closeAll() {
	j.conns.Range(func(key, val any) bool {
		val.(*socksConn).conn.Close()
		j.conns.Delete(key)
		return true
	})
	if j.writer != nil {
		j.writer.close()
	}
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
	id          uint32
	dstAddr     string
	lastSeen    atomic.Int64
}

type udpSession struct {
	conn net.Conn
	mu   sync.Mutex
}

const udpIdleTimeout = 30 * time.Second

func (j *joinerRelay) handleUDPAssociate(tcpConn net.Conn) {
	udpAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		tcpConn.Write(socks.GenFail)
		tcpConn.Close()
		return
	}
	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		tcpConn.Write(socks.GenFail)
		tcpConn.Close()
		return
	}
	localAddr := udpConn.LocalAddr().(*net.UDPAddr)
	reply := []byte{socks.Ver, 0x00, 0x00, socks.AtypIPv4, 127, 0, 0, 1, 0, 0}
	binary.BigEndian.PutUint16(reply[8:10], uint16(localAddr.Port))
	tcpConn.Write(reply)
	logMsg("dc-joiner: UDP ASSOCIATE on port %d", localAddr.Port)

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
			case socks.AtypIPv4:
				if n < 10 {
					continue
				}
				dstAddr = fmt.Sprintf("%d.%d.%d.%d:%d", buf[4], buf[5], buf[6], buf[7],
					binary.BigEndian.Uint16(buf[8:10]))
				headerLen = 10
			case socks.AtypDomain:
				dlen := int(buf[4])
				if n < 5+dlen+2 {
					continue
				}
				dstAddr = fmt.Sprintf("%s:%d", string(buf[5:5+dlen]),
					binary.BigEndian.Uint16(buf[5+dlen:7+dlen]))
				headerLen = 5 + dlen + 2
			case socks.AtypIPv6:
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
			var uc *udpClient
			if existing, ok := j.udpFlows.Load(dstAddr); ok {
				uc = existing.(*udpClient)
				uc.clientAddr = clientAddr
				uc.socksHeader = append(uc.socksHeader[:0], buf[:headerLen]...)
			} else {
				id := j.nextID.Add(1)
				uc = &udpClient{
					udpConn:     udpConn,
					clientAddr:  clientAddr,
					socksHeader: append([]byte(nil), buf[:headerLen]...),
					id:          id,
					dstAddr:     dstAddr,
				}
				actual, loaded := j.udpFlows.LoadOrStore(dstAddr, uc)
				if loaded {
					uc = actual.(*udpClient)
					uc.clientAddr = clientAddr
					uc.socksHeader = append(uc.socksHeader[:0], buf[:headerLen]...)
				} else {
					j.udpClients.Store(id, uc)
				}
			}
			uc.lastSeen.Store(time.Now().UnixNano())
			payload := make([]byte, len(dstAddr)+1+n-headerLen)
			payload[0] = byte(len(dstAddr))
			copy(payload[1:], dstAddr)
			copy(payload[1+len(dstAddr):], buf[headerLen:n])
			j.send(uc.id, msgUDP, payload)
		}
	}()

	go func() {
		ticker := time.NewTicker(udpIdleTimeout)
		defer ticker.Stop()
		for range ticker.C {
			now := time.Now().UnixNano()
			j.udpClients.Range(func(key, value any) bool {
				uc := value.(*udpClient)
				if now-uc.lastSeen.Load() > int64(udpIdleTimeout) {
					j.udpClients.Delete(key)
					j.udpFlows.Delete(uc.dstAddr)
					logMsg("dc-joiner: UDP session expired conn=%d dst=%s", uc.id, maskAddr(uc.dstAddr))
				}
				return true
			})
		}
	}()
}

func (j *joinerRelay) handleWS(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logMsg("dc-joiner: ws upgrade error: %v", err)
		return
	}
	j.writer = newWSWriter(ws)
	j.once.Do(func() { close(j.ready) })
	logMsg("dc-joiner: browser connected via WebSocket")
	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			logMsg("dc-joiner: ws read error: %v", err)
			return
		}
		connID, msgType, payload := decodeFrame(msg)
		j.handleMessage(connID, msgType, payload)
	}
}

func (j *joinerRelay) handleMessage(connID uint32, msgType byte, payload []byte) {
	if msgType != msgData {
		logMsg("dc-joiner: RX frame conn=%d type=%d payload=%d", connID, msgType, len(payload))
	}
	// UDP replies come with connID from udpClients, not conns — handle first.
	if msgType == msgUDPReply {
		uval, ok := j.udpClients.Load(connID)
		if !ok {
			logMsg("dc-joiner: UDP reply for unknown conn %d", connID)
			return
		}
		uc := uval.(*udpClient)
		reply := make([]byte, len(uc.socksHeader)+len(payload))
		copy(reply, uc.socksHeader)
		copy(reply[len(uc.socksHeader):], payload)
		uc.udpConn.WriteToUDP(reply, uc.clientAddr)
		logMsg("dc-joiner: UDP reply delivered conn=%d bytes=%d", connID, len(payload))
		uc.lastSeen.Store(time.Now().UnixNano())
		return
	}
	val, ok := j.conns.Load(connID)
	if !ok {
		logMsg("dc-joiner: unknown conn %d for type=%d payload=%d", connID, msgType, len(payload))
		return
	}
	sc := val.(*socksConn)
	switch msgType {
	case msgConnectOK:
		sc.rdy <- nil
	case msgConnectErr:
		sc.rdy <- fmt.Errorf("%s", payload)
	case msgData:
		if _, err := sc.conn.Write(payload); err != nil {
			logMsg("dc-joiner: conn %d local write error: %v", connID, err)
		}
	case msgClose:
		closeWriteConn(sc.conn)
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
	if msgType != msgData {
		logMsg("dc-joiner: TX frame conn=%d type=%d payload=%d", connID, msgType, len(payload))
	}
	w.send(buf[:n])
	framePool.Put(bufp)
}

func (j *joinerRelay) listenSOCKS(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go j.handleSOCKS(conn)
	}
}

func (j *joinerRelay) handleSOCKS(conn net.Conn) {
	<-j.ready
	buf := make([]byte, socks.HandshakeBuf)
	n, err := conn.Read(buf)
	if err != nil || n < 2 || buf[0] != socks.Ver {
		conn.Close()
		return
	}
	conn.Write(socks.NoAuth)
	n, err = conn.Read(buf)
	if err != nil || n < 7 || buf[0] != socks.Ver {
		conn.Write(socks.GenFail)
		conn.Close()
		return
	}
	cmd := buf[1]
	if cmd == socks.CmdUDP {
		j.handleUDPAssociate(conn)
		return
	}
	if cmd != socks.CmdTCP {
		conn.Write(socks.CmdErr)
		conn.Close()
		return
	}
	var host string
	switch buf[3] {
	case socks.AtypIPv4:
		if n < 10 {
			conn.Close()
			return
		}
		host = fmt.Sprintf("%d.%d.%d.%d:%d", buf[4], buf[5], buf[6], buf[7],
			binary.BigEndian.Uint16(buf[8:10]))
	case socks.AtypDomain:
		dlen := int(buf[4])
		if n < 5+dlen+2 {
			conn.Close()
			return
		}
		host = fmt.Sprintf("%s:%d", string(buf[5:5+dlen]),
			binary.BigEndian.Uint16(buf[5+dlen:7+dlen]))
	case socks.AtypIPv6:
		if n < 22 {
			conn.Close()
			return
		}
		ip := net.IP(buf[4:20])
		host = fmt.Sprintf("[%s]:%d", ip.String(),
			binary.BigEndian.Uint16(buf[20:22]))
	default:
		conn.Write(socks.AddrErr)
		conn.Close()
		return
	}
	id := j.nextID.Add(1)
	sc := &socksConn{id: id, conn: conn, j: j, rdy: make(chan error, 1)}
	j.conns.Store(id, sc)
	logMsg("dc-joiner: CONNECT %d -> %s", id, maskAddr(host))
	j.send(id, msgConnect, []byte(host))
	if err := <-sc.rdy; err != nil {
		logMsg("dc-joiner: CONNECT %d failed: %v", id, err)
		conn.Write(socks.ConnFail)
		conn.Close()
		j.conns.Delete(id)
		return
	}
	conn.Write(socks.OK)
	logMsg("dc-joiner: CONNECTED %d -> %s", id, maskAddr(host))
	go func() {
		buf := make([]byte, readBufSize)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				j.send(id, msgData, buf[:n])
				if n >= 1024 {
					logMsg("dc-joiner: conn %d local read %d bytes", id, n)
				}
			}
			if err != nil {
				logMsg("dc-joiner: conn %d local read error: %v", id, err)
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
	udps   sync.Map
}

func (c *creatorRelay) handleWS(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logMsg("dc-creator: ws upgrade error: %v", err)
		return
	}
	c.writer = newWSWriter(ws)
	logMsg("dc-creator: browser connected via WebSocket")
	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			logMsg("dc-creator: ws read error: %v", err)
			return
		}
		connID, msgType, payload := decodeFrame(msg)
		c.handleMessage(connID, msgType, payload)
	}
}

func (c *creatorRelay) handleMessage(connID uint32, msgType byte, payload []byte) {
	if msgType != msgData {
		logMsg("dc-creator: RX frame conn=%d type=%d payload=%d", connID, msgType, len(payload))
	}
	switch msgType {
	case msgConnect:
		go c.connect(connID, string(payload))
	case msgUDP:
		c.handleUDP(connID, payload)
	case msgData:
		if val, ok := c.conns.Load(connID); ok {
			if conn, ok := val.(net.Conn); ok {
				if _, err := conn.Write(payload); err != nil {
					logMsg("dc-creator: conn %d downstream write error: %v", connID, err)
				}
			}
		} else {
			logMsg("dc-creator: DATA for unknown conn %d (%d bytes)", connID, len(payload))
		}
	case msgClose:
		if val, ok := c.conns.LoadAndDelete(connID); ok {
			if conn, ok := val.(net.Conn); ok {
				closeWriteConn(conn)
			}
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
	if msgType != msgData {
		logMsg("dc-creator: TX frame conn=%d type=%d payload=%d", connID, msgType, len(payload))
	}
	w.send(buf[:n])
	framePool.Put(bufp)
}

func (c *creatorRelay) handleUDP(connID uint32, payload []byte) {
	if len(payload) < 2 {
		logMsg("dc-creator: UDP short frame conn=%d len=%d", connID, len(payload))
		return
	}
	addrLen := int(payload[0])
	if len(payload) < 1+addrLen {
		return
	}
	addr := string(payload[1 : 1+addrLen])
	data := payload[1+addrLen:]
	logMsg("dc-creator: UDP conn=%d -> %s payload=%d", connID, maskAddr(addr), len(data))
	val, ok := c.udps.Load(connID)
	if !ok {
		udpAddr, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			logMsg("dc-creator: UDP resolve %s failed: %v", maskAddr(addr), err)
			return
		}
		udpConn, err := net.DialUDP("udp", nil, udpAddr)
		if err != nil {
			logMsg("dc-creator: UDP dial %s failed: %v", maskAddr(addr), err)
			return
		}
		sess := &udpSession{conn: udpConn}
		actual, loaded := c.udps.LoadOrStore(connID, sess)
		if loaded {
			udpConn.Close()
			val = actual
		} else {
			val = sess
			go func() {
				buf := make([]byte, socks.UDPBufSize)
				for {
					_ = udpConn.SetReadDeadline(time.Now().Add(udpIdleTimeout))
					n, err := udpConn.Read(buf)
					if n > 0 {
						resp := make([]byte, n)
						copy(resp, buf[:n])
						logMsg("dc-creator: UDP conn=%d reply=%d bytes", connID, n)
						c.send(connID, msgUDPReply, resp)
					}
					if err != nil {
						if ne, ok := err.(net.Error); ok && ne.Timeout() {
							logMsg("dc-creator: UDP conn %d idle timeout", connID)
						} else if !isClosedConnError(err) {
							logMsg("dc-creator: UDP conn %d read error: %v", connID, err)
						}
						break
					}
				}
				c.udps.Delete(connID)
				_ = udpConn.Close()
			}()
		}
	}
	sess := val.(*udpSession)
	sess.mu.Lock()
	_, err := sess.conn.Write(data)
	sess.mu.Unlock()
	if err != nil {
		if !isClosedConnError(err) {
			logMsg("dc-creator: UDP conn %d write error: %v", connID, err)
		}
		c.udps.Delete(connID)
		_ = sess.conn.Close()
	}
}

func (c *creatorRelay) connect(connID uint32, addr string) {
	logMsg("dc-creator: CONNECT %d -> %s", connID, maskAddr(addr))
	conn, err := net.DialTimeout("tcp", addr, 10e9)
	if err != nil {
		logMsg("dc-creator: CONNECT %d failed: %v", connID, err)
		c.send(connID, msgConnectErr, []byte(err.Error()))
		return
	}
	c.conns.Store(connID, conn)
	c.send(connID, msgConnectOK, nil)
	logMsg("dc-creator: CONNECTED %d -> %s", connID, maskAddr(addr))
	buf := make([]byte, readBufSize)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			c.send(connID, msgData, buf[:n])
			if n >= 1024 {
				logMsg("dc-creator: conn %d upstream read %d bytes", connID, n)
			}
		}
		if err != nil {
			if err != io.EOF && !isClosedConnError(err) {
				logMsg("dc-creator: conn %d read error: %v", connID, err)
			}
			break
		}
	}
	c.send(connID, msgClose, nil)
	c.conns.Delete(connID)
}
