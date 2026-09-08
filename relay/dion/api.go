package dion

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kulikov0/headless-client"
	"whitelist-bypass/relay/common"
)

var ErrSessionExpired = errors.New("dion: session expired, re-login required")

var errLoginEndpointMissing = errors.New("dion: login endpoint not available")

const (
	accessCookieName  = "vc-access-token"
	refreshCookieName = "vc-refresh-token"

	loginClientsPath  = "/v2/users/login/web"
	loginPlatformPath = "/platform/v2/auth/auth-providers/dion/login/password"
)

const (
	refreshSkewSeconds   = 60
	refreshMaxAttempts   = 3
	refreshBaseDelay     = 2 * time.Second
	refreshDelayMultiply = 1.75
)

const (
	APIBase        = "https://api.dion.vc"
	APIClientsBase = "https://api-clients.dion.vc"
	WebBase        = "https://dion.vc"
	Origin         = "https://dion.vc"
	CookieDomain   = "dion.vc"
)

// ParseRoom accepts a bare room id, a dion://<id> link, or a
// https://dion.vc/event/<id> URL and returns the room id. Trailing query
// strings and path segments are stripped.
func ParseRoom(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return ""
	}
	trimmed = strings.TrimPrefix(trimmed, "dion://")
	trimmed = strings.TrimPrefix(trimmed, "https://")
	trimmed = strings.TrimPrefix(trimmed, "http://")
	trimmed = strings.TrimPrefix(trimmed, "dion.vc/")
	trimmed = strings.TrimPrefix(trimmed, "event/")
	if idx := strings.Index(trimmed, "?"); idx >= 0 {
		trimmed = trimmed[:idx]
	}
	if idx := strings.Index(trimmed, "/"); idx >= 0 {
		trimmed = trimmed[:idx]
	}
	return trimmed
}

type GuestUser struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Email             string   `json:"email"`
	Initials          string   `json:"initials"`
	Position          string   `json:"position"`
	AvatarHTTPPath    string   `json:"avatar_http_path"`
	IsProfileFilledIn bool     `json:"is_profile_filled_in"`
	Roles             []string `json:"roles"`
}

type GuestAuthResponse struct {
	AccessToken  string    `json:"access_token"`
	AuthProvider string    `json:"auth_provider"`
	IsAuthBySSO  bool      `json:"is_auth_by_sso"`
	User         GuestUser `json:"user"`
}

type LoginResponse struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	AuthProvider string    `json:"auth_provider"`
	IsAuthBySSO  bool      `json:"is_auth_by_sso"`
	User         GuestUser `json:"user"`
}

type EventInfo struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Slug   string   `json:"slug"`
	OrgID  string   `json:"org_id"`
	Admins []string `json:"admins"`
	PSTN   struct {
		Number string `json:"number"`
		Pin    int    `json:"pin"`
		Prefix string `json:"prefix"`
	} `json:"pstn"`
}

type WSSConnectResponse struct {
	Host   string            `json:"host"`
	Path   string            `json:"path"`
	Schema string            `json:"schema"`
	URL    string            `json:"url"`
	Params map[string]string `json:"params"`
}

type Session struct {
	HTTPClient     *http.Client
	Device         common.DeviceProfile
	AccessToken    string
	AccessTokenExp time.Time
	UserID         string
	SessionID      string
	cookiesPath    string
	email          string
	password       string
	refreshMu      sync.Mutex
}

func (s *Session) setBaseHeaders(req *http.Request, accessToken string) {
	req.Header.Set("User-Agent", s.Device.UserAgent)
	req.Header.Set("Origin", Origin)
	req.Header.Set("Referer", Origin+"/")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en")
	req.Header.Set("X-Request-Id", uuid.New().String())
	for name, value := range deviceHeaders(s.Device) {
		req.Header.Set(name, value)
	}
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
}

func parseJWTExpiry(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return time.Time{}, fmt.Errorf("invalid JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, fmt.Errorf("decode payload: %w", err)
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return time.Time{}, fmt.Errorf("parse claims: %w", err)
	}
	if claims.Exp == 0 {
		return time.Time{}, fmt.Errorf("no exp claim")
	}
	return time.Unix(claims.Exp, 0), nil
}

