package common

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

	AuthNone     = 0x00
	AuthUserPass = 0x02
	AuthNoMatch  = 0xFF

	HandshakeBuf = 258
	UDPBufSize   = 4096
	RTPBufSize   = 65536
	VP8BufSize   = 900
	DCBufSize    = 32768
)

var (
	NoAuth   = []byte{Ver, AuthNone}
	OK       = []byte{Ver, 0x00, 0x00, AtypIPv4, 0, 0, 0, 0, 0, 0}
	ConnFail = []byte{Ver, 0x05, 0x00, AtypIPv4, 0, 0, 0, 0, 0, 0}
	CmdErr   = []byte{Ver, 0x07, 0x00, AtypIPv4, 0, 0, 0, 0, 0, 0}
	AddrErr  = []byte{Ver, 0x08, 0x00, AtypIPv4, 0, 0, 0, 0, 0, 0}
	GenFail  = []byte{Ver, 0x01, 0x00, AtypIPv4, 0, 0, 0, 0, 0, 0}
)

func NegotiateAuth(conn net.Conn, buf []byte, n int, wantUser, wantPass string) bool {
	if wantUser == "" {
		conn.Write(NoAuth)
		return true
	}
	hasUserPass := false
	methodCount := int(buf[1])
	for i := 0; i < methodCount && i+2 < n; i++ {
		if buf[2+i] == AuthUserPass {
			hasUserPass = true
			break
		}
	}
	if !hasUserPass {
		conn.Write([]byte{Ver, AuthNoMatch})
		return false
	}
	conn.Write([]byte{Ver, AuthUserPass})
	authN, err := conn.Read(buf)
	if err != nil || authN < 5 || buf[0] != 0x01 {
		return false
	}
	userLen := int(buf[1])
	if authN < 2+userLen+1 {
		conn.Write([]byte{0x01, 0x01})
		return false
	}
	user := string(buf[2 : 2+userLen])
	passLen := int(buf[2+userLen])
	if authN < 2+userLen+1+passLen {
		conn.Write([]byte{0x01, 0x01})
		return false
	}
	pass := string(buf[3+userLen : 3+userLen+passLen])
	if user != wantUser || pass != wantPass {
		conn.Write([]byte{0x01, 0x01})
		return false
	}
	conn.Write([]byte{0x01, 0x00})
	return true
}

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
