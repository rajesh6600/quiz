//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	wsmsg "github.com/gokatarajesh/quiz-platform/pkg/http/ws"
)

func TestWebSocketRandomMatch(t *testing.T) {
	baseHTTP := envOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")
	baseWS := envOrDefault("INTEGRATION_WS_URL", "ws://localhost:8080/ws/matches")

	playerA := createGuest(t, baseHTTP, "WSA")
	playerB := createGuest(t, baseHTTP, "WSB")

	connA := dialMatchWS(t, baseWS, playerA.AccessToken)
	defer connA.Close()
	connB := dialMatchWS(t, baseWS, playerB.AccessToken)
	defer connB.Close()

	sendJoinQueue(t, connA)
	sendJoinQueue(t, connB)

	matchA := waitForMatchFound(t, connA, 15*time.Second)
	matchB := waitForMatchFound(t, connB, 15*time.Second)

	if matchA.MatchID != matchB.MatchID {
		t.Fatalf("players joined different matches: %s vs %s", matchA.MatchID, matchB.MatchID)
	}

	if len(matchA.Players) != 2 || len(matchB.Players) != 2 {
		t.Fatalf("expected 2 players in match payload")
	}
}

func dialMatchWS(t *testing.T, wsBase, token string) *websocket.Conn {
	t.Helper()

	u, err := url.Parse(wsBase)
	if err != nil {
		t.Fatalf("invalid WS url: %v", err)
	}
	q := u.Query()
	q.Set("token", token)
	u.RawQuery = q.Encode()

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("websocket dial failed: %v", err)
	}
	return conn
}

func sendJoinQueue(t *testing.T, conn *websocket.Conn) {
	t.Helper()

	msg := wsmsg.Message{
		Type:    wsmsg.TypeJoinQueue,
		Payload: json.RawMessage(`{}`),
	}
	conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	if err := conn.WriteJSON(msg); err != nil {
		t.Fatalf("failed to send join_queue: %v", err)
	}
}

func waitForMatchFound(t *testing.T, conn *websocket.Conn, timeout time.Duration) wsmsg.MatchFoundPayload {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		var msg wsmsg.Message
		if err := conn.ReadJSON(&msg); err != nil {
			t.Fatalf("read ws message failed: %v", err)
		}

		if msg.Type == wsmsg.TypeMatchFound {
			var payload wsmsg.MatchFoundPayload
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				t.Fatalf("decode match_found payload: %v", err)
			}
			return payload
		}
	}
	t.Fatalf("timeout waiting for match_found")
	return wsmsg.MatchFoundPayload{}
}
