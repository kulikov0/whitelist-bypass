package common

import "fmt"

const (
	StatusReady            = "READY"
	StatusConnecting       = "CONNECTING"
	StatusTunnelConnected  = "TUNNEL_CONNECTED"
	StatusTunnelLost       = "TUNNEL_LOST"
	StatusError            = "ERROR"
)

func EmitStatus(status string) {
	fmt.Printf("STATUS:%s\n", status)
}

func EmitStatusError(msg string) {
	fmt.Printf("STATUS:%s:%s\n", StatusError, msg)
}
