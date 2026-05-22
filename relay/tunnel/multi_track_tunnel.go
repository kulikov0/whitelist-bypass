package tunnel

import (
	"encoding/binary"
	"sync"
)

type MultiTrackTunnel struct {
	tunnels  []*VP8DataTunnel
	mu       sync.Mutex
	onData   func([]byte)
	onClose  func()
	isClosed bool
	fps      int
	batch    int
}

func NewMultiTrackTunnel(tunnels []*VP8DataTunnel) *MultiTrackTunnel {
	m := &MultiTrackTunnel{
		tunnels: tunnels,
	}

	for _, tun := range tunnels {
		tun.SetOnData(func(data []byte) {
			m.mu.Lock()
			handler := m.onData
			m.mu.Unlock()

			if handler != nil {
				handler(data)
			}
		})

		tun.SetOnClose(func() {
			m.mu.Lock()
			if m.isClosed {
				m.mu.Unlock()
				return
			}
			m.isClosed = true
			handler := m.onClose
			m.mu.Unlock()

			for _, t := range m.tunnels {
				t.Stop()
			}

			if handler != nil {
				handler()
			}
		})
	}

	return m
}

func (m *MultiTrackTunnel) SendData(data []byte) {
	if len(m.tunnels) == 0 {
		return
	}

	var connID uint32
	if len(data) >= 8 {
		connID = binary.BigEndian.Uint32(data[4:8])
	}

	idx := connID % uint32(len(m.tunnels))
	m.tunnels[idx].SendData(data)
}

func (m *MultiTrackTunnel) SetOnData(fn func([]byte)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onData = fn
}

func (m *MultiTrackTunnel) SetOnClose(fn func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onClose = fn
}

func (m *MultiTrackTunnel) Reconfigure(fps, batch int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fps = fps
	m.batch = batch
	for _, tun := range m.tunnels {
		tun.Reconfigure(fps, batch)
	}
}

func (m *MultiTrackTunnel) Start(fps, batch int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fps = fps
	m.batch = batch
	for _, tun := range m.tunnels {
		tun.Start(fps, batch)
	}
}

func (m *MultiTrackTunnel) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.isClosed {
		return
	}
	m.isClosed = true
	for _, tun := range m.tunnels {
		tun.Stop()
	}
}

func (m *MultiTrackTunnel) HandleFrame(frame []byte) {
	if len(m.tunnels) > 0 {
		// Server passes all incoming frames to tunnels[0] because
		// all tracks use the same obfuscator epoch and tunnels[0] will
		// decrypt it and call its OnData which we wired to m.onData.
		m.tunnels[0].HandleFrame(frame)
	}
}

