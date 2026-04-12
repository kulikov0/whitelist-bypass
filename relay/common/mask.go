package common

import (
	"fmt"
	"net"
)

func MaskError(err error) string {
	if err == nil {
		return ""
	}
	if opErr, ok := err.(*net.OpError); ok {
		msg := opErr.Op
		if opErr.Net != "" {
			msg += " " + opErr.Net
		}
		if opErr.Source != nil {
			msg += " " + MaskAddr(opErr.Source.String())
		}
		if opErr.Source != nil && opErr.Addr != nil {
			msg += "->"
		}
		if opErr.Addr != nil {
			msg += MaskAddr(opErr.Addr.String())
		}
		msg += ": " + opErr.Err.Error()
		return msg
	}
	return err.Error()
}

// MaskAddr masks the sensitive portion of an address for logging.
func MaskAddr(addr string) string {
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