func NewSession(httpClient *http.Client) (*Session, error) {
	if httpClient == nil {
		jar, err := cookiejar.New(nil)
		if err != nil {
			return nil, fmt.Errorf("cookiejar: %w", err)
		}
		httpClient = &http.Client{Jar: jar, Transport: headless.ChromeWindows.HTTPClient().Transport}
	} else if httpClient.Jar == nil {
		jar, err := cookiejar.New(nil)
		if err != nil {
			return nil, fmt.Errorf("cookiejar: %w", err)
		}
		httpClient.Jar = jar
	}
	return &Session{HTTPClient: httpClient, Device: common.RandomDeviceProfile()}, nil
}

func (s *Session) RegisterGuest() (*GuestAuthResponse, error) {
	auth, err := s.callRefreshOnce()
	if err != nil {
		return nil, err
	}
	s.applyRefreshResult(auth)
	if s.cookiesPath != "" {
		if saveErr := s.SaveCookiesToFile(s.cookiesPath); saveErr != nil {
			return nil, fmt.Errorf("refresh ok but save cookies failed: %w", saveErr)
		}
	}
	return auth, nil
}

// RegisterAnonymousGuest seeds anonymous credentials via
// POST /platform/v1/users/register/guest when the cookie jar has no prior
// session. Required for cold guest joiners that have never authenticated.
// The server response carries an access_token directly and rotates the refresh
// cookie via Set-Cookie. Caller usually wants to follow up with Refresh() if
// they need to validate persistence.
func (s *Session) RegisterAnonymousGuest(eventID, displayName string) (*GuestAuthResponse, error) {
	if eventID == "" {
		return nil, fmt.Errorf("empty event_id")
	}
	if displayName == "" {
		displayName = "Guest"
	}
	body, _ := json.Marshal(map[string]any{
		"event_id": eventID,
		"name":     displayName,
	})
	req, err := http.NewRequest(http.MethodPost, APIBase+"/platform/v1/users/register/guest", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	s.setBaseHeaders(req, "")
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("register/guest: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("register/guest: status %d: %s", resp.StatusCode, string(raw))
	}
	var auth GuestAuthResponse
	if err := json.Unmarshal(raw, &auth); err != nil {
		return nil, fmt.Errorf("register/guest decode: %w", err)
	}
	if auth.AccessToken == "" {
		return nil, fmt.Errorf("register/guest: empty access_token: %s", string(raw))
	}
	s.applyRefreshResult(&auth)
	if s.cookiesPath != "" {
		if saveErr := s.SaveCookiesToFile(s.cookiesPath); saveErr != nil {
			return nil, fmt.Errorf("register/guest ok but save cookies failed: %w", saveErr)
		}
	}
	return &auth, nil
}

// LoginWithPassword exchanges credentials for a fresh token pair. The web
// front-end posts to api-clients, and switches to the platform endpoint when
// the DION_PLATFORM_COOKIE_AUTH_ENABLED toggle is on, so both are tried.
func (s *Session) LoginWithPassword(email, password string) error {
	if email == "" || password == "" {
		return fmt.Errorf("login: email and password are required")
	}
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	login, err := s.postLogin(APIClientsBase+loginClientsPath, body)
	if errors.Is(err, errLoginEndpointMissing) {
		login, err = s.postLogin(APIBase+loginPlatformPath, body)
	}
	if err != nil {
		return err
	}
	s.email = email
	s.password = password
	s.applyLoginResult(login)
	if s.cookiesPath != "" {
		if saveErr := s.SaveCookiesToFile(s.cookiesPath); saveErr != nil {
			return fmt.Errorf("login ok but save cookies failed: %w", saveErr)
		}
	}
	return nil
}

func (s *Session) postLogin(target string, body []byte) (*LoginResponse, error) {
	req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	s.setBaseHeaders(req, "")
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		return nil, errLoginEndpointMissing
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("login: status %d: %s", resp.StatusCode, string(raw))
	}
	var login LoginResponse
	if err := json.Unmarshal(raw, &login); err != nil {
		return nil, fmt.Errorf("login decode: %w", err)
	}
	if login.AccessToken == "" {
		return nil, fmt.Errorf("login: empty access_token: %s", string(raw))
	}
	return &login, nil
}

