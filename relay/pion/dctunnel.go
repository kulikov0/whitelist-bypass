package pion

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"github.com/pion/datachannel"
	"github.com/pion/webrtc/v4"
)

type DCTunnel struct {
	dc      *webrtc.DataChannel
	raw     datachannel.ReadWriteCloser
	logFn   func(string, ...any)
	onData  func([]byte)
	onClose func()

	recvBytes atomic.Uint64
	sendBytes atomic.Uint64
	recvMsgs  atomic.Uint64
	sendMsgs  atomic.Uint64
}

func NewDCTunnel(dc *webrtc.DataChannel, logFn func(string, ...any)) *DCTunnel {
	t := &DCTunnel{dc: dc, logFn: logFn}

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

func (t *DCTunnel) readLoop() {
	buf := make([]byte, 65536)
	for {
		n, _, err := t.raw.ReadDataChannel(buf)
		if err != nil {
			if err != io.EOF {
				t.logFn("dctunnel: read error: %v", err)
			}
			if t.onClose != nil {
				t.onClose()
			}
			return
		}
		if t.onData != nil && n > 0 {
			t.recvBytes.Add(uint64(n))
			t.recvMsgs.Add(1)
			frame := make([]byte, 4+n)
			binary.BigEndian.PutUint32(frame[0:4], uint32(n))
			copy(frame[4:], buf[:n])
			t.onData(frame)
		}
	}
}

func (t *DCTunnel) statsLoop() {
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

func (t *DCTunnel) SendData(data []byte) {
	if t.raw != nil {
		decodeFrames(data, func(connID uint32, msgType byte, payload []byte) {
			buf := make([]byte, 5+len(payload))
			binary.BigEndian.PutUint32(buf[0:4], connID)
			buf[4] = msgType
			copy(buf[5:], payload)
			t.sendBytes.Add(uint64(len(buf)))
			t.sendMsgs.Add(1)
			t.raw.Write(buf)
		})
		return
	}
	if t.dc == nil || t.dc.ReadyState() != webrtc.DataChannelStateOpen {
		return
	}
	decodeFrames(data, func(connID uint32, msgType byte, payload []byte) {
		buf := make([]byte, 5+len(payload))
		binary.BigEndian.PutUint32(buf[0:4], connID)
		buf[4] = msgType
		copy(buf[5:], payload)
		t.sendBytes.Add(uint64(len(buf)))
		t.sendMsgs.Add(1)
		t.dc.Send(buf)
	})
}

func (t *DCTunnel) SetOnData(fn func([]byte))  { t.onData = fn }
func (t *DCTunnel) SetOnClose(fn func())        { t.onClose = fn }
