package common

import (
	"crypto/tls"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
)

const UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36"

var (
	keyLogOnce     sync.Once
	keyLogWriter   io.Writer
	httpClientOnce sync.Once
	sharedClient   *http.Client
)

func KeyLogWriter() io.Writer {
	keyLogOnce.Do(func() {
		path := os.Getenv("SSLKEYLOGFILE")
		if path == "" {
			return
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
		if err != nil {
			log.Printf("[keylog] cannot open %s: %v", path, err)
			return
		}
		keyLogWriter = f
	})
	return keyLogWriter
}

func HTTPClient() *http.Client {
	httpClientOnce.Do(func() {
		writer := KeyLogWriter()
		if writer == nil {
			sharedClient = http.DefaultClient
			return
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{}
		}
		transport.TLSClientConfig.KeyLogWriter = writer
		sharedClient = &http.Client{Transport: transport}
	})
	return sharedClient
}

func LoadCookies(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("Cannot read cookies: %v", err)
	}
	var cookies []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(data, &cookies); err != nil {
		log.Fatalf("Cannot parse cookies: %v", err)
	}
	parts := make([]string, len(cookies))
	for i, c := range cookies {
		parts[i] = c.Name + "=" + c.Value
	}
	return strings.Join(parts, "; ")
}

func CookieValue(cookieHeader, name string) string {
	for _, part := range strings.Split(cookieHeader, ";") {
		part = strings.TrimSpace(part)
		eq := strings.IndexByte(part, '=')
		if eq != -1 && part[:eq] == name {
			return part[eq+1:]
		}
	}
	return ""
}

func FilterCookies(cookieHeader string, allow []string) string {
	allowed := make(map[string]struct{}, len(allow))
	for _, n := range allow {
		allowed[n] = struct{}{}
	}
	var out []string
	for _, part := range strings.Split(cookieHeader, ";") {
		trimmed := strings.TrimSpace(part)
		eq := strings.IndexByte(trimmed, '=')
		if eq == -1 {
			continue
		}
		if _, ok := allowed[trimmed[:eq]]; ok {
			out = append(out, trimmed)
		}
	}
	return strings.Join(out, "; ")
}

func HttpGet(endpoint string) ([]byte, error) {
	req, _ := http.NewRequest("GET", endpoint, nil)
	req.Header.Set("User-Agent", UserAgent)
	resp, err := HTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
