//go:build ios || darwin

package ios

import (
	"fmt"
	"io"
	"sync"

	"github.com/xjasonlyu/tun2socks/v2/core"
	"github.com/xjasonlyu/tun2socks/v2/core/device/iobased"
	"github.com/xjasonlyu/tun2socks/v2/proxy"
	"github.com/xjasonlyu/tun2socks/v2/tunnel"

	"gvisor.dev/gvisor/pkg/tcpip/stack"

	"whitelist-bypass/relay/common"
)

const (
	afInet   = 2
	afInet6  = 30
	inboundQ = 512
)

func iosLog(format string, args ...any) {
	activeHeadless.Lock()
	cb := activeHeadless.callback
	activeHeadless.Unlock()
	if cb != nil {
		cb.OnLog(fmt.Sprintf(format, args...))
	}
}

type packetBridge struct {
	sink      PacketFlowSink
	inbound   chan []byte
	done      chan struct{}
	closeOnce sync.Once
}

func newPacketBridge(sink PacketFlowSink) *packetBridge {
	return &packetBridge{
		sink:    sink,
		inbound: make(chan []byte, inboundQ),
		done:    make(chan struct{}),
	}
}

func (b *packetBridge) Read(p []byte) (int, error) {
	select {
	case data := <-b.inbound:
		return copy(p, data), nil
	case <-b.done:
		return 0, io.EOF
	}
}

func (b *packetBridge) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	af := afInet
	if p[0]>>4 == 6 {
		af = afInet6
	}
	cp := make([]byte, len(p))
	copy(cp, p)
	b.sink.WritePacket(cp, af)
	return len(p), nil
}

func (b *packetBridge) input(data []byte) {
	if len(data) == 0 {
		return
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	select {
	case b.inbound <- cp:
	case <-b.done:
	default:
	}
}

func (b *packetBridge) close() {
	b.closeOnce.Do(func() { close(b.done) })
}

var (
	tunMu     sync.Mutex
	tunStack  *stack.Stack
	tunBridge *packetBridge
)

func StartTun2SocksPacketFlow(sink PacketFlowSink, mtu int, socksPort int, socksUser, socksPass string) error {
	tunMu.Lock()
	defer tunMu.Unlock()
	if tunStack != nil {
		return nil
	}
	if sink == nil {
		return fmt.Errorf("tun2socks: nil packet sink")
	}

	addr := fmt.Sprintf("%s:%d", common.SocksLocalhostIP, socksPort)
	dialer, err := proxy.NewSocks5(addr, socksUser, socksPass)
	if err != nil {
		return fmt.Errorf("tun2socks: socks5 dialer: %w", err)
	}
	tunnel.T().SetDialer(dialer)

	bridge := newPacketBridge(sink)
	ep, err := iobased.New(bridge, uint32(mtu), 0)
	if err != nil {
		bridge.close()
		return fmt.Errorf("tun2socks: endpoint: %w", err)
	}

	st, err := core.CreateStack(&core.Config{
		LinkEndpoint:     ep,
		TransportHandler: tunnel.T(),
	})
	if err != nil {
		bridge.close()
		return fmt.Errorf("tun2socks: create stack: %w", err)
	}

	tunStack = st
	tunBridge = bridge
	iosLog("tun2socks: packetFlow stack up, socks=%s mtu=%d", addr, mtu)
	return nil
}

func InputPacket(data []byte) {
	tunMu.Lock()
	b := tunBridge
	tunMu.Unlock()
	if b != nil {
		b.input(data)
	}
}

func StopTun2Socks() {
	tunMu.Lock()
	defer tunMu.Unlock()
	if tunStack == nil {
		return
	}
	if tunBridge != nil {
		tunBridge.close()
		tunBridge = nil
	}
	tunStack.Close()
	tunStack = nil
	iosLog("tun2socks: packetFlow stack stopped")
}
