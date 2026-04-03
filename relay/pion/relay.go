package pion

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"whitelist-bypass/relay/socks"
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

func encodeFrame(connID uint32, msgType byte, payload []byte) []byte {
	buf := make([]byte, 4+5+len(payload))
	binary.BigEndian.PutUint32(buf[0:4], uint32(5+len(payload)))
	binary.BigEndian.PutUint32(buf[4:8], connID)
	buf[8] = msgType
	copy(buf[9:], payload)
	return buf
}

func decodeFrames(data []byte, cb func(connID uint32, msgType byte, payload []byte)) {
	for len(data) >= 4 {
		frameLen := int(binary.BigEndian.Uint32(data[0:4]))
		if frameLen < 5 || 4+frameLen > len(data) {
			return
		}
		connID := binary.BigEndian.Uint32(data[4:8])
		msgType := data[8]
		payload := data[9 : 4+frameLen]
		cb(connID, msgType, payload)
		data = data[4+frameLen:]
	}
}

type udpClient struct {
	udpConn    *net.UDPConn
	clientAddr *net.UDPAddr
	socksHdr   []byte
	id         uint32
	dstAddr    string
	lastSeen   atomic.Int64
}

type udpSession struct {
	conn net.Conn
	mu   sync.Mutex
}

const udpIdleTimeout = 30 * time.Second

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

type RelayBridge struct {
	tunnel     *VP8DataTunnel
	conns      sync.Map
	udpClients sync.Map
	udpFlows   sync.Map
	udpConns   sync.Map
	nextID     atomic.Uint32
	logFn      func(string, ...any)
	mode       string
	ready      chan struct{}
	once       sync.Once
}

func NewRelayBridge(tunnel *VP8DataTunnel, mode string, logFn func(string, ...any)) *RelayBridge {
	rb := &RelayBridge{
		tunnel: tunnel,
		logFn:  logFn,
		mode:   mode,
		ready:  make(chan struct{}),
	}
	tunnel.onData = rb.handleTunnelData
	tunnel.onClose = rb.closeAll
	return rb
}

func (rb *RelayBridge) closeAll() {
	rb.logFn("relay: closing all connections")
	rb.conns.Range(func(key, value any) bool {
		switch v := value.(type) {
		case net.Conn:
			v.Close()
		case *socksConn:
			v.conn.Close()
		}
		rb.conns.Delete(key)
		return true
	})
	rb.udpConns.Range(func(key, value any) bool {
		if s, ok := value.(*udpSession); ok {
			_ = s.conn.Close()
		}
		rb.udpConns.Delete(key)
		return true
	})
}

func (rb *RelayBridge) MarkReady() {
	rb.once.Do(func() { close(rb.ready) })
}

func (rb *RelayBridge) send(connID uint32, msgType byte, payload []byte) {
	frame := encodeFrame(connID, msgType, payload)
	rb.tunnel.SendData(frame)
}

func (rb *RelayBridge) handleTunnelData(data []byte) {
	decodeFrames(data, func(connID uint32, msgType byte, payload []byte) {
		switch rb.mode {
		case "joiner":
			rb.handleJoinerMessage(connID, msgType, payload)
		case "creator":
			rb.handleCreatorMessage(connID, msgType, payload)
		}
	})
}

// Joiner: receives ConnectOK/Data/Close from creator
func (rb *RelayBridge) handleJoinerMessage(connID uint32, msgType byte, payload []byte) {
	if msgType != msgData {
		rb.logFn("relay: joiner RX frame conn=%d type=%d payload=%d", connID, msgType, len(payload))
	}
	if msgType == msgUDPReply {
		uval, ok := rb.udpClients.Load(connID)
		if !ok {
			return
		}
		uc := uval.(*udpClient)
		reply := make([]byte, len(uc.socksHdr)+len(payload))
		copy(reply, uc.socksHdr)
		copy(reply[len(uc.socksHdr):], payload)
		uc.udpConn.WriteToUDP(reply, uc.clientAddr)
		rb.logFn("relay: UDP reply delivered conn=%d bytes=%d", connID, len(payload))
		uc.lastSeen.Store(time.Now().UnixNano())
		return
	}
	val, ok := rb.conns.Load(connID)
	if !ok {
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
			rb.logFn("relay: joiner conn %d local write error: %v", connID, err)
		}
	case msgClose:
		closeWriteConn(sc.conn)
		rb.conns.Delete(connID)
	}
}

