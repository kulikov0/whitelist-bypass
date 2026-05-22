package tunnel

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"whitelist-bypass/relay/common"
)

type udpClient struct {
	udpConn    *net.UDPConn
	clientAddr *net.UDPAddr
	socksHdr   []byte
	mapKey     string
}

type RelayBridge struct {
	tunnel      DataTunnel
	conns       sync.Map
	udpClients  sync.Map
	udpSessions sync.Map
	nextID      atomic.Uint32
	logFn      func(string, ...any)
	mode       string
	readBuf    int
	ready      chan struct{}
	once       sync.Once
	socksUser  string
	socksPass  string

	listenerMu sync.Mutex
	listener   net.Listener
	closed     atomic.Bool
}

func NewRelayBridgeWithAuth(tunnel DataTunnel, mode string, readBuf int, logFn func(string, ...any), socksUser, socksPass string) *RelayBridge {
	rb := NewRelayBridge(tunnel, mode, readBuf, logFn)
	rb.socksUser = socksUser
	rb.socksPass = socksPass
	return rb
}

func NewRelayBridge(tunnel DataTunnel, mode string, readBuf int, logFn func(string, ...any)) *RelayBridge {
	rb := &RelayBridge{
		tunnel:  tunnel,
		logFn:   logFn,
		mode:    mode,
		readBuf: readBuf,
		ready:   make(chan struct{}),
	}
	tunnel.SetOnData(rb.handleTunnelData)
	tunnel.SetOnClose(rb.Close)
	return rb
}

func (rb *RelayBridge) closeAll() {
	var ids []uint32
	rb.conns.Range(func(key, value any) bool {
		if id, ok := key.(uint32); ok {
			ids = append(ids, id)
		}
		switch v := value.(type) {
		case net.Conn:
			v.Close()
		case *socksConn:
			v.conn.Close()
		}
		rb.conns.Delete(key)
		return true
	})
	udpCount := 0
	rb.udpClients.Range(func(key, _ any) bool {
		udpCount++
		rb.udpClients.Delete(key)
		return true
	})
	rb.logFn("relay: closeAll mode=%s tcp=%d udp=%d ids=%v nextID=%d", rb.mode, len(ids), udpCount, ids, rb.nextID.Load())
}

func (rb *RelayBridge) Reset() {
	rb.closeAll()
}

func (rb *RelayBridge) Close() {
	if !rb.closed.CompareAndSwap(false, true) {
		return
	}
	rb.listenerMu.Lock()
	ln := rb.listener
	rb.listener = nil
	rb.listenerMu.Unlock()
	if ln != nil {
		rb.logFn("relay: bridge Close closing socks listener")
		ln.Close()
	}
	rb.closeAll()
}

func (rb *RelayBridge) Stats() (tcpConns, udpConns int, nextID uint32) {
	rb.conns.Range(func(_, _ any) bool { tcpConns++; return true })
	rb.udpClients.Range(func(_, _ any) bool { udpConns++; return true })
	return tcpConns, udpConns, rb.nextID.Load()
}

func (rb *RelayBridge) MarkReady() {
	rb.once.Do(func() { close(rb.ready) })
}

func (rb *RelayBridge) send(connID uint32, msgType byte, payload []byte) {
	frame := EncodeFrame(connID, msgType, payload)
	rb.tunnel.SendData(frame)
}

func (rb *RelayBridge) handleTunnelData(data []byte) {
	DecodeFrames(data, func(connID uint32, msgType byte, payload []byte) {
		if connID == ControlConnID && msgType == MsgConfig {
			fps, batch, ok := DecodeVP8Config(payload)
			if !ok {
				return
			}
			if rb.mode == "creator" {
				rb.logFn("relay: peer requested vp8 pacing fps=%d batch=%d", fps, batch)
				rb.tunnel.Reconfigure(fps, batch)
			}
			return
		}
		switch rb.mode {
		case "joiner":
			rb.handleJoinerMessage(connID, msgType, payload)
		case "creator":
			rb.handleCreatorMessage(connID, msgType, payload)
		}
	})
}

