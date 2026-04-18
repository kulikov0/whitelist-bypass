package joiner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"whitelist-bypass/relay/common"
)


const vkConfigCacheKey = "vk_config"

func loadCachedConfig(cache CacheStore) *vkConfig {
	data := cache.Load(vkConfigCacheKey)
	if data == "" {
		return nil
	}
	var cfg vkConfig
	if json.Unmarshal([]byte(data), &cfg) != nil {
		return nil
	}
	return &cfg
}

func saveCachedConfig(cache CacheStore, cfg *vkConfig) {
	data, err := json.Marshal(cfg)
	if err != nil {
		return
	}
	cache.Save(vkConfigCacheKey, string(data))
}

type vkConfig struct {
	AppID           string `json:"appID"`
	ApiVersion      string `json:"apiVersion"`
	AppVersion      string `json:"appVersion"`
	ProtocolVersion string `json:"protocolVersion"`
	PublicKey       string `json:"publicKey,omitempty"`
	OkJoinLink      string `json:"okJoinLink,omitempty"`
}

type vkCaptchaError struct {
	captchaSid     string
	redirectURI    string
	captchaTs      string
	captchaAttempt string
}

func RunVKAuth(joinLink string, displayName string, logFn func(string, ...any), statusFn func(string), cache CacheStore, resolveFn ResolveFunc) (string, error) {
	transport := &http.Transport{}
	if resolveFn != nil {
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, _ := net.SplitHostPort(addr)
			resolvedIP, err := resolveFn(host)
			if err != nil {
				return nil, err
			}
			return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, resolvedIP+":"+port)
		}
	}
	client := &http.Client{Timeout: 60 * time.Second, Transport: transport}

	httpGet := func(targetURL string) ([]byte, error) {
		req, _ := http.NewRequest("GET", targetURL, nil)
		req.Header.Set("User-Agent", common.UserAgent)
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		return io.ReadAll(resp.Body)
	}

	httpPost := func(targetURL string, form url.Values, extraHeaders map[string]string) (map[string]interface{}, error) {
		req, _ := http.NewRequest("POST", targetURL, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", common.UserAgent)
		req.Header.Set("Origin", "https://vk.com")
		req.Header.Set("Referer", "https://vk.com/")
		for k, v := range extraHeaders {
			req.Header.Set(k, v)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		var result map[string]interface{}
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("json: %w (body: %s)", err, string(body[:minInt(len(body), 200)]))
		}
		return result, nil
	}

	// Use cached config if available, fetch only if missing
	cfg := loadCachedConfig(cache)
	if cfg != nil {
		logFn("vk-auth: using cached config appID=%s api=%s", cfg.AppID, cfg.ApiVersion)
	} else {
		statusFn("Fetching config...")
		logFn("vk-auth: fetching config")
		var err error
		cfg, err = fetchVKConfig(httpGet, logFn)
		if err != nil {
			return "", fmt.Errorf("fetchConfig: %w", err)
		}
		saveCachedConfig(cache, cfg)
	}

	// Get anonymous token
	statusFn("Getting anonymous token...")
	logFn("vk-auth: getting anon token")

	anonResp, err := httpPost("https://login.vk.com/?act=get_anonym_token", url.Values{
		"client_id": {cfg.AppID},
	}, nil)
	if err != nil {
		return "", fmt.Errorf("get_anonym_token: %w", err)
	}
	dataMap, _ := anonResp["data"].(map[string]interface{})
	accessToken, _ := dataMap["access_token"].(string)
	if accessToken == "" {
		return "", fmt.Errorf("empty access_token: %v", anonResp)
	}
	logFn("vk-auth: anon token OK")
	auth := map[string]string{"Authorization": "Bearer " + accessToken}

	// Get call settings (public key)
	statusFn("Getting call settings...")
	logFn("vk-auth: getting call settings")

	settingsResp, err := httpPost("https://api.vk.com/method/calls.getSettings", url.Values{
		"v": {cfg.ApiVersion},
	}, auth)
	if err != nil {
		return "", fmt.Errorf("calls.getSettings: %w", err)
	}
	if respObj, ok := settingsResp["response"].(map[string]interface{}); ok {
		if settings, ok := respObj["settings"].(map[string]interface{}); ok {
			if pk, ok := settings["public_key"].(string); ok {
				cfg.PublicKey = pk
			}
		}
	}
	logFn("vk-auth: publicKey=%s", cfg.PublicKey)

	// Get call preview
	statusFn("Getting call preview...")
	logFn("vk-auth: getting call preview")

	previewResp, err := httpPost("https://api.vk.com/method/calls.getCallPreview", url.Values{
		"v":            {cfg.ApiVersion},
		"vk_join_link": {joinLink},
	}, auth)
	if err == nil {
		if respObj, ok := previewResp["response"].(map[string]interface{}); ok {
			if okLink, ok := respObj["ok_join_link"].(string); ok {
				cfg.OkJoinLink = okLink
				logFn("vk-auth: okJoinLink=%s", okLink)
			}
		}
	}

	// Get call token (may trigger captcha)
	statusFn("Getting call token...")
	logFn("vk-auth: getting call token")

	callParams := url.Values{
		"v":            {cfg.ApiVersion},
		"vk_join_link": {joinLink},
		"name":         {displayName},
	}

	var callToken string
	var apiBaseURL string
	var okJoinLink string

	for attempt := 0; attempt < 5; attempt++ {
		callResp, err := httpPost("https://api.vk.com/method/calls.getAnonymousToken", callParams, auth)
		if err != nil {
			return "", fmt.Errorf("getAnonymousToken: %w", err)
		}

		if errObj, hasErr := callResp["error"].(map[string]interface{}); hasErr {
			errCode, _ := errObj["error_code"].(float64)
			if int(errCode) == 14 {
				captchaErr := parseVKCaptchaError(errObj)
				if captchaErr == nil {
					return "", fmt.Errorf("captcha error missing fields: %v", errObj)
				}

				logFn("vk-auth: captcha required")
				statusFn("Solve the captcha:")

				proxyPort := StartCaptchaProxy(captchaErr.redirectURI, resolveFn)
				if proxyPort == 0 {
					return "", fmt.Errorf("failed to start captcha proxy")
				}
				statusFn(fmt.Sprintf("CAPTCHA:http://127.0.0.1:%d/", proxyPort))

				successToken := GetCaptchaResult()
				StopCaptchaProxy()

				if successToken == "" {
					return "", fmt.Errorf("captcha timed out")
				}

				logFn("vk-auth: captcha solved")
				statusFn("Captcha solved, retrying...")

				captchaAttempt := captchaErr.captchaAttempt
				if captchaAttempt == "" || captchaAttempt == "0" {
					captchaAttempt = "1"
				}
				callParams = url.Values{
					"v":               {cfg.ApiVersion},
					"vk_join_link":    {joinLink},
					"name":            {displayName},
					"captcha_key":     {""},
					"captcha_sid":     {captchaErr.captchaSid},
					"is_sound_captcha": {"0"},
					"success_token":   {successToken},
					"captcha_ts":      {captchaErr.captchaTs},
					"captcha_attempt": {captchaAttempt},
				}
				continue
			}
			return "", fmt.Errorf("VK API error: %v", errObj)
		}

		respMap, ok := callResp["response"].(map[string]interface{})
		if !ok {
			return "", fmt.Errorf("unexpected response: %v", callResp)
		}
		callToken, _ = respMap["token"].(string)
		apiBaseURL, _ = respMap["api_base_url"].(string)
		okJoinLink, _ = respMap["ok_join_link"].(string)
		break
	}

	if callToken == "" {
		return "", fmt.Errorf("failed to get call token")
	}

	// OK.ru anonymLogin
	statusFn("Authenticating with OK.ru...")
	logFn("vk-auth: OK.ru anonymLogin")

	baseURL := strings.TrimRight(apiBaseURL, "/")
	if !strings.HasSuffix(baseURL, "/fb.do") {
		baseURL += "/fb.do"
	}

	deviceID := fmt.Sprintf("%d", rand.Int63n(9e18))
	sessionData, _ := json.Marshal(map[string]interface{}{
		"version":        2,
		"device_id":      deviceID,
		"client_version": cfg.AppVersion,
		"client_type":    "SDK_JS",
	})

	okResp, err := httpPost(baseURL, url.Values{
		"method":          {"auth.anonymLogin"},
		"session_data":    {string(sessionData)},
		"application_key": {cfg.PublicKey},
		"format":          {"json"},
	}, nil)
	if err != nil {
		return "", fmt.Errorf("anonymLogin: %w", err)
	}
	sessionKey, _ := okResp["session_key"].(string)
	if sessionKey == "" {
		return "", fmt.Errorf("missing session_key: %v", okResp)
	}
	logFn("vk-auth: OK.ru session OK")

	// Build result
	finalJoinLink := okJoinLink
	if finalJoinLink == "" {
		finalJoinLink = cfg.OkJoinLink
	}
	if finalJoinLink == "" {
		finalJoinLink = joinLink
	}

	result := map[string]string{
		"sessionKey":      sessionKey,
		"applicationKey":  cfg.PublicKey,
		"apiBaseURL":      baseURL,
		"joinLink":        finalJoinLink,
		"anonymToken":     callToken,
		"appVersion":      cfg.AppVersion,
		"protocolVersion": cfg.ProtocolVersion,
	}

	jsonBytes, _ := json.Marshal(result)
	statusFn("Auth complete")
	logFn("vk-auth: done")
	return string(jsonBytes), nil
}

