package tunnel

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"time"

	kcp "github.com/xtaci/kcp-go/v5"

	"whitelist-bypass/relay/common"
)

const (
	kcpConvBase           = 0x77627374
	kcpUpdateInterval     = 10 * time.Millisecond
	kcpIdleUpdateInterval = 500 * time.Millisecond
	kcpIdleAfterTicks     = 50
	// One KCP segment must ride in a single RTP packet so a dropped packet
	// loses only its own frame, not a two-packet frame that readVP8Track
	// would discard whole. 1200 RTP budget - 1 VP8 descriptor - interframe
	// header - 24 XChaCha20 nonce - 16 Poly1305 tag - 1 channel tag.
	kcpSegmentMTU     = 1200 - 1 - interframeHdrLen - 24 - 16 - 1
	kcpReceiveBufSize = 128 * 1024
	kcpStatsEvery     = 500

	kcpWindowFloor      = 64
	kcpWindowCeiling    = 512
	kcpCarrierRTT       = 250 * time.Millisecond
	kcpWaitSndFactor    = 2
	kcpBackpressurePoll = 2 * time.Millisecond

	kcpChannelReliable byte = 0x00
	kcpChannelRaw      byte = 0x01

	KCPCarrierQueueDepth = kcpWaitSndFactor * kcpWindowCeiling
)

func computeKCPWindow(fps, batch int) int {
	rate := fps * batch
	if rate < 1 {
		rate = defaultVP8FPS * defaultVP8Batch
	}
	window := int(float64(rate) * kcpCarrierRTT.Seconds())
	if window < kcpWindowFloor {
		return kcpWindowFloor
	}
	if window > kcpWindowCeiling {
		return kcpWindowCeiling
	}
	return window
}

type trackKCPSession struct {
	conv    uint32
	vp8     *VP8DataTunnel
	parent  *MultiTrackKCPTunnel
	kcpMu   sync.Mutex
	kcp     *kcp.KCP
	recvBuf []byte
}

func newTrackKCPSession(parent *MultiTrackKCPTunnel, vp8 *VP8DataTunnel, conv uint32, window int) *trackKCPSession {
	session := &trackKCPSession{
		conv:    conv,
		vp8:     vp8,
		parent:  parent,
		recvBuf: make([]byte, kcpReceiveBufSize),
	}
	session.kcp = kcp.NewKCP(conv, func(buf []byte, size int) {
		if size <= 0 {
			return
		}
		segment := make([]byte, size+1)
		segment[0] = kcpChannelReliable
		copy(segment[1:], buf[:size])
		parent.outputSegments.Add(1)
		if !session.vp8.TrySendData(segment) {
			parent.droppedSegments.Add(1)
		}
	})
	session.kcp.NoDelay(1, 10, 2, 1)
	session.kcp.WndSize(window, window)
	session.kcp.SetMtu(kcpSegmentMTU)
	return session
}

func (s *trackKCPSession) setWindow(window int) {
	s.kcpMu.Lock()
	s.kcp.WndSize(window, window)
	s.kcpMu.Unlock()
}

func (s *trackKCPSession) send(frame []byte) {
	s.kcpMu.Lock()
	s.kcp.Send(frame)
	s.kcp.Update()
	s.kcpMu.Unlock()
}

func (s *trackKCPSession) input(segment []byte) [][]byte {
	s.kcpMu.Lock()
	s.kcp.Input(segment, kcp.IKCP_PACKET_REGULAR, true)
	var messages [][]byte
	for {
		size := s.kcp.PeekSize()
		if size <= 0 {
			break
		}
		if size > len(s.recvBuf) {
			s.recvBuf = make([]byte, size)
		}
		n := s.kcp.Recv(s.recvBuf)
		if n <= 0 {
			break
		}
		message := make([]byte, n)
		copy(message, s.recvBuf[:n])
		messages = append(messages, message)
	}
	s.kcpMu.Unlock()
	return messages
}

func (s *trackKCPSession) update() {
	s.kcpMu.Lock()
	s.kcp.Update()
	s.kcpMu.Unlock()
}

func (s *trackKCPSession) waitSnd() int {
	s.kcpMu.Lock()
	pending := s.kcp.WaitSnd()
	s.kcpMu.Unlock()
	return pending
}

