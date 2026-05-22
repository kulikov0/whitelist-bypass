package wbstream

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"whitelist-bypass/relay/common"
)

const (
	APIBase = "https://stream.wb.ru"
	Origin  = "https://stream.wb.ru"
)

// ParseRoomID accepts a bare room id, a wbstream://<id> link, or a
// https://stream.wb.ru/room/<id> URL and returns the room id.
func ParseRoomID(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return ""
	}
	if rest, ok := strings.CutPrefix(trimmed, "wbstream://"); ok {
		return strings.Trim(rest, "/")
	}
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		u, err := url.Parse(trimmed)
		if err == nil {
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			for i := 0; i < len(parts)-1; i++ {
				if parts[i] == "room" && parts[i+1] != "" {
					return parts[i+1]
				}
			}
		}
	}
	return strings.Trim(trimmed, "/")
}

type guestRegisterRequest struct {
	DisplayName string         `json:"displayName"`
	Device      guestDeviceCfg `json:"device"`
}

type guestDeviceCfg struct {
	DeviceName string `json:"deviceName"`
	DeviceType string `json:"deviceType"`
}

type guestRegisterResponse struct {
	AccessToken string `json:"accessToken"`
}

type createRoomRequest struct {
	RoomType    string `json:"roomType"`
	RoomPrivacy string `json:"roomPrivacy"`
}

type createRoomResponse struct {
	RoomID string `json:"roomId"`
}

type connectionDetailsResponse struct {
	RoomToken string `json:"roomToken"`
	ServerURL string `json:"serverUrl"`
}

func httpDo(client *http.Client, req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", common.UserAgent)
	if client == nil {
		client = http.DefaultClient
	}
	return client.Do(req)
}

type cookieTransport struct {
	base   http.RoundTripper
	cookie string
}

func (t *cookieTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Cookie", t.cookie)
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

func clientWithCookies(client *http.Client, cookieHeader string) *http.Client {
	if cookieHeader == "" {
		return client
	}
	if client == nil {
		client = &http.Client{}
	}
	wrapped := *client
	wrapped.Transport = &cookieTransport{base: client.Transport, cookie: cookieHeader}
	return &wrapped
}

func setBearer(req *http.Request, accessToken string) {
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
}

func RegisterGuest(client *http.Client, displayName string) (string, error) {
	body, _ := json.Marshal(guestRegisterRequest{
		DisplayName: displayName,
		Device: guestDeviceCfg{
			DeviceName: "Linux",
			DeviceType: "PARTICIPANT_DEVICE_TYPE_WEB_DESKTOP",
		},
	})
	req, err := http.NewRequest(http.MethodPost, APIBase+"/auth/api/v1/auth/user/guest-register", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpDo(client, req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("guest-register: status %d: %s", resp.StatusCode, string(raw))
	}

	var r guestRegisterResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", fmt.Errorf("guest-register decode: %w", err)
	}
	return r.AccessToken, nil
}

func CreateRoom(client *http.Client, accessToken string) (string, error) {
	body, _ := json.Marshal(createRoomRequest{
		RoomType:    "ROOM_TYPE_ALL_ON_SCREEN",
		RoomPrivacy: "ROOM_PRIVACY_FREE",
	})
	req, err := http.NewRequest(http.MethodPost, APIBase+"/api-room/api/v2/room", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	setBearer(req, accessToken)

	resp, err := httpDo(client, req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("create-room: status %d: %s", resp.StatusCode, string(raw))
	}

	var r createRoomResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", fmt.Errorf("create-room decode: %w", err)
	}
	return r.RoomID, nil
}

func JoinRoom(client *http.Client, accessToken, roomID string) error {
	url := fmt.Sprintf("%s/api-room/api/v1/room/%s/join", APIBase, roomID)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	setBearer(req, accessToken)

	resp, err := httpDo(client, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("join-room: status %d: %s", resp.StatusCode, string(raw))
	}
	return nil
}

func GetConnectionDetails(client *http.Client, accessToken, roomID, displayName string) (string, string, error) {
	detailsURL := fmt.Sprintf("%s/api-room-manager/v2/room/%s/connection-details?deviceType=PARTICIPANT_DEVICE_TYPE_WEB_DESKTOP&displayName=%s",
		APIBase, roomID, url.QueryEscape(displayName))
	req, err := http.NewRequest(http.MethodGet, detailsURL, nil)
	if err != nil {
		return "", "", err
	}
	setBearer(req, accessToken)

	resp, err := httpDo(client, req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("connection-details: status %d: %s", resp.StatusCode, string(raw))
	}

	var r connectionDetailsResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", "", fmt.Errorf("connection-details decode: %w", err)
	}
	return r.RoomToken, r.ServerURL, nil
}

