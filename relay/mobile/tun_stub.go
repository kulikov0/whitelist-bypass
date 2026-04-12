//go:build !android

package mobile

import "fmt"

func StartTun2Socks(fd, mtu, socksPort int, socksUser, socksPass string) error {
	return fmt.Errorf("tun2socks is only available on Android")
}

func StopTun2Socks() {}
