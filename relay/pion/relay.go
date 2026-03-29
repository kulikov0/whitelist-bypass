package pion

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
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
}

type RelayBridge struct {
	tunnel     *VP8DataTunnel
	conns      sync.Map
	udpClients sync.Map
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
		rb.udpClients.Delete(connID)
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
		sc.conn.Write(payload)
	case msgClose:
		sc.conn.Close()
		rb.conns.Delete(connID)
	}
}

// Creator: receives Connect/Data/Close from joiner
func (rb *RelayBridge) handleCreatorMessage(connID uint32, msgType byte, payload []byte) {
	switch msgType {
	case msgConnect:
		go rb.connectTCP(connID, string(payload))
	case msgUDP:
		go rb.handleUDP(connID, payload)
	case msgData:
		val, ok := rb.conns.Load(connID)
		if ok {
			val.(net.Conn).Write(payload)
		}
	case msgClose:
		val, ok := rb.conns.LoadAndDelete(connID)
		if ok {
			val.(net.Conn).Close()
		}
	}
}

func (rb *RelayBridge) handleUDP(connID uint32, payload []byte) {
	if len(payload) < 2 {
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
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return
	}
	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	conn.Write(data)
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return
	}
	rb.send(connID, msgUDPReply, buf[:n])
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

	buf := make([]byte, 900)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			rb.send(connID, msgData, buf[:n])
		}
		if err != nil {
			if err != io.EOF {
				rb.logFn("relay: conn %d read error: %v", connID, err)
			}
			break
		}
	}
	rb.send(connID, msgClose, nil)
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
	buf := make([]byte, 258)
	n, err := conn.Read(buf)
	if err != nil || n < 2 || buf[0] != 0x05 {
		conn.Close()
		return
	}
	conn.Write([]byte{0x05, 0x00})
	n, err = conn.Read(buf)
	if err != nil || n < 7 || buf[0] != 0x05 {
		conn.Close()
		return
	}
	cmd := buf[1]
	if cmd == 0x03 {
		rb.handleUDPAssociate(conn)
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

	id := rb.nextID.Add(1)
	sc := &socksConn{id: id, conn: conn, rb: rb, rdy: make(chan error, 1)}
	rb.conns.Store(id, sc)
	rb.logFn("relay: SOCKS CONNECT %d -> %s", id, maskAddr(host))
	rb.send(id, msgConnect, []byte(host))

	if err := <-sc.rdy; err != nil {
		rb.logFn("relay: SOCKS CONNECT %d failed: %v", id, err)
		conn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		conn.Close()
		rb.conns.Delete(id)
		return
	}
	conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	rb.logFn("relay: SOCKS CONNECTED %d -> %s", id, maskAddr(host))

	go func() {
		readBuf := make([]byte, 900)
		for {
			rn, rerr := conn.Read(readBuf)
			if rn > 0 {
				rb.send(id, msgData, readBuf[:rn])
			}
			if rerr != nil {
				rb.send(id, msgClose, nil)
				rb.conns.Delete(id)
				return
			}
		}
	}()
}

func (rb *RelayBridge) handleUDPAssociate(tcpConn net.Conn) {
	udpAddr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:0")
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

	go func() {
		buf := make([]byte, 1)
		tcpConn.Read(buf)
		udpConn.Close()
	}()

	go func() {
		defer udpConn.Close()
		defer tcpConn.Close()
		buf := make([]byte, 4096)
		for {
			n, addr, err := udpConn.ReadFromUDP(buf)
			if err != nil {
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
			id := rb.nextID.Add(1)
			payload := make([]byte, len(dstAddr)+1+n-headerLen)
			payload[0] = byte(len(dstAddr))
			copy(payload[1:], dstAddr)
			copy(payload[1+len(dstAddr):], buf[headerLen:n])
			rb.udpClients.Store(id, &udpClient{udpConn: udpConn, clientAddr: addr, socksHdr: buf[:headerLen]})
			rb.send(id, msgUDP, payload)
		}
	}()
}