func AuthAndGetToken(client *http.Client, roomID, displayName string) (string, string, string, string, error) {
	accessToken, err := RegisterGuest(client, displayName)
	if err != nil {
		return "", "", "", "", fmt.Errorf("register guest: %w", err)
	}
	return joinAndGetDetails(client, accessToken, roomID, displayName)
}

func AuthAsLoggedIn(client *http.Client, cookieHeader, accessToken, roomID, displayName string) (string, string, string, string, error) {
	if cookieHeader == "" && accessToken == "" {
		return "", "", "", "", fmt.Errorf("cookies or access token required for logged-in auth")
	}
	client = clientWithCookies(client, cookieHeader)
	return joinAndGetDetails(client, accessToken, roomID, displayName)
}

var WBStreamCookieAllowlist = []string{
	"wbx-refresh",
	"x_wbaas_token",
	"_wbauid",
	"wbx-validation-key",
}

type slideV3Response struct {
	Payload struct {
		AccessToken string `json:"access_token"`
	} `json:"payload"`
}

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "00000000-0000-0000-0000-000000000000"
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func RefreshAccessToken(client *http.Client, cookieHeader, deviceID string) (string, error) {
	req, err := http.NewRequest(http.MethodPost, "https://auth-stream.wb.ru/v2/auth/slide-v3", bytes.NewReader(nil))
	if err != nil {
		return "", err
	}
	if deviceID == "" {
		deviceID = newRequestID()
	}
	req.Header.Set("wb-apptype", "web")
	req.Header.Set("X-Real-IP", "")
	req.Header.Set("deviceId", deviceID)
	req.Header.Set("X-Request-ID", newRequestID())
	req.Header.Set("Origin", Origin)
	req.Header.Set("Referer", Origin+"/")
	req.Header.Set("Cookie", cookieHeader)
	req.Header.Set("User-Agent", common.UserAgent)

	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("slide-v3: status %d: %s", resp.StatusCode, string(raw))
	}
	var r slideV3Response
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", fmt.Errorf("slide-v3 decode: %w", err)
	}
	if r.Payload.AccessToken == "" {
		return "", fmt.Errorf("slide-v3: empty access_token in response: %s", string(raw))
	}
	return r.Payload.AccessToken, nil
}

func joinAndGetDetails(client *http.Client, accessToken, roomID, displayName string) (string, string, string, string, error) {
	var err error
	if roomID == "" {
		roomID, err = CreateRoom(client, accessToken)
		if err != nil {
			return "", "", "", "", fmt.Errorf("create room: %w", err)
		}
	}
	if err := JoinRoom(client, accessToken, roomID); err != nil {
		return "", "", "", "", fmt.Errorf("join room: %w", err)
	}
	roomToken, serverURL, err := GetConnectionDetails(client, accessToken, roomID, displayName)
	if err != nil {
		return "", "", "", "", fmt.Errorf("get connection details: %w", err)
	}
	return roomID, roomToken, accessToken, serverURL, nil
}

func KickParticipant(client *http.Client, accessToken, roomID, participantID string) error {
	if client == nil {
		client = http.DefaultClient
	}
	kickURL := fmt.Sprintf("%s/api-room-manager/api/v1/room/%s/participant/%s/kick", APIBase, roomID, participantID)
	req, err := http.NewRequest("DELETE", kickURL, strings.NewReader("{}"))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", common.UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("kick %s -> %d %s", participantID, resp.StatusCode, string(body))
	}
	return nil
}
