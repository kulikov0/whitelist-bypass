package bitrix

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
	headless "github.com/kulikov0/headless-client"
	"whitelist-bypass/relay/common"
)

const (
	slbBase         = "https://slb.bitrix24.tech/v2/join"
	signalSDK       = "js"
	signalVersion   = "1.6.7"
	signalProtocol  = "8"
	clientVersion   = "1.0.0"
	clientPlatform  = "web"
	callTypeInstant = 2
	roomTypeSmall   = 1
)

var (
	reConferenceID = regexp.MustCompile(`conferenceId:\s*'([^']+)'`)
	reChatID       = regexp.MustCompile(`chatId:\s*'([^']+)'`)
	reAlias        = regexp.MustCompile(`alias:\s*'([^']+)'`)
	reSessid       = regexp.MustCompile(`bitrix_sessid["']?\s*[:=]\s*["']([a-f0-9]{8,})`)
	reUserID       = regexp.MustCompile(`"USER_ID"\s*:\s*"?(\d+)`)
)

type Client struct {
	HTTP  *http.Client
	LogFn func(string, ...any)

	portal     string
	sessid     string
	userAgent  string
	instanceID string
	email      string
	password   string
	credsPath  string
}

type JoinResult struct {
	RoomData       string `json:"roomData"`
	RoomID         string `json:"roomId"`
	MediaServerURL string `json:"mediaServerUrl"`
}

type conferenceParams struct {
	ConferenceID string
	ChatID       string
	Alias        string
}

type callInfo struct {
	UUID      string
	ChatID    string
	CallToken string
	UserToken string
	Alias     string
	GuestLink string
}

type slbRequest struct {
	UserToken      string `json:"userToken"`
	IsOneToOne     bool   `json:"isOneToOne"`
	ClientVersion  string `json:"clientVersion"`
	ClientPlatform string `json:"clientPlatform"`
	CallType       int    `json:"callType"`
	RoomType       int    `json:"roomType"`
	InstanceID     string `json:"instanceId"`
	CallToken      string `json:"callToken"`
	Provider       string `json:"provider"`
	IsVideo        bool   `json:"isVideo"`
	CallUUID       string `json:"callUuid"`
}

func NewClient(portal, userAgent string) (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &Client{
		HTTP:       &http.Client{Jar: jar, Transport: headless.ChromeWindows.HTTPClient().Transport},
		LogFn:      log.Printf,
		portal:     strings.TrimRight(portal, "/"),
		userAgent:  userAgent,
		instanceID: uuid.New().String(),
	}, nil
}

func SignalURL(res JoinResult) string {
	q := url.Values{}
	q.Set("auto_subscribe", "1")
	q.Set("sdk", signalSDK)
	q.Set("version", signalVersion)
	q.Set("protocol", signalProtocol)
	q.Set("roomData", res.RoomData)
	q.Set("clientVersion", clientVersion)
	q.Set("clientPlatform", clientPlatform)
	return res.MediaServerURL + "?" + q.Encode()
}

func (c *Client) JoinAsGuest(alias, displayName string) (JoinResult, error) {
	var res JoinResult
	conf, err := c.fetchConference(alias)
	if err != nil {
		return res, err
	}
	userToken, err := c.registerGuest(conf, displayName)
	if err != nil {
		return res, err
	}
	info, err := c.tryJoinCall(conf.ChatID)
	if err != nil {
		return res, err
	}
	info.UserToken = userToken
	return c.slbJoin(info, false)
}

func (c *Client) JoinAsHost(alias string) (JoinResult, error) {
	var res JoinResult
	conf, err := c.fetchConference(alias)
	if err != nil {
		return res, err
	}
	c.LogFn("[host] conference chatId=%s conferenceId=%s", conf.ChatID, conf.ConferenceID)
	callToken, userToken, err := c.getCallToken(conf.ChatID)
	if err != nil {
		return res, err
	}
	info := callInfo{
		UUID:      uuid.New().String(),
		ChatID:    conf.ChatID,
		CallToken: callToken,
		UserToken: userToken,
	}
	c.LogFn("[host] creating media room callUUID=%s callToken=%dB userToken=%dB", info.UUID, len(info.CallToken), len(info.UserToken))
	return c.slbJoin(info, true)
}

func (c *Client) CreateAndJoin() (JoinResult, string, error) {
	var res JoinResult
	info, err := c.createRoom()
	if err != nil {
		return res, "", err
	}
	guestLink := info.GuestLink
	if guestLink == "" {
		guestLink, err = c.getGuestLink(info.ChatID)
		if err != nil {
			return res, "", err
		}
	}
	if info.CallToken == "" {
		callToken, userToken, err := c.getCallToken(info.ChatID)
		if err != nil {
			return res, "", err
		}
		info.CallToken = callToken
		if info.UserToken == "" {
			info.UserToken = userToken
		}
	}
	res, err = c.slbJoin(info, true)
	if err != nil {
		return res, "", err
	}
	return res, guestLink, nil
}

