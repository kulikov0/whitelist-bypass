package common

import (
	"errors"
	"net"
	"sync"
	"syscall"
	"time"
)

const blockedDstLogInterval = 10 * time.Second

var AllowPrivateDst bool

var AllowLoopbackDst bool

var ErrPrivateDst = errors.New("destination not allowed")

var (
	blockedDstMu         sync.Mutex
	blockedDstLastLog    time.Time
	blockedDstSuppressed int
)

func ClaimBlockedDstLog() (int, bool) {
	blockedDstMu.Lock()
	defer blockedDstMu.Unlock()
	now := time.Now()
	if !blockedDstLastLog.IsZero() && now.Sub(blockedDstLastLog) < blockedDstLogInterval {
		blockedDstSuppressed++
		return 0, false
	}
	suppressed := blockedDstSuppressed
	blockedDstSuppressed = 0
	blockedDstLastLog = now
	return suppressed, true
}

// cgnat is caught by neither IsGlobalUnicast nor IsPrivate
var _, cgnat, _ = net.ParseCIDR("100.64.0.0/10")

func hostFromAddr(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

func IsPrivateDst(addr string) bool {
	host := hostFromAddr(addr)
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

func DstBlocked(addr string) bool {
	host := hostFromAddr(addr)
	if host == "" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ipBlocked(ip)
}

func ipBlocked(ip net.IP) bool {
	if ip.IsLoopback() {
		return !AllowLoopbackDst
	}
	if isNeverAllowedIP(ip) {
		return true
	}
	return !AllowPrivateDst && isPrivateIP(ip)
}

// AllowPrivateDst unlocks rfc1918 and cgnat, AllowLoopbackDst unlocks the creator's own loopback, neither flag unlocks the metadata ip
func isNeverAllowedIP(ip net.IP) bool {
	return ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsMulticast()
}

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
	if ipBlocked(udpAddr.IP) {
		return nil, ErrPrivateDst
	}
	return net.DialUDP("udp", nil, udpAddr)
}
