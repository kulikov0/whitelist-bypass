package socks

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
)

var (
	NoAuth   = []byte{Ver, 0x00}
	OK       = []byte{Ver, 0x00, 0x00, AtypIPv4, 0, 0, 0, 0, 0, 0}
	ConnFail = []byte{Ver, 0x05, 0x00, AtypIPv4, 0, 0, 0, 0, 0, 0}
	CmdErr   = []byte{Ver, 0x07, 0x00, AtypIPv4, 0, 0, 0, 0, 0, 0}
	AddrErr  = []byte{Ver, 0x08, 0x00, AtypIPv4, 0, 0, 0, 0, 0, 0}
	GenFail  = []byte{Ver, 0x01, 0x00, AtypIPv4, 0, 0, 0, 0, 0, 0}
)