// Creator: receives Connect/Data/Close from joiner
func (rb *RelayBridge) handleCreatorMessage(connID uint32, msgType byte, payload []byte) {
	if msgType != msgData {
		rb.logFn("relay: creator RX frame conn=%d type=%d payload=%d", connID, msgType, len(payload))
	}
	switch msgType {
	case msgConnect:
		go rb.connectTCP(connID, string(payload))
	case msgUDP:
		rb.handleUDP(connID, payload)
	case msgData:
		if val, ok := rb.conns.Load(connID); ok {
			if c, ok := val.(net.Conn); ok {
				if _, err := c.Write(payload); err != nil {
					rb.logFn("relay: creator conn %d downstream write error: %v", connID, err)
				}
			}
		} else {
			rb.logFn("relay: creator DATA for unknown conn %d (%d bytes)", connID, len(payload))
		}
	case msgClose:
		if val, ok := rb.conns.LoadAndDelete(connID); ok {
			if c, ok := val.(net.Conn); ok {
				closeWriteConn(c)
			}
		}
	}
}

func (rb *RelayBridge) handleUDP(connID uint32, payload []byte) {
	if len(payload) < 2 {
		rb.logFn("relay: UDP short frame conn=%d len=%d", connID, len(payload))
		return
	}
	addrLen := int(payload[0])
	if addrLen == 0 || len(payload) < 1+addrLen {
		return
	}
	if bytes.IndexByte(payload[1:1+addrLen], 0) != -1 {
		return
	}
	addr := string(payload[1 : 1+addrLen])
	data := payload[1+addrLen:]
	rb.logFn("relay: UDP conn=%d -> %s payload=%d", connID, maskAddr(addr), len(data))
	val, ok := rb.udpConns.Load(connID)
	if !ok {
		udpAddr, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			rb.logFn("relay: UDP resolve failed conn=%d addr=%s: %v", connID, maskAddr(addr), err)
			return
		}
		udpConn, err := net.DialUDP("udp", nil, udpAddr)
		if err != nil {
			rb.logFn("relay: UDP dial failed conn=%d addr=%s: %v", connID, maskAddr(addr), err)
			return
		}
		sess := &udpSession{conn: udpConn}
		actual, loaded := rb.udpConns.LoadOrStore(connID, sess)
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
						rb.logFn("relay: UDP conn=%d reply=%d bytes", connID, n)
						rb.send(connID, msgUDPReply, resp)
					}
					if err != nil {
						if ne, ok := err.(net.Error); ok && ne.Timeout() {
							rb.logFn("relay: UDP conn %d idle timeout", connID)
						} else if !isClosedConnError(err) {
							rb.logFn("relay: UDP conn %d read error: %v", connID, err)
						}
						break
					}
				}
				rb.udpConns.Delete(connID)
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
			rb.logFn("relay: UDP conn %d write error: %v", connID, err)
		}
		rb.udpConns.Delete(connID)
		_ = sess.conn.Close()
	}
}

func (rb *RelayBridge) connectTCP(connID uint32, addr string) {
	rb.logFn("relay: CONNECT %d -> %s", connID, maskAddr(addr))
	conn, err := net.DialTimeout("tcp", addr, 10e9)
	if err != nil {
		rb.logFn("relay: CONNECT %d failed: %v", connID, err)
		rb.send(connID, msgConnectErr, []byte(err.Error()))
		return
	}
	rb.conns.Store(connID, conn)
	rb.send(connID, msgConnectOK, nil)
	rb.logFn("relay: CONNECTED %d -> %s", connID, maskAddr(addr))

	buf := make([]byte, socks.VP8BufSize)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			rb.send(connID, msgData, buf[:n])
			if n >= 1024 {
				rb.logFn("relay: conn %d upstream read %d bytes", connID, n)
			}
		}
		if err != nil {
			if err != io.EOF && !isClosedConnError(err) {
				rb.logFn("relay: conn %d read error: %v", connID, err)
			}
			break
		}
	}
	rb.send(connID, msgClose, nil)
	rb.logFn("relay: conn %d cleanup delete", connID)
	rb.conns.Delete(connID)
}

// SOCKS5 proxy for joiner mode
type socksConn struct {
	id   uint32
	conn net.Conn
	rb   *RelayBridge
	rdy  chan error
}

func (rb *RelayBridge) ListenSOCKS(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	rb.logFn("relay: SOCKS5 on %s", addr)
	for {
		conn, err := ln.Accept()
		if err != nil {
			rb.logFn("relay: accept error: %v", err)
			continue
		}
		go rb.handleSOCKS(conn)
	}
}

