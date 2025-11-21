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

func envOrDefault(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func createGuest(t *testing.T, baseURL, displayName string) guestInfo {
	t.Helper()

	payload := map[string]string{
		"display_name": fmt.Sprintf("%s-%d", displayName, time.Now().UnixNano()),
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
		t.Fatalf("unexpected guest response status: %d", resp.StatusCode)
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
