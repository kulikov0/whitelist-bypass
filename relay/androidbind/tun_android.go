//go:build android

package androidbind

/*
#include <stdint.h>

void disable_fdsan() {
#ifdef __ANDROID__
    extern void android_fdsan_set_error_level(uint32_t) __attribute__((weak));
    if (android_fdsan_set_error_level) {
        android_fdsan_set_error_level(0);
    }
#endif
}
*/
import "C"

import (
	"fmt"
	"os"
	"sync"

	"github.com/xjasonlyu/tun2socks/v2/engine"

	"whitelist-bypass/relay/common"
)

var (
	tunReady     sync.WaitGroup
	activeFilter *v6Filter
)

func StartTun2Socks(fd, mtu, socksPort int, socksUser, socksPass string) error {
	var proxy string
	if socksUser != "" {
		proxy = fmt.Sprintf("socks5://%s:%s@%s:%d", socksUser, socksPass, common.SocksLocalhostIP, socksPort)
	} else {
		proxy = fmt.Sprintf("socks5://%s:%d", common.SocksLocalhostIP, socksPort)
	}
	logMsg("tun2socks: starting fd=%d mtu=%d proxy=%s", fd, mtu, proxy)
	os.Setenv("TUN2SOCKS_LOG_LEVEL", "info")
	tunReady.Add(1)
	defer tunReady.Done()

	deviceFd := fd
	filter, stackFd, err := startV6Filter(fd, mtu)
	if err != nil {
		logMsg("tun2socks: v6 filter setup failed: %v, capturing IPv4 only", err)
	} else {
		activeFilter = filter
		deviceFd = stackFd
		logMsg("tun2socks: v6 reject filter active")
	}

	key := &engine.Key{
		Proxy:  proxy,
		Device: fmt.Sprintf("fd://%d", deviceFd),
		MTU:    mtu,
	}
	engine.Insert(key)
	engine.Start()
	logMsg("tun2socks: running")
	return nil
}

func StopTun2Socks() {
	tunReady.Wait()
	C.disable_fdsan()
	engine.Stop()
	if activeFilter != nil {
		activeFilter.stop()
		activeFilter = nil
	}
	logMsg("tun2socks: stopped")
}
