package common

import (
	"errors"
	"net"
	"syscall"
	"time"
)

var AllowPrivateDst bool

var ErrPrivateDst = errors.New("destination not allowed")

// cgnat is caught by neither IsGlobalUnicast nor IsPrivate
var _, cgnat, _ = net.ParseCIDR("100.64.0.0/10")

func IsPrivateDst(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if host == "" {
		// ":80" dials the local machine
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return isPrivateIP(ip)
}

func DstBlocked(addr string) bool { return !AllowPrivateDst && IsPrivateDst(addr) }

func isPrivateIP(ip net.IP) bool {
	return !ip.IsGlobalUnicast() || ip.IsPrivate() || cgnat.Contains(ip)
}

func DialTCP(addr string, timeout time.Duration) (net.Conn, error) {
	if DstBlocked(addr) {
		return nil, ErrPrivateDst
	}
	// Control runs per resolved address right before connect, a re-resolve can't rebind past it
	d := net.Dialer{
		Timeout: timeout,
		Control: func(_, address string, _ syscall.RawConn) error {
			if DstBlocked(address) {
				return ErrPrivateDst
			}
			return nil
		},
	}
	return d.Dial("tcp", addr)
}

func DialUDP(addr string) (*net.UDPConn, error) {
	if DstBlocked(addr) {
		return nil, ErrPrivateDst
	}
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	if !AllowPrivateDst && isPrivateIP(udpAddr.IP) {
		return nil, ErrPrivateDst
	}
	return net.DialUDP("udp", nil, udpAddr)
}
