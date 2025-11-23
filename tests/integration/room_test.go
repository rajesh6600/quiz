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

func TestCreateRoom(t *testing.T) {
	baseURL := envOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")
	user := createRegisteredUser(t, baseURL, fmt.Sprintf("room-%d@example.com", time.Now().UnixNano()), "testpassword123")

	roomCode := createRoom(t, baseURL, user.AccessToken)

	if len(roomCode) != 6 {
		t.Fatalf("room code should be 6 characters, got: %s", roomCode)
	}

	// Verify room code is numeric
	for _, char := range roomCode {
		if char < '0' || char > '9' {
			t.Fatalf("room code should be numeric, got: %s", roomCode)
		}
	}
}

func TestGetRoom(t *testing.T) {
	baseURL := envOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")
	user := createRegisteredUser(t, baseURL, fmt.Sprintf("getroom-%d@example.com", time.Now().UnixNano()), "testpassword123")

	roomCode := createRoom(t, baseURL, user.AccessToken)
	room := getRoom(t, baseURL, roomCode)

	if room["room_code"] != roomCode {
		t.Fatalf("room code mismatch: expected %s, got %v", roomCode, room["room_code"])
	}
	if room["match_name"] != "Test Room" {
		t.Fatalf("match name mismatch: expected 'Test Room', got %v", room["match_name"])
	}
	if room["max_players"] != float64(2) {
		t.Fatalf("max players mismatch: expected 2, got %v", room["max_players"])
	}
	if room["question_count"] != float64(5) {
		t.Fatalf("question count mismatch: expected 5, got %v", room["question_count"])
	}
}

func TestRoomNotFound(t *testing.T) {
	baseURL := envOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")
	invalidCode := "999999"

	resp, err := http.Get(fmt.Sprintf("%s/v1/rooms/%s", baseURL, invalidCode))
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

func TestJoinRoom(t *testing.T) {
	baseURL := envOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")
	baseWS := envOrDefault("INTEGRATION_WS_URL", "ws://localhost:8080/ws/matches")

	host := createRegisteredUser(t, baseURL, fmt.Sprintf("host-%d@example.com", time.Now().UnixNano()), "testpassword123")
	player := createGuest(t, baseURL, "Player")

	roomCode := createRoom(t, baseURL, host.AccessToken)

	// Connect player to WebSocket
	conn := dialMatchWS(t, baseWS, player.AccessToken)
	defer conn.Close()

	// Send join_private message
	joinMsg := map[string]interface{}{
		"type": "join_private",
		"payload": map[string]string{
			"room_code": roomCode,
		},
	}

	if err := conn.WriteJSON(joinMsg); err != nil {
		t.Fatalf("failed to send join_private: %v", err)
	}

	// Wait for private_room_update message
	deadline := time.Now().Add(5 * time.Second)
	var receivedUpdate bool
	for time.Now().Before(deadline) {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		var msg map[string]interface{}
		if err := conn.ReadJSON(&msg); err != nil {
			continue
		}

		if msg["type"] == "private_room_update" {
			receivedUpdate = true
			break
		}
	}

	if !receivedUpdate {
		t.Fatal("timeout waiting for private_room_update")
	}
}

