package dion

import (
	"fmt"

	"whitelist-bypass/relay/common"
)

func deviceHeaders(p common.DeviceProfile) map[string]string {
	return map[string]string{
		"d-platform":        p.Platform,
		"d-browser-type":    p.BrowserType,
		"d-browser-version": p.BrowserVersion,
		"d-device-brand":    p.DeviceBrand,
		"d-device-model":    p.DeviceModel,
		"d-device-type":     p.DeviceType,
		"d-os":              p.OS,
		"d-os-version":      p.OSVersion,
		"d-screen-height":   fmt.Sprintf("%d", p.ScreenHeight),
		"d-screen-width":    fmt.Sprintf("%d", p.ScreenWidth),
	}
}