func (s *Session) applyLoginResult(login *LoginResponse) {
	s.AccessToken = login.AccessToken
	s.UserID = login.User.ID
	if exp, err := parseJWTExpiry(login.AccessToken); err == nil {
		s.AccessTokenExp = exp
	}
	if login.RefreshToken != "" {
		s.SetCookieInJar(refreshCookieName, login.RefreshToken)
	}
	s.SetCookieInJar(accessCookieName, login.AccessToken)
}

func (s *Session) callRefreshOnce() (*GuestAuthResponse, error) {
	req, err := http.NewRequest(http.MethodPost, APIBase+"/platform/v1/auth/refresh/web", bytes.NewReader(nil))
	if err != nil {
		return nil, err
	}
	s.setBaseHeaders(req, "")
	req.Header.Set("Content-Length", "0")

	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("auth/refresh/web: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return nil, fmt.Errorf("%w: status %d: %s", ErrSessionExpired, resp.StatusCode, string(raw))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("auth/refresh/web: status %d: %s", resp.StatusCode, string(raw))
	}
	var auth GuestAuthResponse
	if err := json.Unmarshal(raw, &auth); err != nil {
		return nil, fmt.Errorf("auth/refresh/web decode: %w", err)
	}
	if auth.AccessToken == "" {
		return nil, fmt.Errorf("auth/refresh/web: empty access_token: %s", string(raw))
	}
	return &auth, nil
}

func (s *Session) applyRefreshResult(auth *GuestAuthResponse) {
	s.AccessToken = auth.AccessToken
	s.UserID = auth.User.ID
	if exp, err := parseJWTExpiry(auth.AccessToken); err == nil {
		s.AccessTokenExp = exp
	}
	s.SetCookieInJar(accessCookieName, auth.AccessToken)
}

// Refresh runs /auth/refresh/web with single-flight locking, retries on
// transient 5xx and network errors, treats 4xx as terminal session-expired,
// and persists the rotated cookie jar to disk on success.
func (s *Session) Refresh() error {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	return s.refreshLocked()
}

func (s *Session) refreshLocked() error {
	if !s.HasRefreshCookie() && s.HasCredentials() {
		return s.LoginWithPassword(s.email, s.password)
	}
	var lastErr error
	delay := refreshBaseDelay
	for attempt := 1; attempt <= refreshMaxAttempts; attempt++ {
		auth, err := s.callRefreshOnce()
		if err == nil {
			s.applyRefreshResult(auth)
			if s.cookiesPath != "" {
				if saveErr := s.SaveCookiesToFile(s.cookiesPath); saveErr != nil {
					return fmt.Errorf("refresh ok but save cookies failed: %w", saveErr)
				}
			}
			return nil
		}
		if errors.Is(err, ErrSessionExpired) {
			if s.HasCredentials() {
				return s.LoginWithPassword(s.email, s.password)
			}
			return err
		}
		lastErr = err
		if attempt < refreshMaxAttempts {
			time.Sleep(delay)
			delay = time.Duration(float64(delay) * refreshDelayMultiply)
		}
	}
	return fmt.Errorf("refresh failed after %d attempts: %w", refreshMaxAttempts, lastErr)
}

// EnsureValidToken refreshes if the cached access token is missing or expires
// within refreshSkewSeconds. Safe to call from multiple goroutines.
func (s *Session) EnsureValidToken() error {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	if s.AccessToken != "" && !s.AccessTokenExp.IsZero() &&
		time.Until(s.AccessTokenExp) > time.Duration(refreshSkewSeconds)*time.Second {
		return nil
	}
	return s.refreshLocked()
}

