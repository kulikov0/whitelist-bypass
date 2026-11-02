package blink

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"whitelist-bypass/relay/common"
)

const (
	AccessTokenEntry          = "blink_access_token"
	AccessTokenExpiresAtEntry = "blink_access_token_expires_at"
	RefreshTokenEntry         = "blink_refresh_token"

	accessTokenRefreshMargin = time.Hour
	loginPollInterval        = 5 * time.Second
	loginDefaultTTL          = 600
	loginRenewFraction       = 0.8
)

type Credentials struct {
	path   string
	client *http.Client
	logFn  func(string, ...any)
	tokens TokenPair
}

func LoadCredentials(path string, client *http.Client, logFn func(string, ...any)) (*Credentials, error) {
	tokens, err := readTokens(path)
	if err != nil {
		return nil, err
	}
	if tokens.RefreshToken == "" {
		return nil, fmt.Errorf("%s has no %s, run with --login first", path, RefreshTokenEntry)
	}
	return &Credentials{path: path, client: client, logFn: logFn, tokens: tokens}, nil
}

func SaveTokens(path string, tokens TokenPair) error {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(path, []byte("[]"), 0o600); err != nil {
			return err
		}
	}
	return common.UpdateCookieFile(path, map[string]string{
		AccessTokenEntry:          tokens.Token,
		AccessTokenExpiresAtEntry: strconv.FormatInt(tokens.TokenExpiresAt, 10),
		RefreshTokenEntry:         tokens.RefreshToken,
	})
}

func LoginToFile(path string, client *http.Client, stop <-chan struct{}, statusFn func(string), logFn func(string, ...any)) error {
	tokens, err := waitForLogin(client, stop, statusFn, logFn)
	if err != nil {
		return err
	}
	if err := SaveTokens(path, tokens); err != nil {
		return fmt.Errorf("save tokens to %s: %w", path, err)
	}
	logFn("[login] approved, tokens saved to %s", path)
	return nil
}

func (c *Credentials) AccessToken() (string, error) {
	if accessTokenFresh(c.tokens) {
		return c.tokens.Token, nil
	}
	if err := c.refresh(); err != nil {
		return "", err
	}
	return c.tokens.Token, nil
}

func (c *Credentials) ConnectCall(chatID int64) (CallConnection, error) {
	return withAccessToken(c, "calls connect", func(accessToken string) (CallConnection, error) {
		return ConnectCall(c.client, accessToken, chatID)
	})
}

func (c *Credentials) ResolveChatID(room string) (int64, error) {
	inviteKey, isInvite := ParseInviteKey(room)
	if !isInvite {
		return ParseChatID(room)
	}
	return withAccessToken(c, "invite preview", func(accessToken string) (int64, error) {
		return FetchInviteChatID(c.client, accessToken, inviteKey)
	})
}

func (c *Credentials) CreateChat(name string) (int64, error) {
	return withAccessToken(c, "group chat create", func(accessToken string) (int64, error) {
		accountID, err := FetchAccountID(c.client, accessToken)
		if err != nil {
			return 0, err
		}
		return CreateGroupChat(c.client, accessToken, accountID, name)
	})
}

func (c *Credentials) refresh() error {
	staleRefreshToken := c.tokens.RefreshToken
	stored, err := readTokens(c.path)
	if err != nil {
		c.logFn("[auth] could not re-read %s, refreshing with the tokens in memory: %v", c.path, err)
	} else if stored.RefreshToken != "" {
		c.tokens = stored
	}
	if c.tokens.RefreshToken != staleRefreshToken && accessTokenFresh(c.tokens) {
		c.logFn("[auth] tokens were already refreshed by another process, loaded them from %s", c.path)
		return nil
	}
	fresh, err := RefreshTokens(c.client, c.tokens.RefreshToken)
	if err != nil {
		return err
	}
	c.tokens = fresh
	if err := SaveTokens(c.path, fresh); err != nil {
		return fmt.Errorf("tokens refreshed but not saved to %s: %w", c.path, err)
	}
	c.logFn("[auth] tokens refreshed and saved, access expires %s",
		time.Unix(fresh.TokenExpiresAt, 0).UTC().Format(time.RFC3339))
	return nil
}

func withAccessToken[T any](c *Credentials, operation string, request func(accessToken string) (T, error)) (T, error) {
	var empty T
	accessToken, err := c.AccessToken()
	if err != nil {
		return empty, err
	}
	result, err := request(accessToken)
	if !IsUnauthorized(err) {
		return result, err
	}
	c.logFn("[auth] %s returned 401, refreshing tokens", operation)
	if err := c.refresh(); err != nil {
		return empty, err
	}
	return request(c.tokens.Token)
}

func readTokens(path string) (TokenPair, error) {
	var tokens TokenPair
	raw, err := os.ReadFile(path)
	if err != nil {
		return tokens, err
	}
	var entries []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return tokens, fmt.Errorf("parse %s: %w", path, err)
	}
	for _, entry := range entries {
		switch entry.Name {
		case AccessTokenEntry:
			tokens.Token = entry.Value
		case AccessTokenExpiresAtEntry:
			tokens.TokenExpiresAt, _ = strconv.ParseInt(entry.Value, 10, 64)
		case RefreshTokenEntry:
			tokens.RefreshToken = entry.Value
		}
	}
	return tokens, nil
}

func accessTokenFresh(tokens TokenPair) bool {
	expiresIn := time.Until(time.Unix(tokens.TokenExpiresAt, 0))
	return tokens.Token != "" && expiresIn > accessTokenRefreshMargin
}

func waitForLogin(client *http.Client, stop <-chan struct{}, statusFn func(string), logFn func(string, ...any)) (TokenPair, error) {
	for {
		request, err := RequestLogin(client)
		if err != nil {
			return TokenPair{}, err
		}
		link := LoginLink(request.WAT)
		logFn("[login] open this link on the phone with the Blink app: %s", link)
		statusFn(common.StatusLoginLink + ":" + link)
		ttl := request.TTL
		if ttl <= 0 {
			ttl = loginDefaultTTL
		}
		renewAt := time.Now().Add(time.Duration(float64(ttl)*loginRenewFraction) * time.Second)
		for time.Now().Before(renewAt) {
			select {
			case <-stop:
				return TokenPair{}, ErrLoginCancelled
			case <-time.After(loginPollInterval):
			}
			tokens, err := PollLogin(client, request.WAT)
			if err == nil {
				return tokens, nil
			}
			if !errors.Is(err, ErrLoginPending) {
				logFn("[login] poll: %v", err)
			}
		}
		logFn("[login] login link expired, requesting a new one")
	}
}
