//go:build integration
// +build integration

package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"
)

type guestInfo struct {
	ID          string
	AccessToken string
}

type userInfo struct {
	ID           string
	AccessToken  string
	RefreshToken string
	Email        string
	Username     string
}

func envOrDefault(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func createGuest(t *testing.T, baseURL, displayName string) guestInfo {
	t.Helper()

	// GuestRequest only expects device_fingerprint (optional), username is auto-generated
	payload := map[string]string{
		"device_fingerprint": fmt.Sprintf("%s-%d", displayName, time.Now().UnixNano()),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal guest payload: %v", err)
	}

	resp, err := http.Post(fmt.Sprintf("%s/v1/auth/guest", baseURL), "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create guest request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		var errResp map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errResp)
		t.Fatalf("unexpected guest response status: %d, error: %v", resp.StatusCode, errResp)
	}

	var out struct {
		GuestID     string `json:"guest_id"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode guest response failed: %v", err)
	}

	if out.AccessToken == "" {
		t.Fatalf("empty access token in guest response")
	}

	return guestInfo{
		ID:          out.GuestID,
		AccessToken: out.AccessToken,
	}
}

func createRegisteredUser(t *testing.T, baseURL, email, password string) userInfo {
	t.Helper()

	payload := map[string]string{
		"email":    email,
		"password": password,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal register payload: %v", err)
	}

	resp, err := http.Post(fmt.Sprintf("%s/v1/auth/register", baseURL), "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("register request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		var errResp map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errResp)
		t.Fatalf("unexpected register response status: %d, error: %v", resp.StatusCode, errResp)
	}

	var out struct {
		UserID      string `json:"user_id"`
		AccessToken string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode register response failed: %v", err)
	}

	if out.AccessToken == "" {
		t.Fatalf("empty access token in register response")
	}

	return userInfo{
		ID:           out.UserID,
		AccessToken:  out.AccessToken,
		RefreshToken: out.RefreshToken,
		Email:        email,
	}
}

func loginUser(t *testing.T, baseURL, email, password string) userInfo {
	t.Helper()

	payload := map[string]string{
		"email":    email,
		"password": password,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal login payload: %v", err)
	}

	resp, err := http.Post(fmt.Sprintf("%s/v1/auth/login", baseURL), "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errResp)
		t.Fatalf("unexpected login response status: %d, error: %v", resp.StatusCode, errResp)
	}

	var out struct {
		UserID       string `json:"user_id"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode login response failed: %v", err)
	}

	if out.AccessToken == "" {
		t.Fatalf("empty access token in login response")
	}

	return userInfo{
		ID:           out.UserID,
		AccessToken:  out.AccessToken,
		RefreshToken: out.RefreshToken,
		Email:        email,
	}
}

func createRoom(t *testing.T, baseURL, accessToken string) string {
	t.Helper()

	payload := map[string]interface{}{
		"match_name":           "Test Room",
		"max_players":           2,
		"question_count":       5,
		"per_question_seconds": 15,
		"category":             "general",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal create room payload: %v", err)
	}

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/v1/rooms", baseURL), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create request failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create room request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		var errResp map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errResp)
		t.Fatalf("unexpected create room response status: %d, error: %v", resp.StatusCode, errResp)
	}

	var out struct {
		RoomCode string `json:"room_code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode create room response failed: %v", err)
	}

	if out.RoomCode == "" {
		t.Fatalf("empty room code in response")
	}

	return out.RoomCode
}

func getRoom(t *testing.T, baseURL, roomCode string) map[string]interface{} {
	t.Helper()

	resp, err := http.Get(fmt.Sprintf("%s/v1/rooms/%s", baseURL, roomCode))
	if err != nil {
		t.Fatalf("get room request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errResp)
		t.Fatalf("unexpected get room response status: %d, error: %v", resp.StatusCode, errResp)
	}

	var room map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&room); err != nil {
		t.Fatalf("decode get room response failed: %v", err)
	}

	return room
}

func makeAuthenticatedRequest(t *testing.T, method, url, accessToken string, payload interface{}) *http.Response {
	t.Helper()

	var body *bytes.Reader
	if payload != nil {
		bodyBytes, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload failed: %v", err)
		}
		body = bytes.NewReader(bodyBytes)
	} else {
		body = bytes.NewReader([]byte{})
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("create request failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if accessToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	return resp
}
