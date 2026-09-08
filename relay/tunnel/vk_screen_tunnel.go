package tunnel

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"time"

	"whitelist-bypass/relay/common"
)

const (
	screenWriterFPS       = 24
	screenWriterBatch     = 30
	screenWriterMaxBytes  = 60000
	screenWriterQueue     = 256
	screenKeepalivePadMax = 48
)

type ScreenWriter struct {
	obf   *TunnelObfuscator
	logFn func(string, ...any)
	label string

	sendMu sync.Mutex
	send   func([]byte) error

	stopCh    chan struct{}
	sendQueue chan []byte
	cfgChan   chan struct{}
	stopOnce  sync.Once
	running   atomic.Bool

	cfgMu sync.Mutex
	fps   int
	batch int
	sent  atomic.Uint64
}

func NewScreenWriter(obf *TunnelObfuscator, label string, logFn func(string, ...any)) *ScreenWriter {
	return &ScreenWriter{
		obf:       obf,
		logFn:     logFn,
		label:     label,
		stopCh:    make(chan struct{}),
		sendQueue: make(chan []byte, screenWriterQueue),
		cfgChan:   make(chan struct{}, 1),
		fps:       screenWriterFPS,
		batch:     screenWriterBatch,
	}
}

// SetSend installs the transport. Until it is set, frames are dropped.
func (w *ScreenWriter) SetSend(fn func([]byte) error) {
	w.sendMu.Lock()
	w.send = fn
	w.sendMu.Unlock()
}

func (w *ScreenWriter) SendData(data []byte) {
	if len(data) == 0 {
		return
	}
	select {
	case w.sendQueue <- data:
	case <-w.stopCh:
	}
}

func (w *ScreenWriter) Reconfigure(fps, batch int) {
	if fps <= 0 && batch <= 0 {
		return
	}
	w.cfgMu.Lock()
	changed := false
	if fps > 0 && w.fps != fps {
		w.fps = fps
		changed = true
	}
	if batch > 0 && w.batch != batch {
		w.batch = batch
		changed = true
	}
	w.cfgMu.Unlock()
	if changed {
		select {
		case w.cfgChan <- struct{}{}:
		default:
		}
	}
}

func (w *ScreenWriter) interval() time.Duration {
	w.cfgMu.Lock()
	fps, batch := w.fps, w.batch
	w.cfgMu.Unlock()
	frame := time.Second / time.Duration(fps)
	sample := frame
	if batch > 1 {
		sample = frame / time.Duration(batch)
	}
	if sample <= 0 {
		sample = time.Millisecond
	}
	return sample
}

func (w *ScreenWriter) nextKeepalive(sample time.Duration) (ticks, padLen int) {
	ticks = int(common.DurationInRange(keepaliveIdleMin, keepaliveIdleMax) / sample)
	if ticks < 1 {
		ticks = 1
	}
	return ticks, common.IntInRange(0, screenKeepalivePadMax)
}

func (w *ScreenWriter) Start() {
	if !w.running.CompareAndSwap(false, true) {
		return
	}
	go w.writerLoop()
}

func (w *ScreenWriter) Stop() {
	if !w.running.CompareAndSwap(true, false) {
		return
	}
	w.stopOnce.Do(func() { close(w.stopCh) })
}

func (w *ScreenWriter) emit(msg []byte) {
	if msg == nil || len(msg) > screenWriterMaxBytes {
		return
	}
	w.sendMu.Lock()
	send := w.send
	w.sendMu.Unlock()
	if send == nil {
		return
	}
	if err := send(msg); err != nil {
		return
	}
	n := w.sent.Add(1)
	if common.Debug && (n <= 5 || n%500 == 0) {
		w.logFn("[%s] sent frame #%d size=%d", w.label, n, len(msg))
	}
}

