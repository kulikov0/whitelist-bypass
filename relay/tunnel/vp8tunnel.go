package tunnel

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"

	"whitelist-bypass/relay/common"
)

const (
	defaultVP8FPS       = 24
	defaultVP8Batch     = 30
	keepaliveIdlePeriod = 100 * time.Millisecond
	keepaliveIdleMin    = 60 * time.Millisecond
	keepaliveIdleMax    = 200 * time.Millisecond
	keepalivePadMax     = 176
	sendQueueDepth      = 128

	paceBatchFloorPercent = 80
	paceDriftMin          = 5 * time.Second
	paceDriftMax          = 20 * time.Second

	idleSpinTicks = 40
)

type VP8DataTunnel struct {
	track     *webrtc.TrackLocalStaticSample
	logFn     func(string, ...any)
	obf       *TunnelObfuscator
	stopCh    chan struct{}
	sendQueue chan []byte
	cfgChan   chan struct{}

	stopOnce sync.Once
	running  atomic.Bool

	cfgMu           sync.Mutex
	fps             int
	batch           int
	keepaliveMin    time.Duration
	keepaliveMax    time.Duration
	keepalivePadMax int

	sentFrames      atomic.Uint64
	recvFrames      atomic.Uint64
	keepaliveFrames atomic.Uint64

	OnData        func([]byte)
	OnClose       func()
	OnPeerRestart func()

	WriteFrame func([]byte) error
}

func (t *VP8DataTunnel) SetOnData(fn func([]byte))  { t.OnData = fn }
func (t *VP8DataTunnel) SetOnClose(fn func())       { t.OnClose = fn }
func (t *VP8DataTunnel) SetOnPeerRestart(fn func()) { t.OnPeerRestart = fn }

func NewVP8DataTunnel(track *webrtc.TrackLocalStaticSample, obf *TunnelObfuscator, logFn func(string, ...any)) *VP8DataTunnel {
	return NewVP8DataTunnelWithQueue(track, obf, logFn, sendQueueDepth)
}

func NewVP8DataTunnelWithQueue(track *webrtc.TrackLocalStaticSample, obf *TunnelObfuscator, logFn func(string, ...any), queueDepth int) *VP8DataTunnel {
	if queueDepth < sendQueueDepth {
		queueDepth = sendQueueDepth
	}
	return &VP8DataTunnel{
		track:           track,
		obf:             obf,
		logFn:           logFn,
		stopCh:          make(chan struct{}),
		sendQueue:       make(chan []byte, queueDepth),
		cfgChan:         make(chan struct{}, 1),
		fps:             defaultVP8FPS,
		batch:           defaultVP8Batch,
		keepaliveMin:    keepaliveIdleMin,
		keepaliveMax:    keepaliveIdleMax,
		keepalivePadMax: keepalivePadMax,
	}
}

func (t *VP8DataTunnel) nextKeepalive(sampleInterval time.Duration) (ticks, padLen int) {
	t.cfgMu.Lock()
	minPeriod, maxPeriod, padMax := t.keepaliveMin, t.keepaliveMax, t.keepalivePadMax
	t.cfgMu.Unlock()
	ticks = int(common.DurationInRange(minPeriod, maxPeriod) / sampleInterval)
	if ticks < 1 {
		ticks = 1
	}
	return ticks, common.IntInRange(0, padMax)
}

func (t *VP8DataTunnel) Reconfigure(fps, batch int) {
	if fps <= 0 && batch <= 0 {
		return
	}
	t.cfgMu.Lock()
	changed := false
	if fps > 0 && t.fps != fps {
		t.fps = fps
		changed = true
	}
	if batch > 0 && t.batch != batch {
		t.batch = batch
		changed = true
	}
	newFPS, newBatch := t.fps, t.batch
	t.cfgMu.Unlock()
	if !changed {
		return
	}
	t.logFn("vp8tunnel: reconfigure fps=%d batch=%d", newFPS, newBatch)
	select {
	case t.cfgChan <- struct{}{}:
	default:
	}
}

func (t *VP8DataTunnel) FPS() int {
	t.cfgMu.Lock()
	defer t.cfgMu.Unlock()
	return t.fps
}

func (t *VP8DataTunnel) Batch() int {
	t.cfgMu.Lock()
	defer t.cfgMu.Unlock()
	return t.batch
}

func (t *VP8DataTunnel) SendData(data []byte) {
	if len(data) == 0 {
		return
	}
	select {
	case t.sendQueue <- data:
	case <-t.stopCh:
	}
}

func (t *VP8DataTunnel) TrySendData(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	select {
	case t.sendQueue <- data:
		return true
	case <-t.stopCh:
		return false
	default:
		return false
	}
}

func (t *VP8DataTunnel) Start(fps, batch int) {
	t.cfgMu.Lock()
	if fps > 0 {
		t.fps = fps
	}
	if batch > 0 {
		t.batch = batch
	}
	t.cfgMu.Unlock()
	if !t.running.CompareAndSwap(false, true) {
		return
	}
	go t.writerLoop()
}

func (t *VP8DataTunnel) Stop() {
	if !t.running.CompareAndSwap(true, false) {
		return
	}
	t.stopOnce.Do(func() { close(t.stopCh) })
	if t.OnClose != nil {
		t.OnClose()
	}
}

func (t *VP8DataTunnel) currentRate() (fps, batch int) {
	t.cfgMu.Lock()
	defer t.cfgMu.Unlock()
	return t.fps, t.batch
}