type MultiTrackKCPTunnel struct {
	mt    *MultiTrackTunnel
	logFn func(string, ...any)

	mu       sync.Mutex
	sessions []*trackKCPSession
	convMap  map[uint32]*trackKCPSession
	connPin  map[uint32]int
	onData   func([]byte)
	onClose  func()

	stopCh   chan struct{}
	stopOnce sync.Once
	nudge    chan struct{}

	currentWindow atomic.Int32

	sentMessages      atomic.Uint64
	deliveredMessages atomic.Uint64
	outputSegments    atomic.Uint64
	inputSegments     atomic.Uint64
	rawSent           atomic.Uint64
	rawReceived       atomic.Uint64
	droppedSegments   atomic.Uint64
}

func NewMultiTrackKCPTunnel(mt *MultiTrackTunnel, logFn func(string, ...any)) *MultiTrackKCPTunnel {
	t := &MultiTrackKCPTunnel{
		mt:      mt,
		logFn:   logFn,
		convMap: make(map[uint32]*trackKCPSession),
		connPin: make(map[uint32]int),
		stopCh:  make(chan struct{}),
		nudge:   make(chan struct{}, 1),
	}
	subs := mt.SubTunnels()
	window := kcpWindowFloor
	if len(subs) > 0 {
		window = computeKCPWindow(subs[0].FPS(), subs[0].Batch())
	}
	t.currentWindow.Store(int32(window))
	for i, sub := range subs {
		conv := uint32(kcpConvBase + i)
		session := newTrackKCPSession(t, sub, conv, window)
		t.sessions = append(t.sessions, session)
		t.convMap[conv] = session
	}
	if logFn != nil {
		logFn("kcptunnel: init tracks=%d window=%d queue=%d", len(subs), window, KCPCarrierQueueDepth)
	}
	mt.SetOnData(t.handleDecodedSegment)
	mt.SetOnClose(t.handleInnerClose)
	go t.updateLoop()
	return t
}

func (t *MultiTrackKCPTunnel) SendData(frame []byte) {
	if len(frame) < 9 {
		return
	}
	connID := binary.BigEndian.Uint32(frame[4:8])
	msgType := frame[8]

	if msgType == MsgUDP || msgType == MsgUDPReply {
		t.sendRaw(connID, frame)
		return
	}
	t.wake()

	t.mu.Lock()
	if len(t.sessions) == 0 {
		t.mu.Unlock()
		return
	}
	index, pinned := t.connPin[connID]
	if !pinned || index >= len(t.sessions) {
		index = int(connID % uint32(len(t.sessions)))
		t.connPin[connID] = index
	}
	session := t.sessions[index]
	t.mu.Unlock()

	if msgType == MsgData {
		sndCap := int(t.currentWindow.Load()) * kcpWaitSndFactor
		for session.waitSnd() >= sndCap {
			select {
			case <-t.stopCh:
				return
			case <-time.After(kcpBackpressurePoll):
			}
		}
	}

	t.sentMessages.Add(1)
	session.send(frame)

	if msgType == MsgClose {
		t.mu.Lock()
		delete(t.connPin, connID)
		t.mu.Unlock()
	}
}

func (t *MultiTrackKCPTunnel) sendRaw(connID uint32, frame []byte) {
	t.mu.Lock()
	if len(t.sessions) == 0 {
		t.mu.Unlock()
		return
	}
	index := int(connID % uint32(len(t.sessions)))
	session := t.sessions[index]
	t.mu.Unlock()

	segment := make([]byte, len(frame)+1)
	segment[0] = kcpChannelRaw
	copy(segment[1:], frame)
	t.rawSent.Add(1)
	session.vp8.TrySendData(segment)
}

func (t *MultiTrackKCPTunnel) InjectSegment(payload []byte) {
	t.handleDecodedSegment(payload)
}

func (t *MultiTrackKCPTunnel) handleDecodedSegment(payload []byte) {
	if len(payload) < 1 {
		return
	}
	channel := payload[0]
	body := payload[1:]

	if channel == kcpChannelRaw {
		t.mu.Lock()
		callback := t.onData
		t.mu.Unlock()
		if callback == nil {
			return
		}
		t.rawReceived.Add(1)
		callback(body)
		return
	}

	if len(body) < 4 {
		return
	}
	conv := binary.LittleEndian.Uint32(body[0:4])
	t.mu.Lock()
	session := t.convMap[conv]
	callback := t.onData
	t.mu.Unlock()
	if session == nil {
		return
	}
	t.inputSegments.Add(1)
	t.wake()
	messages := session.input(body)
	if callback == nil {
		return
	}
	for _, message := range messages {
		t.deliveredMessages.Add(1)
		callback(message)
	}
}