func (w *ScreenWriter) writerLoop() {
	for {
		sample := w.interval()
		keepaliveEvery, keepalivePad := w.nextKeepalive(sample)
		ticker := time.NewTicker(sample)
		idle := 0
		reconfigure := false
		for !reconfigure {
			select {
			case <-w.stopCh:
				ticker.Stop()
				return
			case <-w.cfgChan:
				reconfigure = true
			case <-ticker.C:
				select {
				case data := <-w.sendQueue:
					w.emit(w.obf.EncodeData(data))
					idle = 0
				default:
					idle++
					if idle < keepaliveEvery {
						continue
					}
					idle = 0
					w.emit(w.obf.EncodeKeepalive(keepalivePad))
					keepaliveEvery, keepalivePad = w.nextKeepalive(sample)
				}
			}
		}
		ticker.Stop()
	}
}

type SymmetricScreenTunnel struct {
	cam         *VP8DataTunnel
	screen      *ScreenWriter
	obf         *TunnelObfuscator
	logFn       func(string, ...any)
	screenReady func() bool

	onDataMu   sync.Mutex
	onData     func([]byte)
	recv       atomic.Uint64
	trackCount atomic.Int32
}

func (s *SymmetricScreenTunnel) SetTrackCount(n int) {
	if n < 1 {
		n = 1
	}
	if n > 2 {
		n = 2 // VK has two channels: camera VP8 + screen DC
	}
	old := s.trackCount.Swap(int32(n))
	if int(old) != n {
		s.logFn("screen tunnel track count %d -> %d", old, n)
	}
	if n >= 2 {
		s.screen.Start()
	}
}

func NewSymmetricScreenTunnel(cam *VP8DataTunnel, screen *ScreenWriter, obf *TunnelObfuscator, screenReady func() bool, logFn func(string, ...any)) *SymmetricScreenTunnel {
	return &SymmetricScreenTunnel{cam: cam, screen: screen, obf: obf, screenReady: screenReady, logFn: logFn}
}

func (s *SymmetricScreenTunnel) screenUp() bool {
	if s.screenReady == nil {
		return true
	}
	return s.screenReady()
}

func (s *SymmetricScreenTunnel) SendData(data []byte) {
	var connID uint32
	if len(data) >= 8 {
		connID = binary.BigEndian.Uint32(data[4:8])
	}
	if connID == ControlConnID {
		s.cam.SendData(data)
		return
	}
	tc := uint32(s.trackCount.Load())
	if tc < 1 {
		tc = 1
	}
	if connID%tc == 1 && s.screenUp() {
		s.screen.SendData(data)
		return
	}
	s.cam.SendData(data)
}

func (s *SymmetricScreenTunnel) SetOnData(fn func([]byte)) {
	s.onDataMu.Lock()
	s.onData = fn
	s.onDataMu.Unlock()
	s.cam.SetOnData(fn)
}

func (s *SymmetricScreenTunnel) SetOnClose(fn func()) { s.cam.SetOnClose(fn) }
func (s *SymmetricScreenTunnel) Reconfigure(fps, batch int) {
	s.cam.Reconfigure(fps, batch)
	s.screen.Reconfigure(fps, batch)
}

func (s *SymmetricScreenTunnel) Stop() {
	s.screen.Stop()
	s.cam.Stop()
}

func (s *SymmetricScreenTunnel) HandleScreenFrame(frame []byte) {
	res := s.obf.Decode(frame)
	n := s.recv.Add(1)
	if common.Debug && (n <= 10 || n%500 == 0) {
		s.logFn("screen recv frame #%d in=%d hasFrame=%v keepalive=%v payload=%d", n, len(frame), res.HasFrame, res.Keepalive, len(res.Payload))
	}
	if !res.HasFrame || res.SelfEcho || res.Keepalive || len(res.Payload) == 0 {
		return
	}
	s.onDataMu.Lock()
	handler := s.onData
	s.onDataMu.Unlock()
	if handler != nil {
		handler(res.Payload)
	}
}
