package tunnel

import (
	"encoding/binary"
	"sync"
)

type WSTunnel struct {
	mu      sync.RWMutex
	sendFn  func([]byte)
	onData  func([]byte)
	onClose func()
}

func NewWSTunnel() *WSTunnel {
	return &WSTunnel{}
}

func (t *WSTunnel) SetSendFn(sendFn func([]byte)) {
	t.mu.Lock()
	t.sendFn = sendFn
	t.mu.Unlock()
}

func (t *WSTunnel) SendData(data []byte) {
	t.mu.RLock()
	sendFn := t.sendFn
	t.mu.RUnlock()
	if sendFn == nil {
		return
	}
	DecodeFrames(data, func(connID uint32, msgType byte, payload []byte) {
		msg := make([]byte, WireHeaderLen+len(payload))
		binary.BigEndian.PutUint32(msg[0:4], connID)
		msg[4] = msgType
		copy(msg[WireHeaderLen:], payload)
		sendFn(msg)
	})
}

func (t *WSTunnel) Deliver(msg []byte) {
	if len(msg) < WireHeaderLen {
		return
	}
	t.mu.RLock()
	onData := t.onData
	t.mu.RUnlock()
	if onData == nil {
		return
	}
	frame := make([]byte, 4+len(msg))
	binary.BigEndian.PutUint32(frame[0:4], uint32(len(msg)))
	copy(frame[4:], msg)
	onData(frame)
}

func (t *WSTunnel) NotifyClose() {
	t.mu.RLock()
	onClose := t.onClose
	t.mu.RUnlock()
	if onClose != nil {
		onClose()
	}
}

func (t *WSTunnel) SetOnData(fn func([]byte)) {
	t.mu.Lock()
	t.onData = fn
	t.mu.Unlock()
}

func (t *WSTunnel) SetOnClose(fn func()) {
	t.mu.Lock()
	t.onClose = fn
	t.mu.Unlock()
}

func (t *WSTunnel) Reconfigure(fps, batch int) {}
