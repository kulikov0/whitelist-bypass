package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"

	"github.com/kulikov0/headless-client"
)

type TMConfig struct {
	AppVersion string
	SDKVersion string
}

func fetchConfig() (TMConfig, error) {
	var cfg TMConfig

	page, err := tmHttpGet("https://telemost.yandex.ru/", headless.DestDocument)
	if err != nil {
		return cfg, fmt.Errorf("failed to fetch telemost.yandex.ru: %w", err)
	}

	stateRe := regexp.MustCompile(`<script[^>]*id="preloaded-state"[^>]*>([\s\S]*?)</script>`)
	stateMatch := stateRe.FindSubmatch(page)
	if stateMatch == nil {
		return cfg, fmt.Errorf("preloaded-state not found in page")
	}
	var state struct {
		Config struct {
			AppVersion string `json:"appVersion"`
		} `json:"config"`
		AppVersion string `json:"appVersion"`
	}
	if err := json.Unmarshal(stateMatch[1], &state); err != nil {
		return cfg, fmt.Errorf("failed to parse preloaded-state: %w", err)
	}
	cfg.AppVersion = state.Config.AppVersion
	if cfg.AppVersion == "" {
		cfg.AppVersion = state.AppVersion
	}
	if cfg.AppVersion == "" {
		return cfg, fmt.Errorf("appVersion not found in preloaded-state")
	}
	log.Printf("[config] appVersion=%s", cfg.AppVersion)

	bundleRe := regexp.MustCompile(`https://telemost\.yastatic\.net/s3/telemost/_/main\.\w+\.[a-f0-9]+\.js`)
	bundleURL := bundleRe.FindString(string(page))
	if bundleURL == "" {
		return cfg, fmt.Errorf("main bundle URL not found in page")
	}
	log.Printf("[config] Found bundle: %s", bundleURL)

	bundle, err := tmHttpGet(bundleURL, headless.DestScript)
	if err != nil {
		return cfg, fmt.Errorf("failed to fetch bundle: %w", err)
	}

	sdkVerPatterns := []*regexp.Regexp{
		regexp.MustCompile(`goloom_sdk_version:"(\d+\.\d+\.\d+)"`),
		regexp.MustCompile(`"@yandex-video-platform/goloom-sdk":"(\d+\.\d+\.\d+)"`),
		regexp.MustCompile(`goloom-sdk\.(\d+\.\d+\.\d+)\.js`),
	}
	for _, re := range sdkVerPatterns {
		if m := re.FindSubmatch(bundle); m != nil {
			cfg.SDKVersion = string(m[1])
			break
		}
	}
	if cfg.SDKVersion == "" {
		return cfg, fmt.Errorf("goloom SDK version not found in bundle")
	}

	log.Printf("[config] app=%s sdk=%s", cfg.AppVersion, cfg.SDKVersion)
	return cfg, nil
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