func (rb *RelayBridge) handleSOCKS(conn net.Conn) {
	<-rb.ready
	buf := make([]byte, socks.HandshakeBuf)
	n, err := conn.Read(buf)
	if err != nil || n < 2 || buf[0] != socks.Ver {
		conn.Close()
		return
	}
	conn.Write(socks.NoAuth)
	n, err = conn.Read(buf)
	if err != nil || n < 7 || buf[0] != socks.Ver {
		conn.Close()
		return
	}
	cmd := buf[1]
	if cmd == socks.CmdUDP {
		rb.handleUDPAssociate(conn)
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

	id := rb.nextID.Add(1)
	sc := &socksConn{id: id, conn: conn, rb: rb, rdy: make(chan error, 1)}
	rb.conns.Store(id, sc)
	rb.logFn("relay: SOCKS CONNECT %d -> %s", id, maskAddr(host))
	rb.send(id, msgConnect, []byte(host))

	if err := <-sc.rdy; err != nil {
		rb.logFn("relay: SOCKS CONNECT %d failed: %v", id, err)
		conn.Write(socks.ConnFail)
		conn.Close()
		rb.conns.Delete(id)
		return
	}
	conn.Write(socks.OK)
	rb.logFn("relay: SOCKS CONNECTED %d -> %s", id, maskAddr(host))

	go func() {
		readBuf := make([]byte, socks.VP8BufSize)
		for {
			rn, rerr := conn.Read(readBuf)
			if rn > 0 {
				rb.send(id, msgData, readBuf[:rn])
				if rn >= 1024 {
					rb.logFn("relay: SOCKS conn %d local read %d bytes", id, rn)
				}
			}
			if rerr != nil {
				rb.logFn("relay: SOCKS conn %d local read error: %v", id, rerr)
				rb.send(id, msgClose, nil)
				rb.conns.Delete(id)
				return
			}
		}
	}()
}

func (rb *RelayBridge) handleUDPAssociate(tcpConn net.Conn) {
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

	go func() {
		buf := make([]byte, 1)
		tcpConn.Read(buf)
		udpConn.Close()
	}()

	go func() {
		defer udpConn.Close()
		defer tcpConn.Close()
		buf := make([]byte, socks.UDPBufSize)
		for {
			n, addr, err := udpConn.ReadFromUDP(buf)
			if err != nil {
				rb.logFn("relay: UDP ASSOC read error: %v", err)
				return
			}
			if n < 10 {
				continue
			}
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
			if existing, ok := rb.udpFlows.Load(dstAddr); ok {
				uc = existing.(*udpClient)
				uc.clientAddr = addr
				uc.socksHdr = append(uc.socksHdr[:0], buf[:headerLen]...)
			} else {
				id := rb.nextID.Add(1)
				uc = &udpClient{
					udpConn:    udpConn,
					clientAddr: addr,
					socksHdr:   append([]byte(nil), buf[:headerLen]...),
					id:         id,
					dstAddr:    dstAddr,
				}
				actual, loaded := rb.udpFlows.LoadOrStore(dstAddr, uc)
				if loaded {
					uc = actual.(*udpClient)
					uc.clientAddr = addr
					uc.socksHdr = append(uc.socksHdr[:0], buf[:headerLen]...)
				} else {
					rb.udpClients.Store(id, uc)
				}
			}
			uc.lastSeen.Store(time.Now().UnixNano())
			payload := make([]byte, len(dstAddr)+1+n-headerLen)
			payload[0] = byte(len(dstAddr))
			copy(payload[1:], dstAddr)
			copy(payload[1+len(dstAddr):], buf[headerLen:n])
			rb.logFn("relay: UDP ASSOC conn=%d dst=%s payload=%d", uc.id, maskAddr(dstAddr), n-headerLen)
			rb.send(uc.id, msgUDP, payload)
		}
	}()

	go func() {
		ticker := time.NewTicker(udpIdleTimeout)
		defer ticker.Stop()
		for range ticker.C {
			now := time.Now().UnixNano()
			rb.udpClients.Range(func(key, value any) bool {
				uc := value.(*udpClient)
				if now-uc.lastSeen.Load() > int64(udpIdleTimeout) {
					rb.udpClients.Delete(key)
					rb.udpFlows.Delete(uc.dstAddr)
					rb.logFn("relay: UDP ASSOC conn=%d expired dst=%s", uc.id, maskAddr(uc.dstAddr))
				}
				return true
			})
		}
	}()
}