func (c *Client) resetJar() error {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}
	c.HTTP.Jar = jar
	c.sessid = ""
	return nil
}

func (c *Client) do(method, endpoint, contentType string, body io.Reader) ([]byte, int, error) {
	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header = headless.ChromeWindows.Headers(headless.DestEmpty)
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	if c.portal != "" {
		req.Header.Set("Origin", c.portal)
		req.Header.Set("Referer", c.portal+"/")
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return data, resp.StatusCode, err
}

func (c *Client) restForm(action string, form url.Values) ([]byte, int, error) {
	endpoint := c.portal + "/rest/" + action + ".json"
	return c.do("POST", endpoint, "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
}

func (c *Client) ajaxAction(action string, form url.Values) ([]byte, int, error) {
	endpoint := c.portal + "/bitrix/services/main/ajax.php?action=" + action
	if c.sessid != "" {
		endpoint += "&sessid=" + url.QueryEscape(c.sessid)
		form.Set("sessid", c.sessid)
	}
	return c.do("POST", endpoint, "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
}

func (c *Client) fetchConference(alias string) (conferenceParams, error) {
	var p conferenceParams
	body, status, err := c.do("GET", c.portal+"/video/"+alias, "", nil)
	if err != nil {
		return p, err
	}
	if status != 200 {
		return p, fmt.Errorf("fetch conference page: status %d", status)
	}
	page := string(body)
	if m := reConferenceID.FindStringSubmatch(page); m != nil {
		p.ConferenceID = m[1]
	}
	if m := reChatID.FindStringSubmatch(page); m != nil {
		p.ChatID = m[1]
	}
	if m := reAlias.FindStringSubmatch(page); m != nil {
		p.Alias = m[1]
	} else {
		p.Alias = alias
	}
	if m := reSessid.FindStringSubmatch(page); m != nil {
		c.sessid = m[1]
	}
	if p.ConferenceID == "" || p.ChatID == "" {
		return p, fmt.Errorf("conference params not found in page (conferenceId=%q chatId=%q)", p.ConferenceID, p.ChatID)
	}
	return p, nil
}

func (c *Client) registerGuest(p conferenceParams, displayName string) (userToken string, err error) {
	form := url.Values{}
	form.Set("call_auth_id", "guest")
	form.Set("videoconf_id", p.ConferenceID)
	form.Set("call_chat_id", p.ChatID)
	form.Set("alias", p.Alias)
	form.Set("user_hash", "")
	if displayName != "" {
		form.Set("name", displayName)
	}
	body, status, err := c.restForm("call.user.register", form)
	if err != nil {
		return "", err
	}
	var out struct {
		Result struct {
			UserToken string `json:"userToken"`
		} `json:"result"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("register guest: %w (status %d, body %s)", err, status, common.BodySnippet(body))
	}
	if out.Result.UserToken == "" {
		return "", fmt.Errorf("register guest: no userToken (status %d, err %s %s)", status, out.Error, out.ErrorDescription)
	}
	return out.Result.UserToken, nil
}

func (c *Client) tryJoinCall(chatID string) (callInfo, error) {
	var info callInfo
	form := url.Values{}
	form.Set("entityType", "chat")
	form.Set("entityId", "chat"+chatID)
	form.Set("provider", "Bitrix")
	form.Set("callType", fmt.Sprintf("%d", callTypeInstant))
	body, status, err := c.withRelogin(func() ([]byte, int, error) {
		return c.ajaxAction("call.Call.tryJoinCall", form)
	})
	if err != nil {
		return info, err
	}
	var out struct {
		Status string `json:"status"`
		Data   struct {
			Success bool `json:"success"`
			Call    struct {
				UUID   string      `json:"UUID"`
				ChatID json.Number `json:"CHAT_ID"`
			} `json:"call"`
			CallToken string `json:"callToken"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return info, fmt.Errorf("tryJoinCall: %w (status %d, body %s)", err, status, common.BodySnippet(body))
	}
	if out.Status == "success" && !out.Data.Success {
		return info, fmt.Errorf("tryJoinCall: no active call at chat%s, organizer not in the room", chatID)
	}
	if out.Status != "success" || out.Data.Call.UUID == "" {
		return info, fmt.Errorf("tryJoinCall failed (status %d, %s, errors %+v, body %s)", status, out.Status, out.Errors, common.BodySnippet(body))
	}
	info.UUID = out.Data.Call.UUID
	info.ChatID = out.Data.Call.ChatID.String()
	info.CallToken = out.Data.CallToken
	return info, nil
}

func (c *Client) slbJoin(info callInfo, mustCreate bool) (JoinResult, error) {
	var res JoinResult
	reqBody := slbRequest{
		UserToken:      info.UserToken,
		IsOneToOne:     false,
		ClientVersion:  clientVersion,
		ClientPlatform: clientPlatform,
		CallType:       callTypeInstant,
		RoomType:       roomTypeSmall,
		InstanceID:     c.instanceID,
		CallToken:      info.CallToken,
		Provider:       "Bitrix",
		IsVideo:        true,
		CallUUID:       info.UUID,
	}
	payload, _ := json.Marshal(reqBody)
	endpoint := slbBase + "?mustCreate=" + strconv.FormatBool(mustCreate)
	body, status, err := c.do("POST", endpoint, "text/plain;charset=UTF-8", strings.NewReader(string(payload)))
	if err != nil {
		return res, err
	}
	var out struct {
		Result JoinResult `json:"result"`
		Error  string     `json:"error"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return res, fmt.Errorf("slb join: %w (status %d, body %s)", err, status, common.BodySnippet(body))
	}
	if out.Result.MediaServerURL == "" || out.Result.RoomData == "" {
		return res, fmt.Errorf("slb join: missing roomData/mediaServerUrl (status %d, err %s)", status, out.Error)
	}
	return out.Result, nil
}

func (c *Client) currentUserID() (string, error) {
	body, status, err := c.do("GET", c.portal+"/online/", "", nil)
	if err != nil {
		return "", err
	}
	if m := reUserID.FindSubmatch(body); m != nil && string(m[1]) != "0" {
		return string(m[1]), nil
	}
	return "", fmt.Errorf("current user id not found (status %d)", status)
}

func (c *Client) createRoom() (callInfo, error) {
	var info callInfo
	uid, err := c.currentUserID()
	if err != nil {
		return info, fmt.Errorf("createRoom: %w", err)
	}
	form := url.Values{}
	form.Set("fields[entityType]", "VIDEOCONF")
	form.Set("fields[title]", "")
	form.Set("fields[memberEntities][0][0]", "user")
	form.Set("fields[memberEntities][0][1]", uid)
	form.Set("fields[ownerId]", uid)
	form.Set("fields[description]", "")
	form.Set("fields[manageUsersAdd]", "member")
	form.Set("fields[manageUsersDelete]", "manager")
	form.Set("fields[manageUi]", "member")
	form.Set("fields[manageMessages]", "member")
	form.Set("fields[conferencePassword]", "")
	body, status, err := c.withRelogin(func() ([]byte, int, error) {
		return c.ajaxAction("im.v2.Chat.add", form)
	})
	if err != nil {
		return info, err
	}
	var out struct {
		Status string `json:"status"`
		Data   struct {
			ChatID json.Number `json:"chatId"`
			Alias  string      `json:"alias"`
			Link   string      `json:"link"`
		} `json:"data"`
		Errors []struct {
			Code    json.Number `json:"code"`
			Message string      `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return info, fmt.Errorf("createRoom: %w (status %d, body %s)", err, status, common.BodySnippet(body))
	}
	info.ChatID = out.Data.ChatID.String()
	info.Alias = out.Data.Alias
	info.GuestLink = out.Data.Link
	if info.ChatID == "" {
		errMsg := ""
		if len(out.Errors) > 0 {
			errMsg = out.Errors[0].Code.String() + " " + out.Errors[0].Message
		}
		return info, fmt.Errorf("createRoom: no chatId (status %d, status=%q err=%q, body %s)", status, out.Status, errMsg, common.BodySnippet(body))
	}
	c.LogFn("[create] videoconf chatId=%s alias=%s", info.ChatID, info.Alias)
	return info, nil
}

func (c *Client) getGuestLink(chatID string) (string, error) {
	form := url.Values{}
	form.Set("chatId", chatID)
	body, status, err := c.withRelogin(func() ([]byte, int, error) {
		return c.ajaxAction("call.Call.getGuestLink", form)
	})
	if err != nil {
		return "", err
	}
	var out struct {
		Status string `json:"status"`
		Data   struct {
			GuestLink string `json:"guestLink"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("getGuestLink: %w (status %d, body %s)", err, status, common.BodySnippet(body))
	}
	if out.Data.GuestLink == "" {
		return "", fmt.Errorf("getGuestLink: empty (status %d)", status)
	}
	return out.Data.GuestLink, nil
}

func (c *Client) getCallToken(chatID string) (callToken, userToken string, err error) {
	form := url.Values{}
	form.Set("chatId", chatID)
	body, status, err := c.withRelogin(func() ([]byte, int, error) {
		return c.ajaxAction("call.Call.getCallToken", form)
	})
	if err != nil {
		return "", "", err
	}
	var out struct {
		Status string `json:"status"`
		Data   struct {
			CallToken string `json:"callToken"`
			UserToken string `json:"userToken"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", "", fmt.Errorf("getCallToken: %w (status %d, body %s)", err, status, common.BodySnippet(body))
	}
	if out.Data.CallToken == "" {
		return "", "", fmt.Errorf("getCallToken: empty (status %d, body %s)", status, common.BodySnippet(body))
	}
	return out.Data.CallToken, out.Data.UserToken, nil
}
