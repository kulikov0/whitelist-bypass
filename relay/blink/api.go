package blink

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/google/uuid"
	headless "github.com/kulikov0/headless-client"
	"whitelist-bypass/relay/common"
)

const (
	APIBase         = "https://pin.apiblink.ru/api"
	Origin          = "https://web.blinkmap.com"
	ClientUserAgent = "web_client (client 0.1.85)"
	LoginLinkBase   = "https://blinkmap.com/deeplink/webapp/login"
	JoinLinkScheme  = "blink://"
	InviteLinkHost  = "blinkmap.com"
)

var (
	ErrLoginPending   = errors.New("blink login is not approved yet")
	ErrLoginCancelled = errors.New("blink login cancelled")
)

type StatusError struct {
	Operation string
	Status    int
	Body      string
}

type TokenPair struct {
	Token          string `json:"token"`
	TokenExpiresAt int64  `json:"token_expires_at"`
	RefreshToken   string `json:"refresh_token"`
}

type LoginRequest struct {
	TTL int    `json:"ttl"`
	WAT string `json:"wat"`
}

type CallConnection struct {
	CallID      string `json:"call_id"`
	Token       string `json:"token"`
	APIEndpoint string `json:"api_endpoint"`
	Created     bool   `json:"created"`
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s: status %d: %s", e.Operation, e.Status, e.Body)
}

func ParseChatID(input string) (int64, error) {
	trimmed := strings.TrimSpace(input)
	trimmed = strings.TrimPrefix(trimmed, JoinLinkScheme)
	trimmed = strings.Trim(trimmed, "/")
	chatID, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil || chatID <= 0 {
		return 0, fmt.Errorf("not a Blink chat id: %q", input)
	}
	return chatID, nil
}

func ParseInviteKey(input string) (string, bool) {
	trimmed := strings.TrimSpace(input)
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", false
	}
	host := parsed.Hostname()
	invitePath := strings.TrimRight(parsed.Path, "/")
	if (host != InviteLinkHost && host != "web."+InviteLinkHost) || !strings.Contains(invitePath, "/join/") {
		return "", false
	}
	return path.Base(invitePath), true
}

func JoinLink(chatID int64) string {
	return JoinLinkScheme + strconv.FormatInt(chatID, 10)
}

func LoginLink(wat string) string {
	return LoginLinkBase + "?wat=" + url.QueryEscape(wat)
}

func IsUnauthorized(err error) bool {
	var statusErr *StatusError
	return errors.As(err, &statusErr) && statusErr.Status == http.StatusUnauthorized
}

func RequestLogin(client *http.Client) (LoginRequest, error) {
	var result LoginRequest
	raw, status, err := apiDo(client, http.MethodGet, "/login/webapp/token/request", "", nil)
	if err != nil {
		return result, fmt.Errorf("login request: %w", err)
	}
	if status != http.StatusOK {
		return result, &StatusError{Operation: "login request", Status: status, Body: common.BodySnippet(raw)}
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, fmt.Errorf("login request decode: %w", err)
	}
	if result.WAT == "" {
		return result, fmt.Errorf("login request: empty wat in %s", common.BodySnippet(raw))
	}
	return result, nil
}

func PollLogin(client *http.Client, wat string) (TokenPair, error) {
	var result TokenPair
	raw, status, err := apiDo(client, http.MethodGet, "/login/webapp/token?wat="+url.QueryEscape(wat), "", nil)
	if err != nil {
		return result, fmt.Errorf("login poll: %w", err)
	}
	if status == http.StatusNotFound {
		return result, ErrLoginPending
	}
	if status != http.StatusOK {
		return result, &StatusError{Operation: "login poll", Status: status, Body: common.BodySnippet(raw)}
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, fmt.Errorf("login poll decode: %w", err)
	}
	if result.Token == "" {
		return result, ErrLoginPending
	}
	return result, nil
}

func RefreshTokens(client *http.Client, refreshToken string) (TokenPair, error) {
	var result TokenPair
	raw, status, err := apiDo(client, http.MethodPost, "/login/webapp/token/refresh", "",
		map[string]string{"refresh_token": refreshToken})
	if err != nil {
		return result, fmt.Errorf("token refresh: %w", err)
	}
	if status != http.StatusOK {
		return result, &StatusError{Operation: "token refresh", Status: status, Body: common.BodySnippet(raw)}
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, fmt.Errorf("token refresh decode: %w", err)
	}
	if result.Token == "" || result.RefreshToken == "" {
		return result, fmt.Errorf("token refresh: empty token in response")
	}
	return result, nil
}

