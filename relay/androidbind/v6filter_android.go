//go:build android

package androidbind

import (
	"encoding/binary"
	"os"
	"sync"
	"syscall"
)

const (
	ipv6HeaderLen    = 40
	tcpHeaderLen     = 20
	icmpv6HeaderLen  = 8
	nextHeaderTCP    = 6
	nextHeaderUDP    = 17
	nextHeaderICMPv6 = 58
	tcpFlagSYN       = 0x02
	tcpFlagRST       = 0x04
	tcpFlagACK       = 0x10
	minIPv6MTU       = 1280
)

type v6Filter struct {
	tunFile  *os.File
	stackOur *os.File
	writeMu  sync.Mutex
	once     sync.Once
}

func startV6Filter(tunFd, mtu int) (*v6Filter, int, error) {
	pair, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_DGRAM, 0)
	if err != nil {
		return nil, 0, err
	}
	f := &v6Filter{
		tunFile:  os.NewFile(uintptr(tunFd), "vpntun"),
		stackOur: os.NewFile(uintptr(pair[0]), "v6filter"),
	}
	go f.tunToStack(mtu)
	go f.stackToTun(mtu)
	return f, pair[1], nil
}

func (f *v6Filter) writeTun(pkt []byte) {
	f.writeMu.Lock()
	f.tunFile.Write(pkt)
	f.writeMu.Unlock()
}

func (f *v6Filter) tunToStack(mtu int) {
	buf := make([]byte, mtu+128)
	for {
		n, err := f.tunFile.Read(buf)
		if err != nil {
			return
		}
		if n < 1 {
			continue
		}
		switch buf[0] >> 4 {
		case 4:
			f.stackOur.Write(buf[:n])
		case 6:
			if reject := buildV6Reject(buf[:n]); reject != nil {
				f.writeTun(reject)
			}
		}
	}
}

func (f *v6Filter) stackToTun(mtu int) {
	buf := make([]byte, mtu+128)
	for {
		n, err := f.stackOur.Read(buf)
		if err != nil {
			return
		}
		if n > 0 {
			f.writeTun(buf[:n])
		}
	}
}

func (f *v6Filter) stop() {
	f.once.Do(func() {
		f.stackOur.Close()
		f.tunFile.Close()
	})
}

// buildV6Reject turns a captured IPv6 packet into an on-device refusal so the
// app falls back to IPv4 immediately: a TCP RST for a connection-initiating
// SYN, an ICMPv6 "no route to destination" for UDP. Anything else, including
// packets carrying extension headers, returns nil and is dropped.
func buildV6Reject(pkt []byte) []byte {
	if len(pkt) < ipv6HeaderLen {
		return nil
	}
	switch pkt[6] {
	case nextHeaderTCP:
		return buildV6TCPReset(pkt)
	case nextHeaderUDP:
		return buildV6ICMPUnreachable(pkt)
	default:
		return nil
	}
}

func buildV6TCPReset(pkt []byte) []byte {
	if len(pkt) < ipv6HeaderLen+tcpHeaderLen {
		return nil
	}
	tcp := pkt[ipv6HeaderLen:]
	flags := tcp[13]
	if flags&tcpFlagSYN == 0 || flags&tcpFlagACK != 0 {
		return nil
	}
	seq := binary.BigEndian.Uint32(tcp[4:8])

	out := make([]byte, ipv6HeaderLen+tcpHeaderLen)
	out[0] = 0x60
	binary.BigEndian.PutUint16(out[4:6], tcpHeaderLen)
	out[6] = nextHeaderTCP
	out[7] = 64
	copy(out[8:24], pkt[24:40])
	copy(out[24:40], pkt[8:24])

	reset := out[ipv6HeaderLen:]
	copy(reset[0:2], tcp[2:4])
	copy(reset[2:4], tcp[0:2])
	binary.BigEndian.PutUint32(reset[8:12], seq+1)
	reset[12] = 5 << 4
	reset[13] = tcpFlagRST | tcpFlagACK
	binary.BigEndian.PutUint16(reset[16:18], transportChecksum(out[8:24], out[24:40], nextHeaderTCP, reset))
	return out
}

func buildV6ICMPUnreachable(pkt []byte) []byte {
	embed := pkt
	if max := minIPv6MTU - ipv6HeaderLen - icmpv6HeaderLen; len(embed) > max {
		embed = embed[:max]
	}
	msgLen := icmpv6HeaderLen + len(embed)

	out := make([]byte, ipv6HeaderLen+msgLen)
	out[0] = 0x60
	binary.BigEndian.PutUint16(out[4:6], uint16(msgLen))
	out[6] = nextHeaderICMPv6
	out[7] = 64
	copy(out[8:24], pkt[24:40])
	copy(out[24:40], pkt[8:24])

	icmp := out[ipv6HeaderLen:]
	icmp[0] = 1 // Destination Unreachable
	icmp[1] = 0 // no route to destination
	copy(icmp[icmpv6HeaderLen:], embed)
	binary.BigEndian.PutUint16(icmp[2:4], transportChecksum(out[8:24], out[24:40], nextHeaderICMPv6, icmp))
	return out
}

func transportChecksum(srcIP, dstIP []byte, nextHeader uint32, payload []byte) uint16 {
	var sum uint32
	add := func(b []byte) {
		for i := 0; i+1 < len(b); i += 2 {
			sum += uint32(b[i])<<8 | uint32(b[i+1])
		}
		if len(b)&1 == 1 {
			sum += uint32(b[len(b)-1]) << 8
		}
	}
	add(srcIP)
	add(dstIP)
	sum += uint32(len(payload))
	sum += nextHeader
	add(payload)
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}
