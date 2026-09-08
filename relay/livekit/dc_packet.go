package livekit

import (
	"github.com/pion/datachannel"
)

type DataPacketWrapper struct {
	inner datachannel.ReadWriteCloser
	kind  int
}

func NewDataPacketWrapper(inner datachannel.ReadWriteCloser, kind int) *DataPacketWrapper {
	return &DataPacketWrapper{inner: inner, kind: kind}
}

func (w *DataPacketWrapper) ReadDataChannel(p []byte) (int, bool, error) {
	buf := make([]byte, len(p))
	for {
		n, isString, err := w.inner.ReadDataChannel(buf)
		if err != nil {
			return 0, false, err
		}
		if n == 0 {
			continue
		}
		payload, ok := DecodeDataPacketUser(buf[:n])
		if !ok || len(payload) == 0 {
			continue
		}
		copied := copy(p, payload)
		return copied, isString, nil
	}
}

func (w *DataPacketWrapper) WriteDataChannel(p []byte, isString bool) (int, error) {
	wire := EncodeDataPacketUser(p, w.kind)
	if _, err := w.inner.WriteDataChannel(wire, isString); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (w *DataPacketWrapper) Read(p []byte) (int, error) {
	n, _, err := w.ReadDataChannel(p)
	return n, err
}

func (w *DataPacketWrapper) Write(p []byte) (int, error) {
	return w.WriteDataChannel(p, false)
}

func (w *DataPacketWrapper) Close() error { return w.inner.Close() }
