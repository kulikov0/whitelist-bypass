package wtsignal

import (
	"bufio"
	"bytes"
	"compress/flate"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	headless "github.com/kulikov0/headless-client"
	"github.com/kulikov0/headless-client/quic"
	"github.com/kulikov0/headless-client/quic/http3"
	"github.com/kulikov0/headless-client/quic/quicvarint"
)

const (
	dialTimeout     = 15 * time.Second
	keepAlivePeriod = 15 * time.Second
	maxIdleTimeout  = 30 * time.Second
	maxMessageSize  = 8 << 20

	webTransportFrameType     uint64 = 0x41
	webTransportUniStreamType uint64 = 0x54

	settingsEnableWebtransportDraft06  = 0x2b603742
	settingsWebTransportEnabled        = 0x2c7cf000
	settingsWebTransportMaxSessions    = 0x14e9cd29
	settingsWebTransportMaxSessionsStd = 0xc671706a

	closeSessionCapsuleType http3.CapsuleType = 0x2843

	protocolHeaderLegacy = "webtransport"
)

type Conn struct {
	conn     *quic.Conn
	stream   *quic.Stream
	reader   *bufio.Reader
	compress bool
	writeMu  sync.Mutex
}

func Dial(endpoint, serverName, resolvedIP string) (*Conn, error) {
	target, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	port := target.Port()
	if port == "" {
		port = "443"
	}
	compress := target.Query().Get("compression") == "deflate-raw"

	dialCtx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()

	qconn, err := headless.ChromeWindows.DialQUIC(dialCtx, net.JoinHostPort(resolvedIP, port), headless.QUICOptions{
		Transport:          headless.QUICWebTransport,
		ServerName:         serverName,
		InsecureSkipVerify: true,
		EnableDatagrams:    true,
		KeepAlivePeriod:    keepAlivePeriod,
		MaxIdleTimeout:     maxIdleTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("wt dial: %w", err)
	}

	// The VK/OK SFU advertises the HTTP/3 datagram setting but does not
	// negotiate QUIC transport-level datagrams, which makes quic-go's http3
	// layer close the connection. Signaling only uses WebTransport streams, so
	// disable HTTP/3 datagrams on our side, and send the draft-06
	// ENABLE_WEBTRANSPORT codepoint the SFU expects.
	tr := &http3.Transport{
		EnableDatagrams:    false,
		AdditionalSettings: map[uint64]uint64{settingsEnableWebtransportDraft06: 1},
	}
	control := tr.NewRawClientConn(qconn)
	context.AfterFunc(qconn.Context(), func() { tr.Close() })

	go acceptStreams(qconn, control)
	go acceptUniStreams(qconn, control)

	select {
	case <-control.ReceivedSettings():
	case <-dialCtx.Done():
		qconn.CloseWithError(0, "")
		return nil, fmt.Errorf("wt settings: %w", dialCtx.Err())
	}
	settings := control.Settings()
	if !settings.EnableExtendedConnect {
		qconn.CloseWithError(0, "")
		return nil, fmt.Errorf("wt: server did not enable extended connect")
	}

	if settings.Other[settingsWebTransportEnabled] == 0 &&
		settings.Other[settingsEnableWebtransportDraft06] == 0 &&
		settings.Other[settingsWebTransportMaxSessions] == 0 &&
		settings.Other[settingsWebTransportMaxSessionsStd] == 0 {
		qconn.CloseWithError(0, "")
		return nil, fmt.Errorf("wt: server did not enable webtransport")
	}

	requestStr, err := control.OpenRequestStream(dialCtx)
	if err != nil {
		qconn.CloseWithError(0, "")
		return nil, err
	}

	req := (&http.Request{
		Method: http.MethodConnect,
		Header: http.Header{},
		Proto:  protocolHeaderLegacy,
		Host:   target.Host,
		URL:    target,
	}).WithContext(dialCtx)
	if err := requestStr.SendRequestHeader(req); err != nil {
		qconn.CloseWithError(0, "")
		return nil, err
	}
	rsp, err := requestStr.ReadResponse()
	if err != nil {
		qconn.CloseWithError(0, "")
		return nil, err
	}
	if rsp.StatusCode < 200 || rsp.StatusCode >= 300 {
		qconn.CloseWithError(0, "")
		return nil, fmt.Errorf("wt: connect status %d", rsp.StatusCode)
	}
	sessionID := uint64(requestStr.StreamID())

	go watchSessionClose(requestStr, qconn)

	stream, err := qconn.OpenStreamSync(context.Background())
	if err != nil {
		qconn.CloseWithError(0, "")
		return nil, fmt.Errorf("wt open stream: %w", err)
	}
	streamHdr := quicvarint.Append(nil, webTransportFrameType)
	streamHdr = quicvarint.Append(streamHdr, sessionID)
	if _, err := stream.Write(streamHdr); err != nil {
		qconn.CloseWithError(0, "")
		return nil, fmt.Errorf("wt stream header: %w", err)
	}
	stream.SetReliableBoundary()

	return &Conn{
		conn:     qconn,
		stream:   stream,
		reader:   bufio.NewReader(stream),
		compress: compress,
	}, nil
}

func acceptStreams(qconn *quic.Conn, control *http3.RawClientConn) {
	for {
		stream, err := qconn.AcceptStream(context.Background())
		if err != nil {
			return
		}
		go func() {
			typ, err := quicvarint.Peek(stream)
			if err != nil {
				return
			}
			if typ != webTransportFrameType {
				control.HandleBidirectionalStream(stream)
				return
			}
			if _, err := quicvarint.Read(quicvarint.NewReader(stream)); err != nil {
				return
			}
			if _, err := quicvarint.Read(quicvarint.NewReader(stream)); err != nil {
				return
			}
			io.Copy(io.Discard, stream)
		}()
	}
}

func acceptUniStreams(qconn *quic.Conn, control *http3.RawClientConn) {
	for {
		stream, err := qconn.AcceptUniStream(context.Background())
		if err != nil {
			return
		}
		go func() {
			typ, err := quicvarint.Peek(stream)
			if err != nil {
				return
			}
			if typ != webTransportUniStreamType {
				control.HandleUnidirectionalStream(stream)
				return
			}
			if _, err := quicvarint.Read(quicvarint.NewReader(stream)); err != nil {
				return
			}
			if _, err := quicvarint.Read(quicvarint.NewReader(stream)); err != nil {
				return
			}
			io.Copy(io.Discard, stream)
		}()
	}
}

func watchSessionClose(requestStr *http3.RequestStream, qconn *quic.Conn) {
	for {
		typ, r, err := http3.ParseCapsule(quicvarint.NewReader(requestStr))
		if err != nil {
			qconn.CloseWithError(0, "")
			return
		}
		if typ == closeSessionCapsuleType {
			qconn.CloseWithError(0, "")
			return
		}
		io.Copy(io.Discard, r)
	}
}

func (c *Conn) Send(payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.compress {
		compressed, err := deflateRaw(payload)
		if err != nil {
			return err
		}
		payload = compressed
	}
	buf := quicvarint.Append(make([]byte, 0, len(payload)+8), uint64(len(payload)))
	buf = append(buf, payload...)
	_, err := c.stream.Write(buf)
	return err
}

func (c *Conn) Recv() ([]byte, error) {
	length, err := quicvarint.Read(c.reader)
	if err != nil {
		return nil, err
	}
	if length > maxMessageSize {
		return nil, fmt.Errorf("wt message too large: %d", length)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(c.reader, payload); err != nil {
		return nil, err
	}
	if c.compress {
		return inflateRaw(payload)
	}
	return payload, nil
}

func deflateRaw(payload []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer, err := flate.NewWriter(&buf, flate.DefaultCompression)
	if err != nil {
		return nil, err
	}
	if _, err := writer.Write(payload); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func inflateRaw(payload []byte) ([]byte, error) {
	reader := flate.NewReader(bytes.NewReader(payload))
	defer reader.Close()
	return io.ReadAll(reader)
}

func (c *Conn) Close() error {
	if c.conn != nil {
		return c.conn.CloseWithError(0, "")
	}
	return nil
}
