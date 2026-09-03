//go:build !ios && !darwin

package ios

import "fmt"

func StartTun2SocksPacketFlow(sink PacketFlowSink, mtu int, socksPort int, socksUser, socksPass string) error {
	return fmt.Errorf("tun2socks: iOS only")
}

func InputPacket(data []byte) {}

func StopTun2Socks() {}
