package tunnel

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

var vp8Keyframe = []byte{
	16, 2, 0, 157, 1, 42, 2, 0, 2, 0, 2, 7, 8, 133, 133, 136,
	153, 132, 136, 11, 2, 0, 12, 13, 96, 0, 254, 252, 173, 16,
}

var vp8Interframe = []byte{
	177, 1, 0, 8, 17, 24, 0, 24, 0, 24, 88, 47, 244, 0, 8, 0, 0,
}

type VP8DataTunnel struct {
	track      *webrtc.TrackLocalStaticSample
	mu         sync.Mutex
	logFn      func(string, ...any)
	frameCount uint64
	running    bool
	stopCh     chan struct{}
	sendQueue  chan []byte
	OnData     func([]byte)
	OnClose    func()
}

func NewVP8DataTunnel(track *webrtc.TrackLocalStaticSample, logFn func(string, ...any)) *VP8DataTunnel {
	return &VP8DataTunnel{
		track:     track,
		logFn:     logFn,
		stopCh:    make(chan struct{}),
		sendQueue: make(chan []byte, 256),
	}
}

const DataFrameMarker = 0xFF

func (t *VP8DataTunnel) buildFrame(data []byte) []byte {
	t.frameCount++
	if len(data) == 0 {
		if t.frameCount%60 == 0 {
			return vp8Keyframe
		}
		return vp8Interframe
	}
	frame := make([]byte, 1+4+len(data))
	frame[0] = DataFrameMarker
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(data)))
	copy(frame[5:], data)
	return frame
}

var sendCount atomic.Uint64

func (t *VP8DataTunnel) SendData(data []byte) {
	n := sendCount.Add(1)
	if n <= 5 || n%100 == 0 {
		t.logFn("vp8tunnel: SendData #%d len=%d queueLen=%d", n, len(data), len(t.sendQueue))
	}
	t.sendQueue <- data
}

func (t *VP8DataTunnel) Start(fps int) {
	t.running = true
	keepaliveInterval := time.Second / time.Duration(fps)
	lastSend := time.Now()

	go func() {
		ticker := time.NewTicker(keepaliveInterval)
		defer ticker.Stop()
		for {
			select {
			case <-t.stopCh:
				return
			case data := <-t.sendQueue:
				now := time.Now()
				elapsed := now.Sub(lastSend)
				minInterval := 5 * time.Millisecond
				if elapsed < minInterval {
					time.Sleep(minInterval - elapsed)
				}
				lastSend = time.Now()
				frame := t.buildFrame(data)
				err := t.track.WriteSample(media.Sample{Data: frame, Duration: keepaliveInterval})
				if err != nil {
					t.logFn("vp8tunnel: WriteSample DATA error: %v (frame %d, %d bytes)", err, t.frameCount-1, len(frame))
				} else if t.frameCount <= 10 || t.frameCount%100 == 0 {
					t.logFn("vp8tunnel: WriteSample DATA ok frame=%d size=%d dataLen=%d first=0x%02x", t.frameCount-1, len(frame), len(data), frame[0])
				}
				if t.frameCount%60 == 0 {
					kf := t.buildFrame(nil)
					t.track.WriteSample(media.Sample{Data: kf, Duration: keepaliveInterval})
				}
				ticker.Reset(keepaliveInterval)
			case <-ticker.C:
				lastSend = time.Now()
				frame := t.buildFrame(nil)
				err := t.track.WriteSample(media.Sample{Data: frame, Duration: keepaliveInterval})
				if t.frameCount <= 3 || t.frameCount%500 == 0 {
					t.logFn("vp8tunnel: KEEPALIVE frame=%d first=0x%02x err=%v", t.frameCount-1, frame[0], err)
				}
			}
		}
	}()
}

func (t *VP8DataTunnel) Stop() {
	if t.running {
		close(t.stopCh)
		t.running = false
		if t.OnClose != nil {
			t.OnClose()
		}
	}
}

func ExtractDataFromPayload(payload []byte) []byte {
	if len(payload) < 5 {
		return nil
	}
	if payload[0] != DataFrameMarker {
		return nil
	}
	dataLen := binary.BigEndian.Uint32(payload[1:5])
	if dataLen == 0 || int(dataLen) > len(payload)-5 {
		return nil
	}
	return payload[5 : 5+dataLen]
}
