package tunnel

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kulikov0/headlessclient/webrtc"
	"github.com/kulikov0/headlessclient/webrtc/pkg/media"

	"whitelist-bypass/relay/common"
)

var legacyVP8Keyframe = []byte{
	16, 2, 0, 157, 1, 42, 2, 0, 2, 0, 2, 7, 8, 133, 133, 136,
	153, 132, 136, 11, 2, 0, 12, 13, 96, 0, 254, 252, 173, 16,
}

var legacyVP8Interframe = []byte{
	177, 1, 0, 8, 17, 24, 0, 24, 0, 24, 88, 47, 244, 0, 8, 0, 0,
}

const (
	legacyDataMarker    byte = 0xCD
	legacyVP8InterLen        = 17
	legacyDataHeaderLen      = legacyVP8InterLen + 1 + 4
	legacyKeyframeEvery      = 60
)

type VP8LegacyTunnel struct {
	track     *webrtc.TrackLocalStaticSample
	logFn     func(string, ...any)
	stopCh    chan struct{}
	sendQueue chan []byte
	cfgChan   chan struct{}

	stopOnce sync.Once
	running  atomic.Bool

	cfgMu sync.Mutex
	fps   int
	batch int

	sentFrames atomic.Uint64
	recvFrames atomic.Uint64

	OnData  func([]byte)
	OnClose func()
}

func (t *VP8LegacyTunnel) SetOnData(fn func([]byte)) { t.OnData = fn }
func (t *VP8LegacyTunnel) SetOnClose(fn func())      { t.OnClose = fn }

func NewVP8LegacyTunnel(track *webrtc.TrackLocalStaticSample, logFn func(string, ...any)) *VP8LegacyTunnel {
	return &VP8LegacyTunnel{
		track:     track,
		logFn:     logFn,
		stopCh:    make(chan struct{}),
		sendQueue: make(chan []byte, sendQueueDepth),
		cfgChan:   make(chan struct{}, 1),
		fps:       defaultVP8FPS,
		batch:     defaultVP8Batch,
	}
}

func (t *VP8LegacyTunnel) SendData(data []byte) {
	if len(data) == 0 {
		return
	}
	select {
	case t.sendQueue <- data:
	case <-t.stopCh:
	}
}

func (t *VP8LegacyTunnel) Reconfigure(fps, batch int) {
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
	t.logFn("vp8tunnel-legacy: reconfigure fps=%d batch=%d", newFPS, newBatch)
	select {
	case t.cfgChan <- struct{}{}:
	default:
	}
}

func (t *VP8LegacyTunnel) Start(fps, batch int) {
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

func (t *VP8LegacyTunnel) Stop() {
	if !t.running.CompareAndSwap(true, false) {
		return
	}
	t.stopOnce.Do(func() { close(t.stopCh) })
	if t.OnClose != nil {
		t.OnClose()
	}
}

func (t *VP8LegacyTunnel) currentIntervals() (sampleInterval time.Duration, keepaliveEvery, fps, batch int) {
	t.cfgMu.Lock()
	fps = t.fps
	batch = t.batch
	t.cfgMu.Unlock()

	frameInterval := time.Second / time.Duration(fps)
	sampleInterval = frameInterval
	if batch > 1 {
		sampleInterval = frameInterval / time.Duration(batch)
	}
	if sampleInterval <= 0 {
		sampleInterval = time.Millisecond
	}

	keepaliveEvery = int(keepaliveIdlePeriod / sampleInterval)
	if keepaliveEvery < 1 {
		keepaliveEvery = 1
	}
	return
}

func encodeLegacyData(payload []byte) []byte {
	frame := make([]byte, legacyDataHeaderLen+len(payload))
	copy(frame, legacyVP8Interframe)
	frame[len(legacyVP8Interframe)] = legacyDataMarker
	binary.BigEndian.PutUint32(frame[len(legacyVP8Interframe)+1:], uint32(len(payload)))
	copy(frame[legacyDataHeaderLen:], payload)
	return frame
}

func (t *VP8LegacyTunnel) writerLoop() {
	for {
		sampleInterval, keepaliveEvery, fps, batch := t.currentIntervals()
		t.logFn("vp8tunnel-legacy: writer (re)started fps=%d batch=%d sampleInterval=%s keepaliveEvery=%d",
			fps, batch, sampleInterval, keepaliveEvery)

		ticker := time.NewTicker(sampleInterval)
		idleTicks := 0
		reconfigure := false

		for !reconfigure {
			select {
			case <-t.stopCh:
				ticker.Stop()
				return
			case <-t.cfgChan:
				reconfigure = true
			case <-ticker.C:
				var sample []byte
				select {
				case data := <-t.sendQueue:
					sample = encodeLegacyData(data)
					idleTicks = 0
				default:
					idleTicks++
					if idleTicks < keepaliveEvery {
						continue
					}
					idleTicks = 0
					if (t.sentFrames.Load()+1)%legacyKeyframeEvery == 0 {
						sample = legacyVP8Keyframe
					} else {
						sample = legacyVP8Interframe
					}
				}
				if err := t.track.WriteSample(media.Sample{Data: sample, Duration: sampleInterval}); err != nil {
					if common.Debug {
						t.logFn("vp8tunnel-legacy: WriteSample error: %v", err)
					}
					continue
				}
				n := t.sentFrames.Add(1)
				if common.Debug && (n <= 5 || n%500 == 0) {
					t.logFn("vp8tunnel-legacy: sent frame #%d size=%d", n, len(sample))
				}
			}
		}
		ticker.Stop()
	}
}

func extractLegacyPayload(frame []byte) []byte {
	if len(frame) < legacyDataHeaderLen {
		return nil
	}
	for i := range legacyVP8Interframe {
		if frame[i] != legacyVP8Interframe[i] {
			return nil
		}
	}
	if frame[len(legacyVP8Interframe)] != legacyDataMarker {
		return nil
	}
	payloadLen := binary.BigEndian.Uint32(frame[len(legacyVP8Interframe)+1 : legacyDataHeaderLen])
	if payloadLen == 0 || int(payloadLen) > len(frame)-legacyDataHeaderLen {
		return nil
	}
	return frame[legacyDataHeaderLen : legacyDataHeaderLen+int(payloadLen)]
}

func (t *VP8LegacyTunnel) HandleFrame(frame []byte) {
	payload := extractLegacyPayload(frame)
	if payload == nil {
		return
	}
	n := t.recvFrames.Add(1)
	if common.Debug && (n <= 5 || n%500 == 0) {
		t.logFn("vp8tunnel-legacy: recv frame #%d size=%d", n, len(payload))
	}
	if t.OnData != nil {
		t.OnData(payload)
	}
}
