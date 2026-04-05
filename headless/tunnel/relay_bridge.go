package tunnel

import (
	"encoding/binary"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

func EncodeFrame(connID uint32, msgType byte, payload []byte) []byte {
	buf := make([]byte, 4+5+len(payload))
	binary.BigEndian.PutUint32(buf[0:4], uint32(5+len(payload)))
	binary.BigEndian.PutUint32(buf[4:8], connID)
	buf[8] = msgType
	copy(buf[9:], payload)
	return buf
}

func DecodeFrames(data []byte, cb func(connID uint32, msgType byte, payload []byte)) {
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

func NewRelayBridge(tun *VP8DataTunnel, mode string, logFn func(string, ...any)) *RelayBridge {
	rb := &RelayBridge{
		tunnel: tun,
		logFn:  logFn,
	}
	tun.OnData = rb.handleTunnelData
	tun.OnClose = rb.closeAll
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
	frame := EncodeFrame(connID, msgType, payload)
	rb.tunnel.SendData(frame)
}

func (rb *RelayBridge) handleTunnelData(data []byte) {
	DecodeFrames(data, rb.handleCreatorMessage)
}

func (rb *RelayBridge) handleCreatorMessage(connID uint32, msgType byte, payload []byte) {
	switch msgType {
	case MsgConnect:
		go rb.connectTCP(connID, string(payload))
	case MsgUDP:
		go rb.handleUDP(connID, payload)
	case MsgData:
		val, ok := rb.conns.Load(connID)
		if ok {
			val.(net.Conn).Write(payload)
		}
	case MsgClose:
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
	buf := make([]byte, UDPBufSize)
	n, err := conn.Read(buf)
	if err != nil {
		return
	}
	rb.send(connID, MsgUDPReply, buf[:n])
}

func (rb *RelayBridge) connectTCP(connID uint32, addr string) {
	rb.logFn("relay: CONNECT %d -> %s", connID, MaskAddr(addr))
	conn, err := net.DialTimeout("tcp", addr, 10e9)
	if err != nil {
		rb.logFn("relay: CONNECT %d failed: %v", connID, err)
		rb.send(connID, MsgConnectErr, []byte(err.Error()))
		return
	}
	rb.conns.Store(connID, conn)
	rb.send(connID, MsgConnectOK, nil)
	rb.logFn("relay: CONNECTED %d -> %s", connID, MaskAddr(addr))

	buf := make([]byte, VP8RelayBufSize)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			rb.send(connID, MsgData, buf[:n])
		}
		if err != nil {
			if err != io.EOF {
				rb.logFn("relay: conn %d read error: %v", connID, err)
			}
			break
		}
	}
	rb.send(connID, MsgClose, nil)
	rb.conns.Delete(connID)
}
