package main

import (
	"encoding/binary"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
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

type RelayBridge struct {
	tunnel *VP8DataTunnel
	conns  sync.Map
	nextID atomic.Uint32
	logFn  func(string, ...any)
}

func NewRelayBridge(tunnel *VP8DataTunnel, mode string, logFn func(string, ...any)) *RelayBridge {
	rb := &RelayBridge{
		tunnel: tunnel,
		logFn:  logFn,
	}
	tunnel.onData = rb.handleTunnelData
	tunnel.onClose = rb.closeAll
	return rb
}

func (rb *RelayBridge) closeAll() {
	rb.logFn("relay: closing all connections")
	rb.conns.Range(func(key, value any) bool {
		if c, ok := value.(net.Conn); ok {
			c.Close()
		}
		rb.conns.Delete(key)
		return true
	})
}

func (rb *RelayBridge) send(connID uint32, msgType byte, payload []byte) {
	frame := encodeFrame(connID, msgType, payload)
	rb.tunnel.SendData(frame)
}

func (rb *RelayBridge) handleTunnelData(data []byte) {
	decodeFrames(data, rb.handleCreatorMessage)
}

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
	buf := make([]byte, udpBufSize)
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

	buf := make([]byte, vp8RelayBufSize)
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
