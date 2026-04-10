package socks

import (
	"encoding/binary"
	"fmt"
	"net"
)

const (
	Ver        = 0x05
	CmdTCP     = 0x01
	CmdUDP     = 0x03
	AtypIPv4   = 0x01
	AtypDomain = 0x03
	AtypIPv6   = 0x04

	HandshakeBuf = 258
	UDPBufSize   = 4096
	RTPBufSize   = 65536
	VP8BufSize   = 900
	DCBufSize    = 32768
)

var (
	NoAuth   = []byte{Ver, 0x00}
	OK       = []byte{Ver, 0x00, 0x00, AtypIPv4, 0, 0, 0, 0, 0, 0}
	ConnFail = []byte{Ver, 0x05, 0x00, AtypIPv4, 0, 0, 0, 0, 0, 0}
	CmdErr   = []byte{Ver, 0x07, 0x00, AtypIPv4, 0, 0, 0, 0, 0, 0}
	AddrErr  = []byte{Ver, 0x08, 0x00, AtypIPv4, 0, 0, 0, 0, 0, 0}
	GenFail  = []byte{Ver, 0x01, 0x00, AtypIPv4, 0, 0, 0, 0, 0, 0}
)

func ParseAddress(buf []byte, n int) (host string, headerLen int, err error) {
	if n < 7 {
		return "", 0, fmt.Errorf("too short")
	}
	switch buf[3] {
	case AtypIPv4:
		if n < 10 {
			return "", 0, fmt.Errorf("too short for IPv4")
		}
		host = fmt.Sprintf("%d.%d.%d.%d:%d", buf[4], buf[5], buf[6], buf[7],
			binary.BigEndian.Uint16(buf[8:10]))
		return host, 10, nil
	case AtypDomain:
		dlen := int(buf[4])
		if n < 5+dlen+2 {
			return "", 0, fmt.Errorf("too short for domain")
		}
		host = fmt.Sprintf("%s:%d", string(buf[5:5+dlen]),
			binary.BigEndian.Uint16(buf[5+dlen:7+dlen]))
		return host, 5 + dlen + 2, nil
	case AtypIPv6:
		if n < 22 {
			return "", 0, fmt.Errorf("too short for IPv6")
		}
		ip := net.IP(buf[4:20])
		host = fmt.Sprintf("[%s]:%d", ip.String(),
			binary.BigEndian.Uint16(buf[20:22]))
		return host, 22, nil
	default:
		return "", 0, fmt.Errorf("unsupported address type 0x%02x", buf[3])
	}
}
