package common

import (
	"fmt"
	"strings"
)

const (
	StatusReady           = "READY"
	StatusConnecting      = "CONNECTING"
	StatusReconnecting    = "RECONNECTING"
	StatusTunnelConnected = "TUNNEL_CONNECTED"
	StatusTunnelLost      = "TUNNEL_LOST"
	StatusError           = "ERROR"
)

const (
	AuthErrorInvalidCredentials = "INVALID_CREDENTIALS"
	AuthErrorSessionExpired     = "SESSION_EXPIRED"
)

func EmitStatus(status string) {
	fmt.Printf("STATUS:%s\n", status)
}

func EmitStatusError(msg string) {
	fmt.Printf("STATUS:%s:%s\n", StatusError, msg)
}

func EmitAuthError(kind string) {
	fmt.Printf("STATUS:AUTH_ERROR:%s\n", kind)
}

func looksLikeInvalidCreds(msg string) bool {
	return strings.Contains(msg, `"code":1086`) ||
		strings.Contains(msg, "Invalid credentials") ||
		strings.Contains(msg, "login rejected")
}

func looksLikeSessionExpired(msg string) bool {
	return strings.Contains(msg, "status 401") ||
		strings.Contains(msg, "UnauthorizedError") ||
		strings.Contains(msg, `"error":"unauthorized`) ||
		strings.Contains(msg, "empty access_token") ||
		strings.Contains(msg, "empty VK token")
}

func EmitAuthErrorFor(err error) {
	if err == nil {
		return
	}
	msg := err.Error()
	switch {
	case looksLikeInvalidCreds(msg):
		EmitAuthError(AuthErrorInvalidCredentials)
	case looksLikeSessionExpired(msg):
		EmitAuthError(AuthErrorSessionExpired)
	}
}