func fetchVKConfig(httpGet func(string) ([]byte, error), logFn func(string, ...any)) (*vkConfig, error) {
	page, err := httpGet("https://vk.com")
	if err != nil {
		return nil, fmt.Errorf("fetch vk.com: %w", err)
	}

	bundleRe := regexp.MustCompile(`https://[a-z0-9.-]+/dist/core_spa/core_spa_vk\.[a-f0-9]+\.js`)
	bundleURL := bundleRe.FindString(string(page))
	if bundleURL == "" {
		return nil, fmt.Errorf("bundle URL not found")
	}
	logFn("vk-auth: bundle=%s", bundleURL[strings.LastIndex(bundleURL, "/")+1:])

	bundle, err := httpGet(bundleURL)
	if err != nil {
		return nil, fmt.Errorf("fetch bundle: %w", err)
	}
	bundleStr := string(bundle)
	chunksBase := bundleURL[:strings.LastIndex(bundleURL, "core_spa_vk.")] + "chunks/"

	cfg := &vkConfig{}
	if m := regexp.MustCompile(`[,;]u=(\d{7,8}),_=\d{7,8},p=\d{8,9}`).FindStringSubmatch(bundleStr); m != nil {
		cfg.AppID = m[1]
	} else {
		return nil, fmt.Errorf("appID not found")
	}
	if m := regexp.MustCompile(`\d+:\(e,t,n\)=>\{"use strict";n\.d\(t,\{m:\(\)=>r\}\);const r="(5\.\d+)"\}`).FindStringSubmatch(bundleStr); m != nil {
		cfg.ApiVersion = m[1]
	} else {
		return nil, fmt.Errorf("apiVersion not found")
	}
	logFn("vk-auth: appID=%s api=%s", cfg.AppID, cfg.ApiVersion)

	bridgeRef := regexp.MustCompile(`core_spa/chunks/webCallsBridge\.([a-f0-9]+)\.js`).FindStringSubmatch(bundleStr)
	if bridgeRef == nil {
		return nil, fmt.Errorf("webCallsBridge not found")
	}
	bridgeURL := chunksBase + "webCallsBridge." + bridgeRef[1] + ".js"
	bridgeData, err := httpGet(bridgeURL)
	if err != nil {
		return nil, fmt.Errorf("fetch bridge: %w", err)
	}

	requireRe := regexp.MustCompile(`i\((\d{4,6})\)`)
	matches := requireRe.FindAllStringSubmatch(string(bridgeData), -1)
	seen := map[string]bool{}
	var moduleIds []string
	for _, m := range matches {
		if !seen[m[1]] {
			seen[m[1]] = true
			moduleIds = append(moduleIds, m[1])
		}
	}

	chunkMap := map[string]string{}
	chunkRe := regexp.MustCompile(`(\d+)===e\)return"core_spa/chunks/"\+e\+"\.([a-f0-9]+)\.js"`)
	for _, m := range chunkRe.FindAllStringSubmatch(bundleStr, -1) {
		chunkMap[m[1]] = m[2]
	}

	appVerRe := regexp.MustCompile(`appVersion.{0,40}return\s+([0-9.]+)`)
	protoVerRe := regexp.MustCompile(`protocolVersion.{0,40}return.*?(\d+)`)

	for _, modId := range moduleIds {
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
		if av := appVerRe.FindStringSubmatch(chunkStr); av != nil {
			cfg.AppVersion = av[1]
			if pv := protoVerRe.FindStringSubmatch(chunkStr); pv != nil {
				cfg.ProtocolVersion = pv[1]
			}
			logFn("vk-auth: appVersion=%s proto=%s", cfg.AppVersion, cfg.ProtocolVersion)
			break
		}
	}

	if cfg.AppVersion == "" {
		return nil, fmt.Errorf("appVersion not found")
	}
	return cfg, nil
}

func parseVKCaptchaError(errObj map[string]interface{}) *vkCaptchaError {
	redirectURI, _ := errObj["redirect_uri"].(string)
	if redirectURI == "" {
		return nil
	}
	captchaSid := ""
	if sid, ok := errObj["captcha_sid"].(string); ok {
		captchaSid = sid
	} else if sidNum, ok := errObj["captcha_sid"].(float64); ok {
		captchaSid = fmt.Sprintf("%.0f", sidNum)
	}
	captchaTs, _ := errObj["captcha_ts"].(string)
	captchaAttempt, _ := errObj["captcha_attempt"].(string)
	return &vkCaptchaError{
		captchaSid:     captchaSid,
		redirectURI:    redirectURI,
		captchaTs:      captchaTs,
		captchaAttempt: captchaAttempt,
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