func (t *MultiTrackKCPTunnel) SetOnData(fn func([]byte)) {
	t.mu.Lock()
	t.onData = fn
	t.mu.Unlock()
}

func (t *MultiTrackKCPTunnel) SetOnClose(fn func()) {
	t.mu.Lock()
	t.onClose = fn
	t.mu.Unlock()
}

func (t *MultiTrackKCPTunnel) Reconfigure(fps, batch int) {
	t.mt.Reconfigure(fps, batch)
	window := computeKCPWindow(fps, batch)
	t.applyWindow(window)
	if t.logFn != nil {
		t.logFn("kcptunnel: reconfigure fps=%d batch=%d -> window=%d", fps, batch, window)
	}
}

func (t *MultiTrackKCPTunnel) applyWindow(window int) {
	t.currentWindow.Store(int32(window))
	t.mu.Lock()
	sessions := make([]*trackKCPSession, len(t.sessions))
	copy(sessions, t.sessions)
	t.mu.Unlock()
	for _, session := range sessions {
		session.setWindow(window)
	}
}

func (t *MultiTrackKCPTunnel) AddSession(sub *VP8DataTunnel) {
	window := int(t.currentWindow.Load())
	t.mu.Lock()
	conv := uint32(kcpConvBase + len(t.sessions))
	session := newTrackKCPSession(t, sub, conv, window)
	t.sessions = append(t.sessions, session)
	t.convMap[conv] = session
	t.mu.Unlock()
}

func (t *MultiTrackKCPTunnel) RemoveLastSession() {
	t.mu.Lock()
	if len(t.sessions) <= 1 {
		t.mu.Unlock()
		return
	}
	last := t.sessions[len(t.sessions)-1]
	t.sessions = t.sessions[:len(t.sessions)-1]
	delete(t.convMap, last.conv)
	t.mu.Unlock()
}

func (t *MultiTrackKCPTunnel) Stop() {
	t.stopOnce.Do(func() { close(t.stopCh) })
	t.mt.Stop()
}

func (t *MultiTrackKCPTunnel) StopLayer() {
	t.stopOnce.Do(func() { close(t.stopCh) })
}

func (t *MultiTrackKCPTunnel) handleInnerClose() {
	t.stopOnce.Do(func() { close(t.stopCh) })
	t.mu.Lock()
	callback := t.onClose
	t.mu.Unlock()
	if callback != nil {
		callback()
	}
}

func (t *MultiTrackKCPTunnel) wake() {
	select {
	case t.nudge <- struct{}{}:
	default:
	}
}

func (t *MultiTrackKCPTunnel) updateLoop() {
	ticker := time.NewTicker(kcpUpdateInterval)
	defer ticker.Stop()
	ticks := 0
	idleTicks := 0
	fast := true
	for {
		select {
		case <-t.stopCh:
			return
		case <-t.nudge:
			if !fast {
				fast = true
				idleTicks = 0
				ticker.Reset(kcpUpdateInterval)
			}
		case <-ticker.C:
			t.mu.Lock()
			sessions := make([]*trackKCPSession, len(t.sessions))
			copy(sessions, t.sessions)
			t.mu.Unlock()
			pending := 0
			for _, session := range sessions {
				session.update()
				pending += session.waitSnd()
			}
			if pending > 0 {
				idleTicks = 0
				if !fast {
					fast = true
					ticker.Reset(kcpUpdateInterval)
				}
			} else if fast {
				idleTicks++
				if idleTicks >= kcpIdleAfterTicks {
					fast = false
					idleTicks = 0
					ticker.Reset(kcpIdleUpdateInterval)
				}
			}
			ticks++
			if common.Debug && ticks%kcpStatsEvery == 0 && t.logFn != nil {
				t.logFn("kcptunnel: sessions=%d window=%d sent=%d delivered=%d out_segs=%d in_segs=%d raw_out=%d raw_in=%d dropped=%d",
					len(sessions), t.currentWindow.Load(), t.sentMessages.Load(), t.deliveredMessages.Load(),
					t.outputSegments.Load(), t.inputSegments.Load(),
					t.rawSent.Load(), t.rawReceived.Load(), t.droppedSegments.Load())
				snmp := kcp.DefaultSnmp.Copy()
				t.logFn("kcptunnel: kcp_out=%d kcp_in=%d retrans=%d fastretrans=%d lost=%d repeat=%d",
					snmp.OutSegs, snmp.InSegs, snmp.RetransSegs, snmp.FastRetransSegs, snmp.LostSegs, snmp.RepeatSegs)
			}
		}
	}
}
