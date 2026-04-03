package main

import (
	"context"
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
	udps   sync.Map
	nextID atomic.Uint32
	logFn  func(string, ...any)

	ctx    context.Context
	cancel context.CancelFunc
}

type videoUDPConn struct {
	conn       net.Conn
	mu         sync.Mutex
	lastActive atomic.Int64
}

func NewRelayBridge(tunnel *VP8DataTunnel, mode string, logFn func(string, ...any)) *RelayBridge {
	rb := &RelayBridge{
		tunnel: tunnel,
		logFn:  logFn,
	}
	rb.ctx, rb.cancel = context.WithCancel(context.Background())
	go rb.udpSweeper()
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
	rb.udps.Range(func(key, value any) bool {
		if c, ok := value.(*videoUDPConn); ok {
			_ = c.conn.Close()
		}
		rb.udps.Delete(key)
		return true
	})
	if rb.cancel != nil {
		rb.cancel()
	}
}

func (rb *RelayBridge) udpSweeper() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()

	for {
		select {
		case <-rb.ctx.Done():
			return
		case <-t.C:
		}
		now := time.Now().UnixNano()
		rb.udps.Range(func(key, val any) bool {
			s := val.(*videoUDPConn)
			if now-s.lastActive.Load() > int64(2*udpIdleTimeout) {
				s.conn.Close()
				rb.udps.Delete(key)
			}
			return true
		})
	}
}

func (rb *RelayBridge) send(connID uint32, msgType byte, payload []byte) {
	frame := encodeFrame(connID, msgType, payload)
	rb.tunnel.SendData(frame)
}

func (rb *RelayBridge) handleTunnelData(data []byte) {
	decodeFrames(data, rb.handleCreatorMessage)
}

func (rb *RelayBridge) handleCreatorMessage(connID uint32, msgType byte, payload []byte) {
	if msgType != msgData {
		rb.logFn("relay: RX frame conn=%d type=%d payload=%d", connID, msgType, len(payload))
	}
	switch msgType {
	case msgConnect:
		go rb.connectTCP(connID, string(payload))
	case msgUDP:
		go rb.handleUDP(connID, payload)
	case msgData:
		val, ok := rb.conns.Load(connID)
		if ok {
			c := val.(net.Conn)
			_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
			_, err := c.Write(payload)
			if err != nil {
				rb.logFn("relay: conn %d downstream write error: %v", connID, err)
			}
		} else {
			rb.logFn("relay: DATA for unknown conn %d (%d bytes)", connID, len(payload))
		}
	case msgClose:
		val, ok := rb.conns.LoadAndDelete(connID)
		if ok {
			closeWriteConn(val.(net.Conn))
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
	addr := string(payload[1 : 1+addrLen])
	data := payload[1+addrLen:]
	rb.logFn("relay: UDP conn=%d -> %s payload=%d", connID, maskAddr(addr), len(data))
	val, ok := rb.udps.Load(connID)
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
		sess := &videoUDPConn{
			conn: udpConn,
		}
		sess.lastActive.Store(time.Now().UnixNano())
		actual, loaded := rb.udps.LoadOrStore(connID, sess)
		if loaded {
			udpConn.Close()
			val = actual
		} else {
			val = sess
			go func() {
				buf := make([]byte, udpBufSize)
				defer func() {
					rb.udps.Delete(connID)
					_ = udpConn.Close()
				}()

				for {
					n, err := udpConn.Read(buf)

					if n > 0 {
						sess.lastActive.Store(time.Now().UnixNano())

						resp := make([]byte, n)
						copy(resp, buf[:n])

						rb.logFn("relay: UDP conn=%d reply=%d bytes", connID, n)
						rb.send(connID, msgUDPReply, resp)
					}

					if err != nil {
						if !isClosedConnError(err) {
							rb.logFn("UDP read failed conn=%d: %v", connID, err)
						}
						return
					}
				}
			}()
		}
	}
	sess := val.(*videoUDPConn)
	sess.mu.Lock()
	_ = sess.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := sess.conn.Write(data)
	sess.lastActive.Store(time.Now().UnixNano())
	sess.mu.Unlock()
	if err != nil {
		if !isClosedConnError(err) {
			rb.logFn("relay: UDP conn %d write error: %v", connID, err)
		}
		rb.udps.Delete(connID)
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

	buf := make([]byte, vp8RelayBufSize)
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
