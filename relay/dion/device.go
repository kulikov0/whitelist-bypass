package dion

import (
	"fmt"
	"math/rand/v2"
)

type DeviceProfile struct {
	UserAgent      string
	Platform       string
	BrowserType    string
	BrowserVersion string
	DeviceBrand    string
	DeviceModel    string
	DeviceType     string
	OS             string
	OSVersion      string
	ScreenWidth    int
	ScreenHeight   int
}

type deviceTemplate struct {
	os              string
	osVersionPool   []string
	deviceBrandPool []string
	deviceModelPool []string
	browsers        []browserTemplate
}

type browserTemplate struct {
	browserType string
	versionPool []string
	userAgentFn func(osVersion, browserVersion string) string
}

var commonScreens = [][2]int{
	{1280, 720}, {1366, 768}, {1440, 900}, {1536, 864},
	{1600, 900}, {1680, 1050}, {1728, 1117}, {1920, 1080},
	{2048, 1152}, {2560, 1440}, {2880, 1800}, {3840, 2160},
}

var deviceTemplates = []deviceTemplate{
	{
		os:              "Mac OS",
		osVersionPool:   []string{"10.15.7", "11.7.10", "12.7.6", "13.6.9", "14.6.1", "15.1.0"},
		deviceBrandPool: []string{"Apple"},
		deviceModelPool: []string{"Macintosh"},
		browsers: []browserTemplate{
			{
				browserType: "Chrome",
				versionPool: []string{"141.0.0.0", "143.0.0.0", "145.0.0.0", "147.0.0.0", "149.0.0.0"},
				userAgentFn: func(osVersion, browserVersion string) string {
					return fmt.Sprintf("Mozilla/5.0 (Macintosh; Intel Mac OS X %s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36",
						macOSVersionForUA(osVersion), browserVersion)
				},
			},
			{
				browserType: "Safari",
				versionPool: []string{"17.6", "18.0", "18.1", "18.2"},
				userAgentFn: func(osVersion, browserVersion string) string {
					return fmt.Sprintf("Mozilla/5.0 (Macintosh; Intel Mac OS X %s) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/%s Safari/605.1.15",
						macOSVersionForUA(osVersion), browserVersion)
				},
			},
			{
				browserType: "Firefox",
				versionPool: []string{"128.0", "131.0", "133.0", "135.0"},
				userAgentFn: func(osVersion, browserVersion string) string {
					return fmt.Sprintf("Mozilla/5.0 (Macintosh; Intel Mac OS X %s; rv:%s) Gecko/20100101 Firefox/%s",
						macOSVersionForUA(osVersion), browserVersion, browserVersion)
				},
			},
		},
	},
	{
		os:              "Windows",
		osVersionPool:   []string{"10", "11"},
		deviceBrandPool: []string{"Dell", "Lenovo", "HP", "Asus", "Acer", "MSI"},
		deviceModelPool: []string{"PC"},
		browsers: []browserTemplate{
			{
				browserType: "Chrome",
				versionPool: []string{"141.0.0.0", "143.0.0.0", "145.0.0.0", "147.0.0.0", "149.0.0.0"},
				userAgentFn: func(osVersion, browserVersion string) string {
					return fmt.Sprintf("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36",
						browserVersion)
				},
			},
			{
				browserType: "Edge",
				versionPool: []string{"141.0.0.0", "143.0.0.0", "145.0.0.0", "147.0.0.0"},
				userAgentFn: func(osVersion, browserVersion string) string {
					return fmt.Sprintf("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36 Edg/%s",
						browserVersion, browserVersion)
				},
			},
			{
				browserType: "Firefox",
				versionPool: []string{"128.0", "131.0", "133.0", "135.0"},
				userAgentFn: func(osVersion, browserVersion string) string {
					return fmt.Sprintf("Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:%s) Gecko/20100101 Firefox/%s",
						browserVersion, browserVersion)
				},
			},
		},
	},
	{
		os:              "Linux",
		osVersionPool:   []string{"x86_64", "x86_64 GNU"},
		deviceBrandPool: []string{"Dell", "Lenovo", "HP", "System76", "Framework"},
		deviceModelPool: []string{"PC"},
		browsers: []browserTemplate{
			{
				browserType: "Chrome",
				versionPool: []string{"141.0.0.0", "143.0.0.0", "145.0.0.0", "147.0.0.0", "149.0.0.0"},
				userAgentFn: func(osVersion, browserVersion string) string {
					return fmt.Sprintf("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36",
						browserVersion)
				},
			},
			{
				browserType: "Firefox",
				versionPool: []string{"128.0", "131.0", "133.0", "135.0"},
				userAgentFn: func(osVersion, browserVersion string) string {
					return fmt.Sprintf("Mozilla/5.0 (X11; Linux x86_64; rv:%s) Gecko/20100101 Firefox/%s",
						browserVersion, browserVersion)
				},
			},
		},
	},
}

func macOSVersionForUA(osVersion string) string {
	switch osVersion {
	case "10.15.7":
		return "10_15_7"
	case "11.7.10":
		return "10_15_7"
	case "12.7.6":
		return "10_15_7"
	case "13.6.9":
		return "10_15_7"
	case "14.6.1":
		return "10_15_7"
	case "15.1.0":
		return "10_15_7"
	}
	return "10_15_7"
}

func RandomDeviceProfile() DeviceProfile {
	tmpl := deviceTemplates[rand.IntN(len(deviceTemplates))]
	browser := tmpl.browsers[rand.IntN(len(tmpl.browsers))]
	osVersion := tmpl.osVersionPool[rand.IntN(len(tmpl.osVersionPool))]
	browserVersion := browser.versionPool[rand.IntN(len(browser.versionPool))]
	screen := commonScreens[rand.IntN(len(commonScreens))]
	return DeviceProfile{
		UserAgent:      browser.userAgentFn(osVersion, browserVersion),
		Platform:       "web",
		BrowserType:    browser.browserType,
		BrowserVersion: browserVersion,
		DeviceBrand:    tmpl.deviceBrandPool[rand.IntN(len(tmpl.deviceBrandPool))],
		DeviceModel:    tmpl.deviceModelPool[rand.IntN(len(tmpl.deviceModelPool))],
		DeviceType:     "pc",
		OS:             tmpl.os,
		OSVersion:      osVersion,
		ScreenWidth:    screen[0],
		ScreenHeight:   screen[1],
	}
}

func (p DeviceProfile) Headers() map[string]string {
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
