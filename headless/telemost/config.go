package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/kulikov0/headless-client"
)

const (
	fallbackAppVersion = "211.2.0"
	fallbackSDKVersion = "5.28.0"
)

var (
	globalParamsRe = regexp.MustCompile(`var globalParams\s*=\s*`)
	appBundleRe    = regexp.MustCompile(`src="([^"]+app\.js)"`)

	sdkVersionPatterns = []*regexp.Regexp{
		regexp.MustCompile(`buildInfo\s*=\s*\{version:"(\d+\.\d+\.\d+)"`),
		regexp.MustCompile(`goloom-sdk@(\d+\.\d+\.\d+)`),
		regexp.MustCompile(`goloom_sdk_version:"(\d+\.\d+\.\d+)"`),
		regexp.MustCompile(`"@yandex-video-platform/goloom-sdk":"(\d+\.\d+\.\d+)"`),
		regexp.MustCompile(`goloom-sdk\.(\d+\.\d+\.\d+)\.js`),
	}
)

type TMConfig struct {
	AppVersion string
	SDKVersion string
}

func fetchConfig() TMConfig {
	var cfg TMConfig

	page, err := tmHttpGet("https://telemost.yandex.ru/", headless.DestDocument)
	if err != nil {
		log.Printf("[config] failed to fetch telemost.yandex.ru: %v", err)
		return finishConfig(cfg)
	}

	cfg.AppVersion = parseAppVersion(page)
	cfg.SDKVersion = fetchSDKVersion(page)
	return finishConfig(cfg)
}

func parseAppVersion(page []byte) string {
	loc := globalParamsRe.FindIndex(page)
	if loc == nil {
		log.Println("[config] globalParams not found in page")
		return ""
	}
	var params struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(bytes.NewReader(page[loc[1]:])).Decode(&params); err != nil {
		log.Printf("[config] failed to parse globalParams: %v", err)
		return ""
	}
	if params.Version == "" {
		log.Println("[config] version not found in globalParams")
	}
	return params.Version
}

func fetchSDKVersion(page []byte) string {
	bundleURL := parseBundleURL(page)
	if bundleURL == "" {
		log.Println("[config] app bundle URL not found in page")
		return ""
	}
	log.Printf("[config] Found bundle: %s", bundleURL)

	bundle, err := tmHttpGet(bundleURL, headless.DestScript)
	if err != nil {
		log.Printf("[config] failed to fetch bundle: %v", err)
		return ""
	}
	for _, pattern := range sdkVersionPatterns {
		if match := pattern.FindSubmatch(bundle); match != nil {
			return string(match[1])
		}
	}
	log.Println("[config] goloom SDK version not found in bundle")
	return ""
}

func parseBundleURL(page []byte) string {
	match := appBundleRe.FindSubmatch(page)
	if match == nil {
		return ""
	}
	bundleURL := string(match[1])
	if strings.HasPrefix(bundleURL, "//") {
		bundleURL = "https:" + bundleURL
	}
	return bundleURL
}

func finishConfig(cfg TMConfig) TMConfig {
	if cfg.AppVersion == "" {
		cfg.AppVersion = fallbackAppVersion
		log.Printf("[config] page scrape missed appVersion, using pinned %s", fallbackAppVersion)
	}
	if cfg.SDKVersion == "" {
		cfg.SDKVersion = fallbackSDKVersion
		log.Printf("[config] bundle scrape missed sdkVersion, using pinned %s", fallbackSDKVersion)
	}
	log.Printf("[config] app=%s sdk=%s", cfg.AppVersion, cfg.SDKVersion)
	return cfg
}

func tmHttpGet(endpoint string, dest headless.RequestDest) ([]byte, error) {
	request, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header = headless.ChromeWindows.Headers(dest)
	if dest == headless.DestScript {
		request.Header.Set("Referer", "https://telemost.yandex.ru/")
	}
	response, err := headless.ChromeWindows.HTTPClient().Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	return io.ReadAll(response.Body)
}