func (rb *RelayBridge) handleJoinerMessage(connID uint32, msgType byte, payload []byte) {
	if msgType == MsgUDPReply {
		uval, ok := rb.udpClients.Load(connID)
		if !ok {
			rb.send(connID, MsgClose, nil)
			return
		}
		uc := uval.(*udpClient)
		reply := make([]byte, len(uc.socksHdr)+len(payload))
		copy(reply, uc.socksHdr)
		copy(reply[len(uc.socksHdr):], payload)
		uc.udpConn.WriteToUDP(reply, uc.clientAddr)
		// DO NOT delete connID here, UDP sessions are persistent
		return
	}
	
	val, ok := rb.conns.Load(connID)
	if !ok {
		// If it's a UDP close, handle it
		if msgType == MsgClose {
			if uval, ok := rb.udpClients.LoadAndDelete(connID); ok {
				uc := uval.(*udpClient)
				rb.udpSessions.Delete(uc.mapKey)
			}
		} else {
			rb.logFn("relay[joiner]: drop msgType=%d for unknown conn %d (payload=%dB)", msgType, connID, len(payload))
			if msgType == MsgData {
				rb.send(connID, MsgClose, nil)
			}
		}
		return
	}
	
	sc := val.(*socksConn)
	switch msgType {
	case MsgConnectOK:
		select {
		case sc.rdy <- nil:
		default:
			rb.logFn("relay[joiner]: MsgConnectOK %d: rdy already signalled (duplicate)", connID)
		}
	case MsgConnectErr:
		select {
		case sc.rdy <- fmt.Errorf("%s", payload):
		default:
			rb.logFn("relay[joiner]: MsgConnectErr %d: rdy already signalled (duplicate)", connID)
		}
	case MsgData:
		if _, err := sc.conn.Write(payload); err != nil {
			rb.logFn("relay[joiner]: write to socks %d failed: %s", connID, common.MaskError(err))
		}
	case MsgClose:
		sc.conn.Close()
		rb.conns.Delete(connID)
	}
}

func (rb *RelayBridge) handleCreatorMessage(connID uint32, msgType byte, payload []byte) {
	switch msgType {
	case MsgConnect:
		go rb.connectTCP(connID, string(payload))
	case MsgUDP:
		payloadCopy := make([]byte, len(payload))
		copy(payloadCopy, payload)
		go rb.handleUDP(connID, payloadCopy)
	case MsgData:
		val, ok := rb.conns.Load(connID)
		if !ok {
			rb.logFn("relay[creator]: drop MsgData for unknown conn %d (payload=%dB)", connID, len(payload))
			rb.send(connID, MsgClose, nil)
			return
		}
		if c, ok := val.(net.Conn); ok {
			if _, err := c.Write(payload); err != nil {
				rb.logFn("relay[creator]: write to target %d failed: %s", connID, common.MaskError(err))
			}
		}
	case MsgClose:
		found := false
		if val, ok := rb.conns.LoadAndDelete(connID); ok {
			found = true
			if c, ok := val.(net.Conn); ok {
				c.Close()
			}
		}
		if uval, ok := rb.udpClients.LoadAndDelete(connID); ok {
			found = true
			if uc, ok := uval.(*net.UDPConn); ok {
				uc.Close()
			}
		}
		if !found {
			rb.logFn("relay[creator]: drop MsgClose for unknown conn %d", connID)
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

	var conn *net.UDPConn
	if val, ok := rb.udpClients.Load(connID); ok {
		conn = val.(*net.UDPConn)
	} else {
		udpAddr, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			return
		}
		conn, err = net.DialUDP("udp", nil, udpAddr)
		if err != nil {
			return
		}
		rb.udpClients.Store(connID, conn)

		go func(c *net.UDPConn, id uint32) {
			defer c.Close()
			defer rb.udpClients.Delete(id)
			defer rb.send(id, MsgClose, nil)

			buf := make([]byte, common.UDPBufSize)
			for {
				c.SetReadDeadline(time.Now().Add(60 * time.Second))
				n, err := c.Read(buf)
				if err != nil {
					break
				}
				rb.send(id, MsgUDPReply, buf[:n])
			}
		}(conn, connID)
	}

	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	conn.Write(data)
}

func (rb *RelayBridge) connectTCP(connID uint32, addr string) {
	rb.logFn("relay: CONNECT %d -> %s", connID, common.MaskAddr(addr))
	conn, err := net.DialTimeout("tcp", addr, 10e9)
	if err != nil {
		rb.logFn("relay: CONNECT %d failed: %s", connID, common.MaskError(err))
		rb.send(connID, MsgConnectErr, []byte(common.MaskError(err)))
		return
	}
	rb.conns.Store(connID, conn)
	rb.send(connID, MsgConnectOK, nil)
	rb.logFn("relay: CONNECTED %d -> %s", connID, common.MaskAddr(addr))

	buf := make([]byte, rb.readBuf)
	var totalRead int64
	var reads int
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			rb.send(connID, MsgData, buf[:n])
			totalRead += int64(n)
			reads++
			if reads == 1 {
				rb.logFn("relay: conn %d first read %dB", connID, n)
			}
		}
		if err != nil {
			if err != io.EOF {
				rb.logFn("relay: conn %d read error: %s (read %d times, %dB)", connID, common.MaskError(err), reads, totalRead)
			} else if reads == 0 {
				rb.logFn("relay: conn %d EOF with no data read", connID)
			}
			break
		}
	}
	rb.send(connID, MsgClose, nil)
	rb.conns.Delete(connID)
}

