//go:build windows

package wintunnel

import (
	"net"
	"strings"
)

// extractCandidateIPs pulls every IPv4 literal out of one SDP candidate
// line. ICE candidates look like
//
//	candidate:1 1 udp 2122260223 18.197.224.5 49555 typ host
//	candidate:2 1 udp 1686052607 203.0.113.5 49555 typ srflx raddr 192.168.1.2 rport 50000
//
// We bypass every IP on the line because, at this point, we don't know
// which one Pion will end up using. A handful of extra /32 host routes
// is cheap.
func extractCandidateIPs(line string) []net.IP {
	if line == "" {
		return nil
	}
	if strings.HasPrefix(line, "a=") {
		line = line[2:]
	}
	if !strings.HasPrefix(line, "candidate:") {
		return nil
	}
	parts := strings.Fields(line)
	var out []net.IP
	for i := 0; i < len(parts); i++ {
		ip := net.ParseIP(parts[i])
		if ip == nil {
			continue
		}
		v4 := ip.To4()
		if v4 == nil {
			continue
		}
		if v4.IsLoopback() || v4.IsLinkLocalUnicast() {
			continue
		}
		out = append(out, v4)
	}
	return dedupIPs(out)
}

func dedupIPs(ips []net.IP) []net.IP {
	if len(ips) <= 1 {
		return ips
	}
	seen := make(map[string]struct{}, len(ips))
	out := ips[:0]
	for _, ip := range ips {
		k := ip.String()
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, ip)
	}
	return out
}
