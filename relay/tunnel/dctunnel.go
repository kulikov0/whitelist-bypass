package tunnel

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/datachannel"
	"github.com/pion/webrtc/v4"
)

// Telemost chunk size
const chunkSize = 994
const dcStatsEnabled = false

type chunkBuf struct {
	chunks [][]byte
	count  int
	size   int
}

type DCTunnel struct {
	dc       *webrtc.DataChannel
	raw      datachannel.ReadWriteCloser
	writeRaw datachannel.ReadWriteCloser
	logFn    func(string, ...any)
	onData   func([]byte)
	onClose  func()
	chunked bool
	readBuf int

	recvBufs  sync.Map
	sendMsgID uint32

	recvBytes atomic.Uint64
	sendBytes atomic.Uint64
	recvMsgs  atomic.Uint64
	sendMsgs  atomic.Uint64
}

func NewDCTunnel(dc *webrtc.DataChannel, readBuf int, logFn func(string, ...any)) *DCTunnel {
	t := &DCTunnel{dc: dc, readBuf: readBuf, logFn: logFn}

	raw, err := dc.Detach()
	if err != nil {
		logFn("dctunnel: detach failed, using callback mode: %v", err)
		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			if t.onData != nil {
				t.recvBytes.Add(uint64(len(msg.Data)))
				t.recvMsgs.Add(1)
				frame := make([]byte, 4+len(msg.Data))
				binary.BigEndian.PutUint32(frame[0:4], uint32(len(msg.Data)))
				copy(frame[4:], msg.Data)
				t.onData(frame)
			}
		})
		dc.OnClose(func() {
			if t.onClose != nil {
				t.onClose()
			}
		})
		go t.statsLoop()
		return t
	}

	t.raw = raw
	go t.readLoop()
	go t.statsLoop()
	return t
}

func NewDCTunnelFromRaw(dc *webrtc.DataChannel, raw datachannel.ReadWriteCloser, readBuf int, logFn func(string, ...any)) *DCTunnel {
	t := &DCTunnel{dc: dc, raw: raw, readBuf: readBuf, logFn: logFn}
	go t.readLoop()
	go t.statsLoop()
	return t
}

func NewChunkedDCTunnel(readRaw datachannel.ReadWriteCloser, writeDC *webrtc.DataChannel, readBuf int, logFn func(string, ...any)) *DCTunnel {
	writeRaw, err := writeDC.Detach()
	if err != nil {
		logFn("dctunnel: write DC detach failed: %v", err)
		return nil
	}
	t := &DCTunnel{raw: readRaw, writeRaw: writeRaw, readBuf: readBuf, logFn: logFn, chunked: true}
	go t.readLoop()
	go t.statsLoop()
	return t
}

func (t *DCTunnel) readLoop() {
	buf := make([]byte, t.readBuf)
	for {
		n, isString, err := t.raw.ReadDataChannel(buf)
		if err != nil {
			if err != io.EOF {
				t.logFn("dctunnel: read error: %v", err)
			}
			if t.onClose != nil {
				t.onClose()
			}
			return
		}
		if isString {
			continue
		}
		t.recvBytes.Add(uint64(n))
		t.recvMsgs.Add(1)
		if t.chunked && n >= 6 {
			t.handleChunk(buf[:n])
		} else if n > 0 {
			t.deliverMessage(buf[:n])
		}
	}
}

func (t *DCTunnel) handleChunk(data []byte) {
	id := uint16(data[0])<<8 | uint16(data[1])
	idx := int(uint16(data[2])<<8 | uint16(data[3]))
	total := int(uint16(data[4])<<8 | uint16(data[5]))
	payload := data[6:]

	if total == 1 {
		cp := make([]byte, len(payload))
		copy(cp, payload)
		t.deliverMessage(cp)
		return
	}

	val, _ := t.recvBufs.LoadOrStore(id, &chunkBuf{chunks: make([][]byte, total)})
	cb := val.(*chunkBuf)
	if idx < len(cb.chunks) && cb.chunks[idx] == nil {
		cp := make([]byte, len(payload))
		copy(cp, payload)
		cb.chunks[idx] = cp
		cb.count++
		cb.size += len(cp)
	}
	if cb.count == total {
		t.recvBufs.Delete(id)
		out := make([]byte, 0, cb.size)
		for _, c := range cb.chunks {
			out = append(out, c...)
		}
		t.deliverMessage(out)
	}
}

