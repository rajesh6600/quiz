//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	wsmsg "github.com/gokatarajesh/quiz-platform/pkg/http/ws"
)

func TestWebSocketAuthentication(t *testing.T) {
	baseWS := envOrDefault("INTEGRATION_WS_URL", "ws://localhost:8080/ws/matches")

	// Try to connect without token
	u, err := url.Parse(baseWS)
	if err != nil {
		t.Fatalf("invalid WS url: %v", err)
	}

	_, resp, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err == nil {
		t.Fatal("expected connection to fail without token")
	}
	if resp != nil && resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}

	// Try with invalid token
	guest := createGuest(t, envOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080"), "TestGuest")
	invalidToken := "invalid.token.here"

	u, err = url.Parse(baseWS)
	if err != nil {
		t.Fatalf("invalid WS url: %v", err)
	}
	q := u.Query()
	q.Set("token", invalidToken)
	u.RawQuery = q.Encode()

	_, resp, err = websocket.DefaultDialer.Dial(u.String(), nil)
	if err == nil {
		t.Fatal("expected connection to fail with invalid token")
	}
	if resp != nil && resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}

	// Connect with valid token
	conn := dialMatchWS(t, baseWS, guest.AccessToken)
	defer conn.Close()

	// Connection should succeed
	if conn == nil {
		t.Fatal("connection should succeed with valid token")
	}
}

func TestInvalidMessageType(t *testing.T) {
	baseURL := envOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")
	baseWS := envOrDefault("INTEGRATION_WS_URL", "ws://localhost:8080/ws/matches")

	guest := createGuest(t, baseURL, "TestGuest")
	conn := dialMatchWS(t, baseWS, guest.AccessToken)
	defer conn.Close()

	// Send message with unknown type
	msg := wsmsg.Message{
		Type:    "unknown_message_type",
		Payload: json.RawMessage(`{}`),
	}
	if err := conn.WriteJSON(msg); err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

	// Wait for error response
	deadline := time.Now().Add(5 * time.Second)
	var errorReceived bool

	for time.Now().Before(deadline) {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		var response wsmsg.Message
		if err := conn.ReadJSON(&response); err != nil {
			continue
		}

		if response.Type == wsmsg.TypeError {
			var payload wsmsg.ErrorPayload
			if err := json.Unmarshal(response.Payload, &payload); err != nil {
				t.Fatalf("decode error payload failed: %v", err)
			}
			if payload.Code == "unknown_message_type" {
				errorReceived = true
				break
			}
		}
	}

	if !errorReceived {
		t.Fatal("timeout waiting for error response")
	}
}

func TestInvalidPayload(t *testing.T) {
	baseURL := envOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")
	baseWS := envOrDefault("INTEGRATION_WS_URL", "ws://localhost:8080/ws/matches")

	guest := createGuest(t, baseURL, "TestGuest")
	conn := dialMatchWS(t, baseWS, guest.AccessToken)
	defer conn.Close()

	// Send join_queue with invalid payload (missing required fields)
	msg := wsmsg.Message{
		Type:    wsmsg.TypeJoinQueue,
		Payload: json.RawMessage(`{"invalid": "payload"}`),
	}
	if err := conn.WriteJSON(msg); err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

	// Wait for error response
	deadline := time.Now().Add(5 * time.Second)
	var errorReceived bool

	for time.Now().Before(deadline) {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		var response wsmsg.Message
		if err := conn.ReadJSON(&response); err != nil {
			continue
		}

		if response.Type == wsmsg.TypeError {
			var payload wsmsg.ErrorPayload
			if err := json.Unmarshal(response.Payload, &payload); err != nil {
				t.Fatalf("decode error payload failed: %v", err)
			}
			if payload.Code == "invalid_payload" {
				errorReceived = true
				break
			}
		}
	}

	if !errorReceived {
		t.Fatal("timeout waiting for error response")
	}
}

func TestQueueFlow(t *testing.T) {
	baseURL := envOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")
	baseWS := envOrDefault("INTEGRATION_WS_URL", "ws://localhost:8080/ws/matches")

	playerA := createGuest(t, baseURL, "PlayerA")
	playerB := createGuest(t, baseURL, "PlayerB")

	connA := dialMatchWS(t, baseWS, playerA.AccessToken)
	defer connA.Close()
	connB := dialMatchWS(t, baseWS, playerB.AccessToken)
	defer connB.Close()

	// Both players join queue
	joinMsg := wsmsg.Message{
		Type:    wsmsg.TypeJoinQueue,
		Payload: json.RawMessage(`{}`),
	}

	if err := connA.WriteJSON(joinMsg); err != nil {
		t.Fatalf("failed to send join_queue from A: %v", err)
	}
	if err := connB.WriteJSON(joinMsg); err != nil {
		t.Fatalf("failed to send join_queue from B: %v", err)
	}

	// Wait for match_found
	matchA := waitForMatchFound(t, connA, 15*time.Second)
	matchB := waitForMatchFound(t, connB, 15*time.Second)

	if matchA.MatchID != matchB.MatchID {
		t.Fatalf("players joined different matches: %s vs %s", matchA.MatchID, matchB.MatchID)
	}

	if len(matchA.Players) != 2 || len(matchB.Players) != 2 {
		t.Fatalf("expected 2 players in match payload")
	}
}