func ConnectCall(client *http.Client, accessToken string, chatID int64) (CallConnection, error) {
	var result CallConnection
	raw, status, err := apiDo(client, http.MethodPost, "/calls/connect", accessToken,
		map[string]int64{"chat_id": chatID})
	if err != nil {
		return result, fmt.Errorf("calls connect: %w", err)
	}
	if status != http.StatusOK {
		return result, &StatusError{Operation: "calls connect", Status: status, Body: common.BodySnippet(raw)}
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, fmt.Errorf("calls connect decode: %w", err)
	}
	if result.Token == "" || result.APIEndpoint == "" {
		return result, fmt.Errorf("calls connect: no token or endpoint in %s", common.BodySnippet(raw))
	}
	return result, nil
}

func FetchAccountID(client *http.Client, accessToken string) (int64, error) {
	var result struct {
		AccountID int64 `json:"account_id"`
	}
	raw, status, err := apiDo(client, http.MethodGet, "/account/profile", accessToken, nil)
	if err != nil {
		return 0, fmt.Errorf("account profile: %w", err)
	}
	if status != http.StatusOK {
		return 0, &StatusError{Operation: "account profile", Status: status, Body: common.BodySnippet(raw)}
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return 0, fmt.Errorf("account profile decode: %w", err)
	}
	if result.AccountID <= 0 {
		return 0, fmt.Errorf("account profile: no account_id in response")
	}
	return result.AccountID, nil
}

func CreateGroupChat(client *http.Client, accessToken string, accountID int64, name string) (int64, error) {
	var result struct {
		Data struct {
			ChatID int64 `json:"chat_id"`
		} `json:"data"`
	}
	raw, status, err := apiDo(client, http.MethodPost, "/messenger/v2/group-chat/create", accessToken, map[string]any{
		"action_type": "create_group_chat",
		"request_id":  uuid.New().String(),
		"actor_id":    accountID,
		"action_data": map[string]any{
			"members_ids": []int64{},
			"name":        name,
		},
	})
	if err != nil {
		return 0, fmt.Errorf("group chat create: %w", err)
	}
	if status != http.StatusOK {
		return 0, &StatusError{Operation: "group chat create", Status: status, Body: common.BodySnippet(raw)}
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return 0, fmt.Errorf("group chat create decode: %w", err)
	}
	if result.Data.ChatID <= 0 {
		return 0, fmt.Errorf("group chat create: no chat_id in %s", common.BodySnippet(raw))
	}
	return result.Data.ChatID, nil
}

func FetchInviteChatID(client *http.Client, accessToken, inviteKey string) (int64, error) {
	var result struct {
		ChatID        int64 `json:"chat_id"`
		AlreadyMember bool  `json:"already_member"`
	}
	raw, status, err := apiDo(client, http.MethodGet, "/messenger/v2/chats/invite/"+url.PathEscape(inviteKey)+"/preview", accessToken, nil)
	if err != nil {
		return 0, fmt.Errorf("invite preview: %w", err)
	}
	if status != http.StatusOK {
		return 0, &StatusError{Operation: "invite preview", Status: status, Body: common.BodySnippet(raw)}
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return 0, fmt.Errorf("invite preview decode: %w", err)
	}
	if result.ChatID <= 0 {
		return 0, fmt.Errorf("invite preview: no chat_id in %s", common.BodySnippet(raw))
	}
	if !result.AlreadyMember {
		return 0, fmt.Errorf("invite preview: the account is not a member of chat %d, join it in the Blink app first", result.ChatID)
	}
	return result.ChatID, nil
}

func apiDo(client *http.Client, method, path, bearer string, body any) ([]byte, int, error) {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		reader = bytes.NewReader(payload)
	}
	request, err := http.NewRequest(method, APIBase+path, reader)
	if err != nil {
		return nil, 0, err
	}
	for name, values := range headless.ChromeWindows.Headers(headless.DestEmpty) {
		request.Header[name] = values
	}
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Origin", Origin)
	request.Header.Set("Referer", Origin+"/")
	request.Header.Set("X-User-Agent", ClientUserAgent)
	if client == nil {
		client = headless.ChromeWindows.HTTPClient()
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	return raw, response.StatusCode, err
}
