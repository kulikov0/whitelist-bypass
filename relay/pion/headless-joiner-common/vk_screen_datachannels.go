package joiner

import (
	"errors"
	"io"
	"sync"

	"github.com/pion/datachannel"
	"github.com/kulikov0/headless-client/webrtc"
	"whitelist-bypass/relay/common"
)

var errScreenNotReady = errors.New("screen DC not ready")

type screenUplink struct {
	mu  sync.Mutex
	raw io.WriteCloser
}

func (u *screenUplink) attach(raw io.WriteCloser) {
	u.mu.Lock()
	u.raw = raw
	u.mu.Unlock()
}

func (u *screenUplink) ready() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.raw != nil
}

func (u *screenUplink) send(b []byte) error {
	u.mu.Lock()
	raw := u.raw
	u.mu.Unlock()
	if raw == nil {
		return errScreenNotReady
	}
	_, err := raw.Write(b)
	if err != nil {
		u.mu.Lock()
		if u.raw == raw {
			u.raw = nil
		}
		u.mu.Unlock()
	}
	return err
}

func (u *screenUplink) reset() {
	u.mu.Lock()
	if u.raw != nil {
		u.raw.Close()
		u.raw = nil
	}
	u.mu.Unlock()
}

func readScreenDataChannel(dc *webrtc.DataChannel, handler func([]byte), logFn func(string, ...any)) {
	dc.OnOpen(func() {
		var raw datachannel.ReadWriteCloser
		raw, err := dc.Detach()
		if err != nil {
			logFn("vk-joiner: screen DC detach failed, using OnMessage: %v", err)
			dc.OnMessage(func(m webrtc.DataChannelMessage) {
				if !m.IsString && len(m.Data) > 0 {
					frame := make([]byte, len(m.Data))
					copy(frame, m.Data)
					handler(frame)
				}
			})
			return
		}
		logFn("vk-joiner: screen DC attached for reading")
		buf := make([]byte, common.RTPBufSize)
		for {
			n, isString, rerr := raw.ReadDataChannel(buf)
			if rerr != nil {
				return
			}
			if isString || n == 0 {
				continue
			}
			frame := make([]byte, n)
			copy(frame, buf[:n])
			handler(frame)
		}
	})
}

func attachScreenWriterDC(dc *webrtc.DataChannel, onRaw func(io.WriteCloser), logFn func(string, ...any)) {
	dc.OnOpen(func() {
		raw, err := dc.Detach()
		if err != nil {
			logFn("vk-joiner: screen writer DC detach failed: %v", err)
			return
		}
		logFn("vk-joiner: screen DC attached for writing")
		onRaw(raw)
	})
}