func sampleIntervalFor(fps, batch int) time.Duration {
	if fps < 1 {
		fps = 1
	}
	frameInterval := time.Second / time.Duration(fps)
	interval := frameInterval
	if batch > 1 {
		interval = frameInterval / time.Duration(batch)
	}
	if interval <= 0 {
		interval = time.Millisecond
	}
	return interval
}

func pacedBatchFor(batch int) int {
	if batch <= 1 {
		return batch
	}
	floor := batch * paceBatchFloorPercent / 100
	if floor < 1 {
		floor = 1
	}
	return common.IntInRange(floor, batch)
}

func (t *VP8DataTunnel) writerLoop() {
	for {
		fps, batch := t.currentRate()
		pacedBatch := pacedBatchFor(batch)
		sampleInterval := sampleIntervalFor(fps, pacedBatch)
		keepaliveEvery, keepalivePad := t.nextKeepalive(sampleInterval)
		t.logFn("vp8tunnel: writer (re)started fps=%d batch=%d pacedBatch=%d sampleInterval=%s keepaliveEvery=%d",
			fps, batch, pacedBatch, sampleInterval, keepaliveEvery)

		ticker := time.NewTicker(sampleInterval)
		drift := time.NewTimer(common.DurationInRange(paceDriftMin, paceDriftMax))
		idle := time.NewTimer(time.Hour)
		if !idle.Stop() {
			<-idle.C
		}
		idleTicks := 0
		spinning := true
		reconfigure := false

		emit := func(sample []byte, isKeepalive bool) {
			if sample == nil {
				return
			}
			if t.WriteFrame != nil {
				if err := t.WriteFrame(sample); err != nil {
					if common.Debug {
						t.logFn("vp8tunnel: WriteFrame error: %v", err)
					}
					return
				}
			} else if err := t.track.WriteSample(media.Sample{Data: sample, Duration: sampleInterval}); err != nil {
				if common.Debug {
					t.logFn("vp8tunnel: WriteSample error: %v", err)
				}
				return
			}
			n := t.sentFrames.Add(1)
			if isKeepalive {
				t.keepaliveFrames.Add(1)
			}
			if common.Debug && (n <= 5 || n%500 == 0) {
				keepalives := t.keepaliveFrames.Load()
				t.logFn("vp8tunnel: sent frame #%d size=%d data=%d keepalive=%d", n, len(sample), n-keepalives, keepalives)
			}
		}

		repace := func() {
			pacedBatch = pacedBatchFor(batch)
			sampleInterval = sampleIntervalFor(fps, pacedBatch)
			if spinning {
				ticker.Reset(sampleInterval)
			}
			keepaliveEvery, keepalivePad = t.nextKeepalive(sampleInterval)
			drift.Reset(common.DurationInRange(paceDriftMin, paceDriftMax))
			if common.Debug {
				t.logFn("vp8tunnel: pace drift pacedBatch=%d/%d sampleInterval=%s", pacedBatch, batch, sampleInterval)
			}
		}

		for !reconfigure {
			if spinning {
				select {
				case <-t.stopCh:
					ticker.Stop()
					drift.Stop()
					idle.Stop()
					return
				case <-t.cfgChan:
					reconfigure = true
				case <-drift.C:
					repace()
				case <-ticker.C:
					select {
					case data := <-t.sendQueue:
						emit(t.obf.EncodeData(data), false)
						idleTicks = 0
					default:
						idleTicks++
						switch {
						case idleTicks >= keepaliveEvery:
							idleTicks = 0
							emit(t.obf.EncodeKeepalive(keepalivePad), true)
							keepaliveEvery, keepalivePad = t.nextKeepalive(sampleInterval)
						case idleTicks >= idleSpinTicks:
							ticker.Stop()
							spinning = false
							idle.Reset(time.Duration(keepaliveEvery-idleTicks) * sampleInterval)
						}
					}
				}
				continue
			}

			select {
			case <-t.stopCh:
				drift.Stop()
				idle.Stop()
				return
			case <-t.cfgChan:
				reconfigure = true
			case <-drift.C:
				repace()
			case data := <-t.sendQueue:
				if !idle.Stop() {
					select {
					case <-idle.C:
					default:
					}
				}
				emit(t.obf.EncodeData(data), false)
				idleTicks = 0
				spinning = true
				ticker.Reset(sampleInterval)
			case <-idle.C:
				idleTicks = 0
				emit(t.obf.EncodeKeepalive(keepalivePad), true)
				keepaliveEvery, keepalivePad = t.nextKeepalive(sampleInterval)
				idle.Reset(time.Duration(keepaliveEvery) * sampleInterval)
			}
		}
		ticker.Stop()
		drift.Stop()
		idle.Stop()
	}
}

func (t *VP8DataTunnel) HandleFrame(frame []byte) {
	res := t.obf.Decode(frame)
	if !res.HasFrame {
		return
	}
	if res.SelfEcho {
		return
	}
	if res.PeerRestart {
		t.logFn("vp8tunnel: peer restart detected, new epoch=0x%08x", res.PeerEpoch)
		if t.OnPeerRestart != nil {
			t.OnPeerRestart()
		}
	}
	if res.Keepalive || len(res.Payload) == 0 {
		return
	}
	n := t.recvFrames.Add(1)
	if common.Debug && (n <= 5 || n%500 == 0) {
		t.logFn("vp8tunnel: recv frame #%d size=%d", n, len(res.Payload))
	}
	if t.OnData != nil {
		t.OnData(res.Payload)
	}
}