func (t *DCTunnel) deliverMessage(data []byte) {
	if t.onData != nil && len(data) > 0 {
		frame := make([]byte, 4+len(data))
		binary.BigEndian.PutUint32(frame[0:4], uint32(len(data)))
		copy(frame[4:], data)
		t.onData(frame)
	}
}

func (t *DCTunnel) sendChunked(data []byte) {
	w := t.writeRaw
	if w == nil {
		w = t.raw
	}
	if w == nil {
		return
	}
	total := int(math.Ceil(float64(len(data)) / float64(chunkSize)))
	if total == 0 {
		total = 1
	}
	id := uint16(atomic.AddUint32(&t.sendMsgID, 1)) & 0xFFFF
	for i := 0; i < total; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > len(data) {
			end = len(data)
		}
		p := data[start:end]
		f := make([]byte, 6+len(p))
		f[0] = byte(id >> 8)
		f[1] = byte(id & 0xFF)
		f[2] = byte(i >> 8)
		f[3] = byte(i & 0xFF)
		f[4] = byte(total >> 8)
		f[5] = byte(total & 0xFF)
		copy(f[6:], p)
		t.sendBytes.Add(uint64(len(f)))
		t.sendMsgs.Add(1)
		w.Write(f)
	}
}

func (t *DCTunnel) sendRaw(data []byte) {
	w := t.writeRaw
	if w == nil {
		w = t.raw
	}
	if w != nil {
		t.sendBytes.Add(uint64(len(data)))
		t.sendMsgs.Add(1)
		w.Write(data)
		return
	}
	if t.dc == nil || t.dc.ReadyState() != webrtc.DataChannelStateOpen {
		return
	}
	t.sendBytes.Add(uint64(len(data)))
	t.sendMsgs.Add(1)
	t.dc.Send(data)
}

func (t *DCTunnel) SendData(data []byte) {
	DecodeFrames(data, func(connID uint32, msgType byte, payload []byte) {
		buf := make([]byte, 5+len(payload))
		binary.BigEndian.PutUint32(buf[0:4], connID)
		buf[4] = msgType
		copy(buf[5:], payload)
		if t.chunked {
			t.sendChunked(buf)
		} else {
			t.sendRaw(buf)
		}
	})
}

func (t *DCTunnel) SetOnData(fn func([]byte))  { t.onData = fn }
func (t *DCTunnel) SetOnClose(fn func())        { t.onClose = fn }

func (t *DCTunnel) statsLoop() {
	if !dcStatsEnabled {
		return
	}
	var lastRecv, lastSend uint64
	var lastRecvMsgs, lastSendMsgs uint64
	for {
		time.Sleep(2 * time.Second)
		recv := t.recvBytes.Load()
		send := t.sendBytes.Load()
		recvMsgs := t.recvMsgs.Load()
		sendMsgs := t.sendMsgs.Load()
		recvDelta := recv - lastRecv
		sendDelta := send - lastSend
		recvMsgsDelta := recvMsgs - lastRecvMsgs
		sendMsgsDelta := sendMsgs - lastSendMsgs
		lastRecv = recv
		lastSend = send
		lastRecvMsgs = recvMsgs
		lastSendMsgs = sendMsgs
		if recvDelta > 0 || sendDelta > 0 {
			fmt.Printf("DC-STATS: recv=%dKB/s(%dmsg) send=%dKB/s(%dmsg)\n",
				recvDelta/2/1024, recvMsgsDelta/2,
				sendDelta/2/1024, sendMsgsDelta/2)
		}
	}
}
