package bitrix

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"whitelist-bypass/relay/common"
)

const authNetBase = "https://auth2.bitrix24.net"

var (
	reFlowCfg    = regexp.MustCompile(`b24network\.security\.flowtoken"[^>]*>(\{[^<]*\})</script>`)
	reJunk       = regexp.MustCompile(`[!#$%@~]`)
	reJSRedirect = regexp.MustCompile(`window\.location\.href\s*=\s*['"]([^'"]+)['"]`)
)

type Credentials struct {
	Email    string        `json:"email"`
	Password string        `json:"password"`
	Portal   string        `json:"portal"`
	Cookies  []CookieEntry `json:"cookies,omitempty"`
}

func LoadCredentials(path string) (Credentials, error) {
	var c Credentials
	data, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("parse credentials %s: %w", path, err)
	}
	c.Email = strings.TrimSpace(c.Email)
	c.Portal = strings.TrimRight(strings.TrimSpace(c.Portal), "/")
	return c, nil
}

func (c *Client) loginWithPassword(email, password string) error {
	if email == "" || password == "" {
		return fmt.Errorf("login: no email/password")
	}
	if err := c.resetJar(); err != nil {
		return err
	}
	flowToken, sessid, currentURI, err := c.bootstrapLogin()
	if err != nil {
		return err
	}
	c.LogFn("[auth] bootstrap: flowToken=%dB sessid=%v authURL=%v", len(flowToken), sessid != "", strings.Contains(currentURI, "auth2.bitrix24.net"))
	if err := c.authCheckLogin(flowToken, email, sessid, currentURI); err != nil {
		return err
	}
	c.LogFn("[auth] checkLogin OK")
	if err := c.authCheck(flowToken, email, password, sessid); err != nil {
		return err
	}
	c.LogFn("[auth] check OK")
	c.email = email
	c.password = password
	if err := c.completeOAuth(); err != nil {
		return err
	}
	if c.credsPath != "" {
		if err := c.saveSession(); err != nil {
			return fmt.Errorf("login ok but save session failed: %w", err)
		}
	}
	return nil
}

func (c *Client) bootstrapLogin() (flowToken, sessid, currentURI string, err error) {
	req, err := http.NewRequest("GET", c.portal+"/", nil)
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "ru,en;q=0.9")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	page := string(body)
	currentURI = resp.Request.URL.String()
	if !strings.Contains(currentURI, "auth2.bitrix24.net") {
		return "", "", "", fmt.Errorf("portal did not redirect to auth2 (landed on %s)", currentURI)
	}
	flowToken = computeFlowToken(page)
	if m := reSessid.FindStringSubmatch(page); m != nil {
		sessid = m[1]
	}
	return flowToken, sessid, currentURI, nil
}

func computeFlowToken(page string) string {
	m := reFlowCfg.FindStringSubmatch(page)
	if m == nil {
		return ""
	}
	var cfg struct {
		FTV string `json:"ftv"`
	}
	if err := json.Unmarshal([]byte(m[1]), &cfg); err != nil {
		return ""
	}
	return reJunk.ReplaceAllString(cfg.FTV, "")
}

func (c *Client) authCheckLogin(flowToken, login, sessid, currentURI string) error {
	form := url.Values{}
	form.Set("flow-token-unique-id", flowToken)
	form.Set("login", login)
	form.Set("currentUri", currentURI)
	form.Set("checkSocserv", "Y")
	body, status, err := c.authDo("b24network.authorize.checkLogin", form, sessid)
	if err != nil {
		return err
	}
	var out struct {
		Status string `json:"status"`
		Data   []struct {
			HasAccess bool        `json:"HAS_ACCESS"`
			Captcha   interface{} `json:"CAPTCHA"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return fmt.Errorf("checkLogin: %w (status %d, body %s)", err, status, common.BodySnippet(body))
	}
	if out.Status != "success" || len(out.Data) == 0 {
		return fmt.Errorf("login rejected: checkLogin (status %d, errors %+v, body %s)", status, out.Errors, common.BodySnippet(body))
	}
	if out.Data[0].Captcha != nil {
		return fmt.Errorf("login rejected: captcha required at login step")
	}
	if !out.Data[0].HasAccess {
		return fmt.Errorf("login rejected: account has no access")
	}
	return nil
}

func (c *Client) authCheck(flowToken, login, password, sessid string) error {
	form := url.Values{}
	form.Set("flow-token-unique-id", flowToken)
	form.Set("login", login)
	form.Set("password", password)
	form.Set("remember", "Y")
	body, status, err := c.authDo("b24network.authorize.check", form, sessid)
	if err != nil {
		return err
	}
	var out struct {
		Status string `json:"status"`
		Errors []struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return fmt.Errorf("check: %w (status %d, body %s)", err, status, common.BodySnippet(body))
	}
	if out.Status != "success" || len(out.Errors) > 0 {
		return fmt.Errorf("login rejected: password check (status %d, errors %+v, body %s)", status, out.Errors, common.BodySnippet(body))
	}
	return nil
}

func (c *Client) authDo(action string, form url.Values, sessid string) ([]byte, int, error) {
	endpoint := authNetBase + "/bitrix/services/main/ajax.php?action=" + action
	req, err := http.NewRequest("POST", endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("Origin", authNetBase)
	req.Header.Set("Referer", authNetBase+"/authorization/")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	if sessid != "" {
		req.Header.Set("X-Bitrix-Csrf-Token", sessid)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return body, resp.StatusCode, err
}

func (c *Client) getPage(target string) (body, final string, err error) {
	req, err := http.NewRequest("GET", target, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "ru,en;q=0.9")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return string(raw), resp.Request.URL.String(), nil
}

func (c *Client) completeOAuth() error {
	target := c.portal + "/"
	for hop := 0; hop < 8; hop++ {
		body, final, err := c.getPage(target)
		if err != nil {
			return err
		}
		c.LogFn("[auth] oauth hop %d: final=%s len=%d", hop, final, len(body))
		if next := reJSRedirect.FindStringSubmatch(body); next != nil {
			target = next[1]
			continue
		}
		if strings.HasPrefix(final, c.portal) {
			if m := reSessid.FindStringSubmatch(body); m != nil {
				c.sessid = m[1]
				return nil
			}
		}
		target = c.portal + "/"
		body, _, err = c.getPage(target)
		if err != nil {
			return err
		}
		if m := reSessid.FindStringSubmatch(body); m != nil {
			c.sessid = m[1]
		}
		return nil
	}
	return fmt.Errorf("oauth handoff did not converge")
}
