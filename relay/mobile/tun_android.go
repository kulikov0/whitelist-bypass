//go:build android

package mobile

import (
	"fmt"
	"os"
	"sync"
	"syscall"

	"github.com/xjasonlyu/tun2socks/v2/engine"
)

var (
	tunReady  sync.WaitGroup
	tunOrigFd int = -1
)

func StartTun2Socks(fd, mtu, socksPort int, socksUser, socksPass string) error {
	// dup the fd: tun2socks will close the dup on Stop(),
	// we keep the original open to prevent the kernel from
	// recycling the fd number while gvisor goroutines drain
	dupFd, err := syscall.Dup(fd)
	if err != nil {
		return fmt.Errorf("dup tun fd: %w", err)
	}
	tunOrigFd = fd

	var proxy string
	if socksUser != "" {
		proxy = fmt.Sprintf("socks5://%s:%s@127.0.0.1:%d", socksUser, socksPass, socksPort)
	} else {
		proxy = fmt.Sprintf("socks5://127.0.0.1:%d", socksPort)
	}
	logMsg("tun2socks: starting fd=%d (dup=%d) mtu=%d proxy=%s", fd, dupFd, mtu, proxy)
	os.Setenv("TUN2SOCKS_LOG_LEVEL", "info")
	key := &engine.Key{
		Proxy:  proxy,
		Device: fmt.Sprintf("fd://%d", dupFd),
		MTU:    mtu,
	}
	tunReady.Add(1)
	engine.Insert(key)
	engine.Start()
	tunReady.Done()
	logMsg("tun2socks: running")
	return nil
}

func StopTun2Socks() {
	tunReady.Wait()
	engine.Stop()
	// now safe to close the original - gvisor goroutines are done
	if tunOrigFd >= 0 {
		syscall.Close(tunOrigFd)
		tunOrigFd = -1
	}
	logMsg("tun2socks: stopped")
}
