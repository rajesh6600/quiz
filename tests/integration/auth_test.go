//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestRegisterFlow(t *testing.T) {
	baseURL := envOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")
	email := fmt.Sprintf("test-%d@example.com", time.Now().UnixNano())
	password := "testpassword123"

	user := createRegisteredUser(t, baseURL, email, password)

	if user.ID == "" {
		t.Fatal("user ID is empty")
	}
	if user.AccessToken == "" {
		t.Fatal("access token is empty")
	}
	if user.RefreshToken == "" {
		t.Fatal("refresh token is empty")
	}
}

func TestLoginFlow(t *testing.T) {
	baseURL := envOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")
	email := fmt.Sprintf("test-%d@example.com", time.Now().UnixNano())
	password := "testpassword123"

	// First register
	_ = createRegisteredUser(t, baseURL, email, password)

	// Then login
	user := loginUser(t, baseURL, email, password)

	if user.ID == "" {
		t.Fatal("user ID is empty")
	}
	if user.AccessToken == "" {
		t.Fatal("access token is empty")
	}
	if user.RefreshToken == "" {
		t.Fatal("refresh token is empty")
	}
}

func TestGuestCreation(t *testing.T) {
	baseURL := envOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")
	guest := createGuest(t, baseURL, "TestGuest")

	if guest.ID == "" {
		t.Fatal("guest ID is empty")
	}
	if guest.AccessToken == "" {
		t.Fatal("access token is empty")
	}
}

func TestGuestConversion(t *testing.T) {
	baseURL := envOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")
	guest := createGuest(t, baseURL, "ConvertGuest")
	email := fmt.Sprintf("convert-%d@example.com", time.Now().UnixNano())
	password := "testpassword123"

	payload := map[string]interface{}{
		"guest_id": guest.ID,
		"email":    email,
		"password": password,
	}

	resp := makeAuthenticatedRequest(t, "POST", fmt.Sprintf("%s/v1/auth/convert", baseURL), guest.AccessToken, payload)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errResp)
		t.Fatalf("unexpected convert response status: %d, error: %v", resp.StatusCode, errResp)
	}

	var out struct {
		UserID       string `json:"user_id"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Converted    bool   `json:"converted"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode convert response failed: %v", err)
	}

	if !out.Converted {
		t.Fatal("converted flag is not true")
	}
	if out.AccessToken == "" {
		t.Fatal("access token is empty")
	}
}

func TestTokenRefresh(t *testing.T) {
	baseURL := envOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")
	user := createRegisteredUser(t, baseURL, fmt.Sprintf("refresh-%d@example.com", time.Now().UnixNano()), "testpassword123")

	payload := map[string]string{
		"refresh_token": user.RefreshToken,
	}

	resp := makeAuthenticatedRequest(t, "POST", fmt.Sprintf("%s/v1/auth/refresh", baseURL), "", payload)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errResp)
		t.Fatalf("unexpected refresh response status: %d, error: %v", resp.StatusCode, errResp)
	}

	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode refresh response failed: %v", err)
	}

	if out.AccessToken == "" {
		t.Fatal("access token is empty")
	}
	if out.AccessToken == user.AccessToken {
		t.Fatal("new access token should be different from old one")
	}
}

func TestGetMe(t *testing.T) {
	baseURL := envOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")
	user := createRegisteredUser(t, baseURL, fmt.Sprintf("getme-%d@example.com", time.Now().UnixNano()), "testpassword123")

	resp := makeAuthenticatedRequest(t, "GET", fmt.Sprintf("%s/v1/users/me", baseURL), user.AccessToken, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errResp)
		t.Fatalf("unexpected get me response status: %d, error: %v", resp.StatusCode, errResp)
	}

	var out map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode get me response failed: %v", err)
	}

	if out["user_id"] == "" {
		t.Fatal("user_id is empty")
	}
	if out["user_id"] != user.ID {
		t.Fatalf("user_id mismatch: expected %s, got %v", user.ID, out["user_id"])
	}
}

func TestSetUsername(t *testing.T) {
	baseURL := envOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")
	user := createRegisteredUser(t, baseURL, fmt.Sprintf("setuser-%d@example.com", time.Now().UnixNano()), "testpassword123")
	username := fmt.Sprintf("testuser%d", time.Now().UnixNano())

	payload := map[string]string{
		"username": username,
	}

	resp := makeAuthenticatedRequest(t, "POST", fmt.Sprintf("%s/v1/users/me/username", baseURL), user.AccessToken, payload)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errResp)
		t.Fatalf("unexpected set username response status: %d, error: %v", resp.StatusCode, errResp)
	}

	var out map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode set username response failed: %v", err)
	}

	if out["username"] != username {
		t.Fatalf("username mismatch: expected %s, got %v", username, out["username"])
	}
}

