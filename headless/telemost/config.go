package main

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"

	"headless-creator/tunnel"
)

type TMConfig struct {
	AppVersion string
	SDKVersion string
}

func fetchConfig() (TMConfig, error) {
	var cfg TMConfig

	page, err := tunnel.HttpGet("https://telemost.yandex.ru/")
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

	bundle, err := tunnel.HttpGet(bundleURL)
	if err != nil {
		return cfg, fmt.Errorf("failed to fetch bundle: %w", err)
	}

	sdkVerRe := regexp.MustCompile(`goloom-sdk\.(\d+\.\d+\.\d+)\.js`)
	if m := sdkVerRe.FindSubmatch(bundle); m != nil {
		cfg.SDKVersion = string(m[1])
	} else {
		return cfg, fmt.Errorf("goloom SDK version not found in bundle")
	}

	log.Printf("[config] app=%s sdk=%s", cfg.AppVersion, cfg.SDKVersion)
	return cfg, nil
}
