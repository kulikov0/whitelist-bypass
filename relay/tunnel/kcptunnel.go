package tunnel

import (
	"sync"
	"sync/atomic"
	"time"

	kcp "github.com/xtaci/kcp-go/v5"
)

const (
	kcpConversationID = 0x77627374
	kcpUpdateInterval = 10 * time.Millisecond
	kcpSendWindow     = 1024
	kcpRecvWindow     = 1024
	kcpSegmentMTU     = 1000
	kcpReceiveBufSize = 128 * 1024
	kcpStatsEvery     = 500
)

type KCPTunnel struct {
	inner DataTunnel
	logFn func(string, ...any)

	mu      sync.Mutex
	kcp     *kcp.KCP
	recvBuf []byte
	onData  func([]byte)
	onClose func()

	stopCh   chan struct{}
	stopOnce sync.Once

	sentMessages      atomic.Uint64
	deliveredMessages atomic.Uint64
	outputSegments    atomic.Uint64
	inputSegments     atomic.Uint64
}

func NewKCPTunnel(inner DataTunnel, logFn func(string, ...any)) *KCPTunnel {
	t := &KCPTunnel{
		inner:   inner,
		logFn:   logFn,
		recvBuf: make([]byte, kcpReceiveBufSize),
		stopCh:  make(chan struct{}),
	}
	t.kcp = kcp.NewKCP(kcpConversationID, func(buf []byte, size int) {
		if size <= 0 {
			return
		}
		segment := make([]byte, size)
		copy(segment, buf[:size])
		t.outputSegments.Add(1)
		t.inner.SendData(segment)
	})
	t.kcp.NoDelay(1, 10, 2, 1)
	t.kcp.WndSize(kcpSendWindow, kcpRecvWindow)
	t.kcp.SetMtu(kcpSegmentMTU)
	inner.SetOnData(t.handleInnerData)
	inner.SetOnClose(t.handleInnerClose)
	go t.updateLoop()
	return t
}

func (t *KCPTunnel) SendData(data []byte) {
	if len(data) == 0 {
		return
	}
	t.sentMessages.Add(1)
	t.mu.Lock()
	t.kcp.Send(data)
	t.kcp.Update()
	t.mu.Unlock()
}

func (t *KCPTunnel) SetOnData(fn func([]byte)) {
	t.mu.Lock()
	t.onData = fn
	t.mu.Unlock()
}

func (t *KCPTunnel) SetOnClose(fn func()) {
	t.mu.Lock()
	t.onClose = fn
	t.mu.Unlock()
}

func (t *KCPTunnel) Reconfigure(fps, batch int) {
	t.inner.Reconfigure(fps, batch)
}

func (t *KCPTunnel) Stop() {
	t.stopOnce.Do(func() { close(t.stopCh) })
}

func (t *KCPTunnel) handleInnerData(segment []byte) {
	if len(segment) == 0 {
		return
	}
	t.inputSegments.Add(1)
	t.mu.Lock()
	t.kcp.Input(segment, kcp.IKCP_PACKET_REGULAR, true)
	cb := t.onData
	var messages [][]byte
	if cb != nil {
		for {
			size := t.kcp.PeekSize()
			if size <= 0 {
				break
			}
			if size > len(t.recvBuf) {
				t.recvBuf = make([]byte, size)
			}
			n := t.kcp.Recv(t.recvBuf)
			if n <= 0 {
				break
			}
			message := make([]byte, n)
			copy(message, t.recvBuf[:n])
			messages = append(messages, message)
		}
	}
	t.mu.Unlock()

	if cb == nil {
		return
	}
	for _, message := range messages {
		t.deliveredMessages.Add(1)
		cb(message)
	}
}

func (t *KCPTunnel) handleInnerClose() {
	t.stopOnce.Do(func() { close(t.stopCh) })
	t.mu.Lock()
	cb := t.onClose
	t.mu.Unlock()
	if cb != nil {
		cb()
	}
}

func (t *KCPTunnel) updateLoop() {
	ticker := time.NewTicker(kcpUpdateInterval)
	defer ticker.Stop()
	ticks := 0
	for {
		select {
		case <-t.stopCh:
			return
		case <-ticker.C:
			t.mu.Lock()
			t.kcp.Update()
			t.mu.Unlock()
			ticks++
			if ticks%kcpStatsEvery == 0 && t.logFn != nil {
				t.logFn("kcptunnel: sent=%d delivered=%d out_segs=%d in_segs=%d",
					t.sentMessages.Load(), t.deliveredMessages.Load(),
					t.outputSegments.Load(), t.inputSegments.Load())
			}
		}
	}
}
