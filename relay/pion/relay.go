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
		if val, ok := rb.conns.Load(connID); ok {
			if c, ok := val.(net.Conn); ok {
				c.Write(payload)
			}
		}
	case msgClose:
		if val, ok := rb.conns.LoadAndDelete(connID); ok {
			if c, ok := val.(net.Conn); ok {
				c.Close()
			}
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
	buf := make([]byte, socks.UDPBufSize)
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

	buf := make([]byte, socks.VP8BufSize)
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
	host, _, err := socks.ParseAddress(buf, n)
	if err != nil {
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
				return
			}
			if n < 10 {
				continue
			}
			frag := buf[2]
			if frag != 0 {
				continue
			}
			dstAddr, headerLen, addrErr := socks.ParseAddress(buf, n)
			if addrErr != nil {
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
