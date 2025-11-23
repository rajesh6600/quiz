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

func TestUnauthorizedAccess(t *testing.T) {
	baseURL := envOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")

	// Try to access protected endpoint without token
	resp := makeAuthenticatedRequest(t, "GET", fmt.Sprintf("%s/v1/users/me", baseURL), "", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		var errResp map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errResp)
		t.Fatalf("expected 401, got %d, error: %v", resp.StatusCode, errResp)
	}

	var errResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response failed: %v", err)
	}

	if errResp["error"] == nil {
		t.Fatal("error field is missing")
	}
}

func TestForbiddenAccess(t *testing.T) {
	baseURL := envOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")

	// Guest tries to create room (should be forbidden)
	guest := createGuest(t, baseURL, "TestGuest")

	payload := map[string]interface{}{
		"match_name":           "Test Room",
		"max_players":           2,
		"question_count":       5,
		"per_question_seconds": 15,
	}

	resp := makeAuthenticatedRequest(t, "POST", fmt.Sprintf("%s/v1/rooms", baseURL), guest.AccessToken, payload)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		var errResp map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errResp)
		t.Fatalf("expected 403, got %d, error: %v", resp.StatusCode, errResp)
	}

	var errResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response failed: %v", err)
	}

	// Accept both the specific error code and generic forbidden
	if errResp["error"] != "guests_cannot_create_rooms" && errResp["error"] != "forbidden" {
		t.Fatalf("expected error code 'guests_cannot_create_rooms' or 'forbidden', got %v", errResp["error"])
	}
}

func TestValidationErrors(t *testing.T) {
	baseURL := envOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")
	user := createRegisteredUser(t, baseURL, fmt.Sprintf("validate-%d@example.com", time.Now().UnixNano()), "testpassword123")

	// Try to create room with invalid data
	testCases := []struct {
		name    string
		payload map[string]interface{}
		status  int
	}{
		{
			name: "missing match_name",
			payload: map[string]interface{}{
				"max_players":           2,
				"question_count":       5,
				"per_question_seconds": 15,
			},
			status: http.StatusBadRequest,
		},
		{
			name: "invalid max_players",
			payload: map[string]interface{}{
				"match_name":           "Test",
				"max_players":           3, // must be 2
				"question_count":       5,
				"per_question_seconds": 15,
			},
			status: http.StatusBadRequest,
		},
		{
			name: "invalid question_count",
			payload: map[string]interface{}{
				"match_name":           "Test",
				"max_players":           2,
				"question_count":       20, // must be 5, 10, or 15
				"per_question_seconds": 15,
			},
			status: http.StatusBadRequest,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resp := makeAuthenticatedRequest(t, "POST", fmt.Sprintf("%s/v1/rooms", baseURL), user.AccessToken, tc.payload)
			defer resp.Body.Close()

			if resp.StatusCode != tc.status {
				var errResp map[string]interface{}
				json.NewDecoder(resp.Body).Decode(&errResp)
				t.Fatalf("expected %d, got %d, error: %v", tc.status, resp.StatusCode, errResp)
			}

			var errResp map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
				t.Fatalf("decode error response failed: %v", err)
			}

			if errResp["error"] == nil {
				t.Fatal("error field is missing")
			}
		})
	}
}

func TestNotFoundErrors(t *testing.T) {
	baseURL := envOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")

	// Try to get non-existent room
	resp, err := http.Get(fmt.Sprintf("%s/v1/rooms/000000", baseURL))
	if err != nil {
		t.Fatalf("get room request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		var errResp map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errResp)
		t.Fatalf("expected 404, got %d, error: %v", resp.StatusCode, errResp)
	}

	var errResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response failed: %v", err)
	}

	if errResp["error"] != "room_not_found" {
		t.Fatalf("expected error code 'room_not_found', got %v", errResp["error"])
	}
}

func TestInvalidJSONPayload(t *testing.T) {
	baseURL := envOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")

	// Send invalid JSON
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/v1/auth/register", baseURL), nil)
	if err != nil {
		t.Fatalf("create request failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Body = http.NoBody

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		var errResp map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errResp)
		t.Fatalf("expected 400, got %d, error: %v", resp.StatusCode, errResp)
	}

	var errResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response failed: %v", err)
	}

	if errResp["error"] == nil {
		t.Fatal("error field is missing")
	}
}

