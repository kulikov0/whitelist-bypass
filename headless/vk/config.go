package main

import (
	"fmt"
	"log"
	"regexp"
	"strings"
)

type VKConfig struct {
	AppID           string
	APIVersion      string
	SDKVersion      string
	AppVersion      string
	ProtocolVersion string
}

func fetchConfig() (VKConfig, error) {
	var cfg VKConfig

	// Discover current bundle URL
	page, err := httpGet("https://vk.com")
	if err != nil {
		return cfg, fmt.Errorf("failed to fetch vk.com: %w", err)
	}

	bundleRe := regexp.MustCompile(`https://[a-z0-9.-]+/dist/core_spa/core_spa_vk\.[a-f0-9]+\.js`)
	bundleURL := bundleRe.FindString(string(page))
	if bundleURL == "" {
		snippet := string(page)
		if len(snippet) > 500 {
			snippet = snippet[:500]
		}
		return cfg, fmt.Errorf("bundle URL not found in page (length: %d): %s", len(page), snippet)
	}
	log.Printf("[config] Found bundle: %s", bundleURL)

	bundle, err := httpGet(bundleURL)
	if err != nil {
		return cfg, fmt.Errorf("failed to fetch bundle: %w", err)
	}
	bundleStr := string(bundle)
	chunksBase := bundleURL[:strings.LastIndex(bundleURL, "core_spa_vk.")] + "chunks/"

	// Extract app_id and API version from the main bundle
	if m := regexp.MustCompile(`[,;]u=(\d{7,8}),_=\d{7,8},p=\d{8,9}`).FindStringSubmatch(bundleStr); m != nil {
		cfg.AppID = m[1]
	} else {
		return cfg, fmt.Errorf("app_id not found in bundle")
	}
	if m := regexp.MustCompile(`\d+:\(e,t,n\)=>\{"use strict";n\.d\(t,\{m:\(\)=>r\}\);const r="(5\.\d+)"\}`).FindStringSubmatch(bundleStr); m != nil {
		cfg.APIVersion = m[1]
	} else {
		return cfg, fmt.Errorf("apiVersion not found in bundle")
	}
	log.Printf("[config] app_id=%s api=%s", cfg.AppID, cfg.APIVersion)

	// Find webCallsBridge chunk
	bridgeRef := regexp.MustCompile(`core_spa/chunks/webCallsBridge\.([a-f0-9]+)\.js`).FindStringSubmatch(bundleStr)
	if bridgeRef == nil {
		return cfg, fmt.Errorf("webCallsBridge chunk not found in bundle")
	}
	bridgeURL := chunksBase + "webCallsBridge." + bridgeRef[1] + ".js"
	bridgeData, err := httpGet(bridgeURL)
	if err != nil {
		return cfg, fmt.Errorf("failed to fetch bridge chunk: %w", err)
	}
	bridgeStr := string(bridgeData)

	// Extract module IDs imported by the bridge
	requireRe := regexp.MustCompile(`i\((\d{4,6})\)`)
	matches := requireRe.FindAllStringSubmatch(bridgeStr, -1)
	seen := map[string]bool{}
	var moduleIds []string
	for _, m := range matches {
		if !seen[m[1]] {
			seen[m[1]] = true
			moduleIds = append(moduleIds, m[1])
		}
	}

	// Build chunk ID -> hash map from the bundle
	chunkMap := map[string]string{}
	chunkRe := regexp.MustCompile(`(\d+)===e\)return"core_spa/chunks/"\+e\+"\.([a-f0-9]+)\.js"`)
	for _, m := range chunkRe.FindAllStringSubmatch(bundleStr, -1) {
		chunkMap[m[1]] = m[2]
	}

	// Match module IDs to chunk IDs, fetch until sdkVersion found
	sdkVerRe := regexp.MustCompile(`sdkVersion.{0,40}return\s*"([^"]+)"`)
	appVerRe := regexp.MustCompile(`appVersion.{0,40}return\s+([0-9.]+)`)
	protoVerRe := regexp.MustCompile(`protocolVersion.{0,40}return.*?(\d+)`)

	for _, modId := range moduleIds {
		// try exact match, then with first digit dropped
		chunkId := modId
		hash, ok := chunkMap[chunkId]
		if !ok {
			chunkId = modId[1:]
			hash, ok = chunkMap[chunkId]
		}
		if !ok {
			continue
		}

		chunkURL := fmt.Sprintf("%s%s.%s.js", chunksBase, chunkId, hash)
		chunkData, err := httpGet(chunkURL)
		if err != nil {
			continue
		}
		chunkStr := string(chunkData)

		sv := sdkVerRe.FindStringSubmatch(chunkStr)
		if sv == nil {
			continue
		}

		log.Printf("[config] Found SDK in chunk %s", chunkId)
		cfg.SDKVersion = sv[1]
		if av := appVerRe.FindStringSubmatch(chunkStr); av != nil {
			cfg.AppVersion = av[1]
		} else {
			log.Printf("[config] WARNING: appVersion not found in SDK chunk")
		}
		if pv := protoVerRe.FindStringSubmatch(chunkStr); pv != nil {
			cfg.ProtocolVersion = pv[1]
		} else {
			log.Printf("[config] WARNING: protocolVersion not found in SDK chunk")
		}
		break
	}

	if cfg.SDKVersion == "" {
		return cfg, fmt.Errorf("sdkVersion not found in any chunk")
	}
	log.Printf("[config] sdk=%s app=%s proto=%s", cfg.SDKVersion, cfg.AppVersion, cfg.ProtocolVersion)
	return cfg, nil
}
