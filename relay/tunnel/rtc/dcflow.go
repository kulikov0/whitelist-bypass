package rtc

import (
	"sync"
	"time"

	"github.com/kulikov0/headless-client/webrtc"
)

const dcFlowRecheckInterval = 100 * time.Millisecond

type DCFlowGate struct {
	dc        *webrtc.DataChannel
	highWater uint64
	lowWater  uint64
	mu        sync.Mutex
	lowSignal chan struct{}
}

func NewDCFlowGate(dc *webrtc.DataChannel, maxDCBuf uint64) *DCFlowGate {
	gate := &DCFlowGate{
		dc:        dc,
		highWater: maxDCBuf,
		lowWater:  maxDCBuf / 2,
		lowSignal: make(chan struct{}, 1),
	}
	dc.SetBufferedAmountLowThreshold(gate.lowWater)
	dc.OnBufferedAmountLow(func() {
		select {
		case gate.lowSignal <- struct{}{}:
		default:
		}
	})
	return gate
}

func (g *DCFlowGate) Acquire() {
	g.mu.Lock()
	if g.dc.BufferedAmount() <= g.highWater {
		return
	}
	for g.dc.BufferedAmount() > g.lowWater && g.dc.ReadyState() == webrtc.DataChannelStateOpen {
		select {
		case <-g.lowSignal:
		case <-time.After(dcFlowRecheckInterval):
		}
	}
}

func (g *DCFlowGate) Release() {
	g.mu.Unlock()
}
