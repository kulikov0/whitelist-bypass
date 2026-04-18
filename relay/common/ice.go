package common

import (
	"net"
	"strings"
)

func FixICEURL(iceURL string) string {
	idx := strings.Index(iceURL, ":")
	if idx < 0 {
		return iceURL
	}
	scheme := iceURL[:idx]
	if scheme != "turn" && scheme != "stun" && scheme != "turns" && scheme != "stuns" {
		return iceURL
	}
	rest := iceURL[idx+1:]
	if strings.HasPrefix(rest, "[") {
		return iceURL
	}
	if strings.Count(rest, ":") <= 1 {
		return iceURL
	}
	params := ""
	if qm := strings.Index(rest, "?"); qm >= 0 {
		params = rest[qm:]
		rest = rest[:qm]
	}
	lastColon := strings.LastIndex(rest, ":")
	if lastColon > 0 {
		host := rest[:lastColon]
		port := rest[lastColon+1:]
		if net.ParseIP(host) != nil {
			return scheme + ":[" + host + "]:" + port + params
		}
	}
	if net.ParseIP(rest) != nil {
		return scheme + ":[" + rest + "]" + params
	}
	return iceURL
}

func ExtractICEHost(iceURL string) string {
	idx := strings.Index(iceURL, ":")
	if idx < 0 {
		return ""
	}
	rest := iceURL[idx+1:]
	params := strings.Index(rest, "?")
	if params >= 0 {
		rest = rest[:params]
	}
	host, _, err := net.SplitHostPort(rest)
	if err != nil {
		return rest
	}
	return host
}