type socksConn struct {
	id   uint32
	conn net.Conn
	rb   *RelayBridge
	rdy  chan error
}

func (rb *RelayBridge) ListenSOCKS(addr string) error {
	if rb.closed.Load() {
		return fmt.Errorf("relay: bridge already closed")
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	rb.listenerMu.Lock()
	if rb.closed.Load() {
		rb.listenerMu.Unlock()
		ln.Close()
		return fmt.Errorf("relay: bridge already closed")
	}
	rb.listener = ln
	rb.listenerMu.Unlock()
	rb.logFn("relay: SOCKS5 on %s", addr)
	for {
		conn, err := ln.Accept()
		if err != nil {
			if rb.closed.Load() {
				rb.logFn("relay: SOCKS listener stopped (bridge closed)")
				return nil
			}
			rb.logFn("relay: accept error: %v", err)
			continue
		}
		go rb.handleSOCKS(conn)
	}
}

func (rb *RelayBridge) handleSOCKS(conn net.Conn) {
	<-rb.ready
	if rb.closed.Load() {
		conn.Close()
		return
	}
	buf := make([]byte, common.HandshakeBuf)
	n, err := conn.Read(buf)
	if err != nil || n < 2 || buf[0] != common.Ver {
		conn.Close()
		return
	}
	if !common.NegotiateAuth(conn, buf, n, rb.socksUser, rb.socksPass) {
		conn.Close()
		return
	}
	n, err = conn.Read(buf)
	if err != nil || n < 7 || buf[0] != common.Ver {
		conn.Close()
		return
	}
	cmd := buf[1]
	if cmd == common.CmdUDP {
		rb.handleUDPAssociate(conn)
		return
	}
	if cmd != common.CmdTCP {
		conn.Write(common.CmdErr)
		conn.Close()
		return
	}
	host, _, err := common.ParseAddress(buf, n)
	if err != nil {
		conn.Write(common.AddrErr)
		conn.Close()
		return
	}

	hostOnly, _, _ := net.SplitHostPort(host)
	if ip := net.ParseIP(hostOnly); ip != nil && ip.IsUnspecified() {
		conn.Write(common.ConnFail)
		conn.Close()
		return
	}
	// Dial local/private addresses directly instead of tunneling to the creator,
	// which cannot reach the joiner's local network. Disabled for now until
	// there is a real use case for local network access through the proxy. So idk if 
	// this is a bug or a feature
	// if ip := net.ParseIP(hostOnly); ip != nil && !ip.IsGlobalUnicast() {
	// 	rb.logFn("relay: SOCKS local dial %s", common.MaskAddr(host))
	// 	target, dialErr := net.DialTimeout("tcp", host, 10*time.Second)
	// 	if dialErr != nil {
	// 		rb.logFn("relay: SOCKS local dial failed: %s", common.MaskError(dialErr))
	// 		conn.Write(common.ConnFail)
	// 		conn.Close()
	// 		return
	// 	}
	// 	conn.Write(common.OK)
	// 	go func() {
	// 		defer target.Close()
	// 		defer conn.Close()
	// 		done := make(chan struct{})
	// 		go func() {
	// 			io.Copy(target, conn)
	// 			close(done)
	// 		}()
	// 		io.Copy(conn, target)
	// 		<-done
	// 	}()
	// 	return
	// }

	id := rb.nextID.Add(1)
	sc := &socksConn{id: id, conn: conn, rb: rb, rdy: make(chan error, 1)}
	rb.conns.Store(id, sc)
	rb.logFn("relay: SOCKS CONNECT %d -> %s", id, common.MaskAddr(host))
	rb.send(id, MsgConnect, []byte(host))

	rdyStart := time.Now()
	select {
	case rdyErr := <-sc.rdy:
		if rdyErr != nil {
			rb.logFn("relay: SOCKS CONNECT %d failed after %s: %s", id, time.Since(rdyStart), common.MaskError(rdyErr))
			conn.Write(common.ConnFail)
			conn.Close()
			rb.conns.Delete(id)
			return
		}
	case <-time.After(20 * time.Second):
		rb.logFn("relay: SOCKS CONNECT %d TIMEOUT after %s waiting for MsgConnectOK", id, time.Since(rdyStart))
		conn.Write(common.ConnFail)
		conn.Close()
		rb.conns.Delete(id)
		return
	}
	conn.Write(common.OK)
	rb.logFn("relay: SOCKS CONNECTED %d -> %s rdy_wait=%s", id, common.MaskAddr(host), time.Since(rdyStart))

	go func() {
		readBuf := make([]byte, rb.readBuf)
		var totalSent int64
		var sends int
		for {
			rn, rerr := conn.Read(readBuf)
			if rn > 0 {
				rb.send(id, MsgData, readBuf[:rn])
				totalSent += int64(rn)
				sends++
				if sends == 1 {
					rb.logFn("relay: SOCKS %d first send %dB to tunnel", id, rn)
				}
			}
			if rerr != nil {
				rb.send(id, MsgClose, nil)
				rb.conns.Delete(id)
				if rerr != io.EOF {
					rb.logFn("relay: SOCKS %d read error: %s (sent %d times, %dB)", id, common.MaskError(rerr), sends, totalSent)
				}
				return
			}
		}
	}()
}

func (rb *RelayBridge) handleUDPAssociate(tcpConn net.Conn) {
	udpAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		tcpConn.Write(common.GenFail)
		tcpConn.Close()
		return
	}
	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		tcpConn.Write(common.GenFail)
		tcpConn.Close()
		return
	}
	localAddr := udpConn.LocalAddr().(*net.UDPAddr)
	reply := []byte{common.Ver, 0x00, 0x00, common.AtypIPv4, 127, 0, 0, 1, 0, 0}
	binary.BigEndian.PutUint16(reply[8:10], uint16(localAddr.Port))
	tcpConn.Write(reply)

	go func() {
		buf := make([]byte, 1)
		tcpConn.Read(buf)
		udpConn.Close()
	}()

	go func() {
		var sessionKeys []string
		var mu sync.Mutex

		defer udpConn.Close()
		defer tcpConn.Close()
		
		defer func() {
			mu.Lock()
			for _, k := range sessionKeys {
				if idVal, ok := rb.udpSessions.Load(k); ok {
					id := idVal.(uint32)
					rb.send(id, MsgClose, nil)
					rb.udpClients.Delete(id)
					rb.udpSessions.Delete(k)
				}
			}
			mu.Unlock()
		}()

		buf := make([]byte, common.UDPBufSize)
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
			dstAddr, headerLen, addrErr := common.ParseAddress(buf, n)
			if addrErr != nil {
				continue
			}
			
			mapKey := addr.String() + "|" + string(dstAddr)
			var id uint32
			if idVal, ok := rb.udpSessions.Load(mapKey); ok {
				id = idVal.(uint32)
			} else {
				id = rb.nextID.Add(1)
				hdrCopy := make([]byte, headerLen)
				copy(hdrCopy, buf[:headerLen])
				rb.udpClients.Store(id, &udpClient{udpConn: udpConn, clientAddr: addr, socksHdr: hdrCopy, mapKey: mapKey})
				rb.udpSessions.Store(mapKey, id)
				
				mu.Lock()
				sessionKeys = append(sessionKeys, mapKey)
				mu.Unlock()
			}

			payload := make([]byte, len(dstAddr)+1+n-headerLen)
			payload[0] = byte(len(dstAddr))
			copy(payload[1:], dstAddr)
			copy(payload[1+len(dstAddr):], buf[headerLen:n])
			rb.send(id, MsgUDP, payload)
		}
	}()
}