// DoAuthenticated sends a request with the current access token, refreshes
// once on a 401, and retries the request. The buildRequest closure must be
// idempotent so the retry can construct a fresh request with a fresh body.
func (s *Session) DoAuthenticated(buildRequest func() (*http.Request, error)) (*http.Response, error) {
	if err := s.EnsureValidToken(); err != nil {
		return nil, err
	}
	req, err := buildRequest()
	if err != nil {
		return nil, err
	}
	s.setBaseHeaders(req, s.AccessToken)
	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}
	staleToken := s.AccessToken
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	s.refreshMu.Lock()
	if s.AccessToken == staleToken {
		if err := s.refreshLocked(); err != nil {
			s.refreshMu.Unlock()
			return nil, err
		}
	}
	s.refreshMu.Unlock()
	retryReq, err := buildRequest()
	if err != nil {
		return nil, err
	}
	s.setBaseHeaders(retryReq, s.AccessToken)
	return s.HTTPClient.Do(retryReq)
}

func (s *Session) WhoAmI() (json.RawMessage, error) {
	resp, err := s.DoAuthenticated(func() (*http.Request, error) {
		return http.NewRequest(http.MethodGet, APIBase+"/platform/v1/whoami", nil)
	})
	if err != nil {
		return nil, fmt.Errorf("whoami: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("whoami: status %d: %s", resp.StatusCode, string(raw))
	}
	return raw, nil
}

func (s *Session) GetEventBySlug(slug string) (*EventInfo, error) {
	if slug == "" {
		return nil, fmt.Errorf("empty room ID")
	}
	eventURL := fmt.Sprintf("%s/conference/v1/events/slug/%s", APIBase, slug)
	resp, err := s.DoAuthenticated(func() (*http.Request, error) {
		req, err := http.NewRequest(http.MethodGet, eventURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	})
	if err != nil {
		return nil, fmt.Errorf("get event: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get event: status %d: %s", resp.StatusCode, string(raw))
	}

	var event EventInfo
	if err := json.Unmarshal(raw, &event); err != nil {
		return nil, fmt.Errorf("get event decode: %w", err)
	}
	if event.ID == "" {
		return nil, fmt.Errorf("get event: empty id: %s", string(raw))
	}
	return &event, nil
}

func (s *Session) GenerateSlug() (string, error) {
	resp, err := s.DoAuthenticated(func() (*http.Request, error) {
		return http.NewRequest(http.MethodGet, APIClientsBase+"/v2/events/slug/generate", nil)
	})
	if err != nil {
		return "", fmt.Errorf("generate room ID: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("generate room ID: status %d: %s", resp.StatusCode, string(raw))
	}

	var out struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("generate room ID decode: %w", err)
	}
	if out.Slug == "" {
		return "", fmt.Errorf("generate room ID: empty: %s", string(raw))
	}
	return out.Slug, nil
}

type CreateEventOptions struct {
	Slug             string
	EventParams      []string
	IsImpersonalSlug bool
	IsOnCloud        bool
}

func (s *Session) CreateEvent(opts CreateEventOptions) (*EventInfo, error) {
	if opts.Slug == "" {
		return nil, fmt.Errorf("empty room ID")
	}
	if opts.EventParams == nil {
		opts.EventParams = []string{"guest_access"}
	}
	body, _ := json.Marshal(map[string]any{
		"event_params":       opts.EventParams,
		"is_impersonal_slug": opts.IsImpersonalSlug,
		"is_on_cloud":        opts.IsOnCloud,
		"slug":               opts.Slug,
	})
	resp, err := s.DoAuthenticated(func() (*http.Request, error) {
		req, err := http.NewRequest(http.MethodPost, APIBase+"/conference/v1/events", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	})
	if err != nil {
		return nil, fmt.Errorf("create event: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("create event: status %d: %s", resp.StatusCode, string(raw))
	}

	var event EventInfo
	if err := json.Unmarshal(raw, &event); err != nil {
		return nil, fmt.Errorf("create event decode: %w", err)
	}
	if event.ID == "" {
		return nil, fmt.Errorf("create event: empty id: %s", string(raw))
	}
	return &event, nil
}

func (s *Session) CreateRoom() (*EventInfo, error) {
	slug, err := s.GenerateSlug()
	if err != nil {
		return nil, err
	}
	return s.CreateEvent(CreateEventOptions{
		Slug:             slug,
		EventParams:      []string{"guest_access"},
		IsImpersonalSlug: true,
		IsOnCloud:        true,
	})
}

func (s *Session) ConnectWSS(sessionID string) (*WSSConnectResponse, error) {
	if sessionID == "" {
		sessionID = uuid.New().String()
	}
	body, _ := json.Marshal(map[string]string{"session_id": sessionID})
	resp, err := s.DoAuthenticated(func() (*http.Request, error) {
		req, err := http.NewRequest(http.MethodPost, APIBase+"/conference/v1/connect/wss", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	})
	if err != nil {
		return nil, fmt.Errorf("connect/wss: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("connect/wss: status %d: %s", resp.StatusCode, string(raw))
	}

	var wss WSSConnectResponse
	if err := json.Unmarshal(raw, &wss); err != nil {
		return nil, fmt.Errorf("connect/wss decode: %w", err)
	}
	if wss.URL == "" {
		return nil, fmt.Errorf("connect/wss: empty url: %s", string(raw))
	}
	s.SessionID = sessionID
	return &wss, nil
}

type AuthResult struct {
	Session   *Session
	Event     *EventInfo
	WSS       *WSSConnectResponse
	SessionID string
}

// LookupEventBySlugAnonymous calls the public conference slug endpoint without
// an access token. The server allows unauthenticated GETs and returns 404 for
// unknown slugs, 200 with EventInfo for known ones. Used to bootstrap anonymous
// guest joiners that need event_id before they can RegisterAnonymousGuest.
func (s *Session) LookupEventBySlugAnonymous(slug string) (*EventInfo, error) {
	if slug == "" {
		return nil, fmt.Errorf("empty room ID")
	}
	eventURL := fmt.Sprintf("%s/conference/v1/events/slug/%s", APIBase, slug)
	req, err := http.NewRequest(http.MethodGet, eventURL, nil)
	if err != nil {
		return nil, err
	}
	s.setBaseHeaders(req, "")
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get event anon: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get event anon: status %d: %s", resp.StatusCode, string(raw))
	}
	var event EventInfo
	if err := json.Unmarshal(raw, &event); err != nil {
		return nil, fmt.Errorf("get event anon decode: %w", err)
	}
	if event.ID == "" {
		return nil, fmt.Errorf("get event anon: empty id: %s", string(raw))
	}
	return &event, nil
}

// JoinAsGuest runs the full cold-start anonymous guest flow: HTML prime ->
// unauthenticated slug lookup -> RegisterAnonymousGuest -> returns a ready
// Session paired with the resolved EventInfo. Caller can then proceed to
// ConnectWSS / DialSignaling / BuildPionPeer (or use NewCall to do all of that).
func JoinAsGuest(httpClient *http.Client, slug, displayName string) (*Session, *EventInfo, error) {
	session, err := NewSession(httpClient)
	if err != nil {
		return nil, nil, err
	}
	if err := session.PrimeCookies(slug); err != nil {
		return nil, nil, fmt.Errorf("prime cookies: %w", err)
	}
	event, err := session.LookupEventBySlugAnonymous(slug)
	if err != nil {
		return nil, nil, err
	}
	if _, err := session.RegisterAnonymousGuest(event.ID, displayName); err != nil {
		return nil, nil, fmt.Errorf("RegisterAnonymousGuest: %w", err)
	}
	return session, event, nil
}

func AuthAndGetTicket(httpClient *http.Client, slug string) (*AuthResult, error) {
	session, err := NewSession(httpClient)
	if err != nil {
		return nil, err
	}
	if err := session.PrimeCookies(slug); err != nil {
		return nil, err
	}
	if _, err := session.RegisterGuest(); err != nil {
		return nil, err
	}
	if _, err := session.WhoAmI(); err != nil {
		return nil, fmt.Errorf("whoami after guest auth: %w", err)
	}
	event, err := session.GetEventBySlug(slug)
	if err != nil {
		return nil, err
	}
	sessionID := uuid.New().String()
	wss, err := session.ConnectWSS(sessionID)
	if err != nil {
		return nil, err
	}
	return &AuthResult{
		Session:   session,
		Event:     event,
		WSS:       wss,
		SessionID: sessionID,
	}, nil
}
