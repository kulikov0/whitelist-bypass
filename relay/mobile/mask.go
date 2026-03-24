package mobile

import (
	"fmt"
	"net"
)

// maskAddr masks the sensitive portion of an address for logging.
// IPv4: keeps first two octets, e.g. "192.168.x.x"
// IPv6: fully masked
// hostname: first char + "***"
// host:port: masks host, keeps port
func maskAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
		port = ""
	}
	masked := maskHost(host)
	if port != "" {
		return net.JoinHostPort(masked, port)
	}
	return masked
}

func maskHost(host string) string {
	if host == "" {
		return ""
	}
	ip := net.ParseIP(host)
	if ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			return fmt.Sprintf("%d.%d.x.x", ip4[0], ip4[1])
		}
		return "x::x"
	}
	if len(host) <= 1 {
		return "*"
	}
	return string(host[0]) + "***"
}
