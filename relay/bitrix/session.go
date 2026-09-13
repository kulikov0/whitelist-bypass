package bitrix

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
)

type CookieEntry struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Host  string `json:"host"`
}

func (c *Client) LoadSession(path string) error {
	creds, err := LoadCredentials(path)
	if err != nil {
		return err
	}
	c.credsPath = path
	c.email = creds.Email
	c.password = creds.Password
	if creds.Portal != "" {
		c.portal = strings.TrimRight(creds.Portal, "/")
	}
	byHost := map[string][]*http.Cookie{}
	for _, e := range creds.Cookies {
		host := e.Host
		if host == "" {
			host = c.portal
		}
		byHost[host] = append(byHost[host], &http.Cookie{Name: e.Name, Value: e.Value, Path: "/"})
	}
	for host, cookies := range byHost {
		u, err := url.Parse(host)
		if err != nil {
			continue
		}
		c.HTTP.Jar.SetCookies(u, cookies)
	}
	return nil
}

func (c *Client) EnsureLogin() error {
	if c.checkSession() {
		c.LogFn("[auth] session valid, reusing saved cookies")
		return nil
	}
	if c.email == "" || c.password == "" {
		return fmt.Errorf("ensureLogin: no email/password")
	}
	c.LogFn("[auth] session dead, re-logging in")
	return c.loginWithPassword(c.email, c.password)
}

func (c *Client) checkSession() bool {
	body, final, err := c.getPage(c.portal + "/")
	if err != nil {
		return false
	}
	if strings.Contains(final, "auth2.bitrix24.net") {
		return false
	}
	if m := reSessid.FindStringSubmatch(body); m != nil {
		c.sessid = m[1]
		return true
	}
	return false
}

func (c *Client) saveSession() error {
	if c.credsPath == "" {
		return fmt.Errorf("no creds path")
	}
	creds := Credentials{
		Email:    c.email,
		Password: c.password,
		Portal:   c.portal,
		Cookies:  c.gatherCookies(),
	}
	raw, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if atomicErr := writeFileAtomic(c.credsPath, raw); atomicErr != nil {
		if inPlaceErr := os.WriteFile(c.credsPath, raw, 0o600); inPlaceErr != nil {
			return fmt.Errorf("write session: %v, in-place fallback: %w", atomicErr, inPlaceErr)
		}
	}
	return nil
}

func writeFileAtomic(path string, raw []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("write session tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename session tmp: %w", err)
	}
	return nil
}

func (c *Client) gatherCookies() []CookieEntry {
	seen := map[string]struct{}{}
	var out []CookieEntry
	for _, host := range c.cookieHosts() {
		u, err := url.Parse(host)
		if err != nil {
			continue
		}
		for _, ck := range c.HTTP.Jar.Cookies(u) {
			key := host + "|" + ck.Name
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, CookieEntry{Name: ck.Name, Value: ck.Value, Host: host})
		}
	}
	return out
}

func (c *Client) cookieHosts() []string {
	return []string{c.portal, authNetBase}
}

func (c *Client) withRelogin(do func() ([]byte, int, error)) ([]byte, int, error) {
	body, status, err := do()
	if err != nil {
		return body, status, err
	}
	if token := csrfTokenFromBody(body); token != "" && token != c.sessid {
		c.LogFn("[auth] stale csrf token, retrying with the one from the response")
		c.sessid = token
		body, status, err = do()
		if err != nil {
			return body, status, err
		}
	}
	if !looksUnauth(body, status) {
		return body, status, err
	}
	if c.email == "" || c.password == "" {
		return body, status, err
	}
	c.LogFn("[auth] unauthenticated response (status %d), re-logging in and retrying", status)
	if lerr := c.loginWithPassword(c.email, c.password); lerr != nil {
		return body, status, fmt.Errorf("relogin: %w", lerr)
	}
	return do()
}

func csrfTokenFromBody(body []byte) string {
	var out struct {
		Errors []struct {
			Code       any             `json:"code"`
			CustomData json.RawMessage `json:"customData"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return ""
	}
	for _, e := range out.Errors {
		if e.Code != "invalid_csrf" {
			continue
		}
		var customData struct {
			CSRF string `json:"csrf"`
		}
		if err := json.Unmarshal(e.CustomData, &customData); err != nil {
			continue
		}
		return customData.CSRF
	}
	return ""
}

func looksUnauth(body []byte, status int) bool {
	if status == 401 || status == 403 {
		return true
	}
	s := strings.TrimSpace(string(body))
	if s == "" || s[0] != '{' {
		return true
	}
	low := strings.ToLower(s)
	return strings.Contains(low, "not_authorized") ||
		strings.Contains(low, "unauthorized") ||
		strings.Contains(low, "invalid_token") ||
		strings.Contains(low, "user_not_authorized")
}
