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

func TestPrivateRoomMatchFlow(t *testing.T) {
	baseURL := envOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")
	baseWS := envOrDefault("INTEGRATION_WS_URL", "ws://localhost:8080/ws/matches")

	host := createRegisteredUser(t, baseURL, fmt.Sprintf("host-%d@example.com", time.Now().UnixNano()), "testpassword123")
	player := createGuest(t, baseURL, "Player")

	roomCode := createRoom(t, baseURL, host.AccessToken)

	// Connect both players to WebSocket
	hostConn := dialMatchWS(t, baseWS, host.AccessToken)
	defer hostConn.Close()

	playerConn := dialMatchWS(t, baseWS, player.AccessToken)
	defer playerConn.Close()

	// Player joins room
	joinMsg := wsmsg.Message{
		Type: wsmsg.TypeJoinPrivate,
		Payload: json.RawMessage(fmt.Sprintf(`{"room_code": "%s"}`, roomCode)),
	}
	if err := playerConn.WriteJSON(joinMsg); err != nil {
		t.Fatalf("failed to send join_private: %v", err)
	}

	// Wait for questions to be sent (match should start automatically when second player joins)
	deadline := time.Now().Add(10 * time.Second)
	var questionsReceived bool
	var matchID string

	for time.Now().Before(deadline) {
		playerConn.SetReadDeadline(time.Now().Add(2 * time.Second))
		var msg wsmsg.Message
		if err := playerConn.ReadJSON(&msg); err != nil {
			continue
		}

		if msg.Type == wsmsg.TypeQuestionBatch {
			var payload wsmsg.QuestionBatchPayload
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				t.Fatalf("decode question batch failed: %v", err)
			}
			matchID = payload.MatchID
			if len(payload.Batch) > 0 {
				questionsReceived = true
				break
			}
		}
	}

	if !questionsReceived {
		t.Fatal("timeout waiting for question batch")
	}

	if matchID == "" {
		t.Fatal("match ID is empty")
	}
}

func TestAnswerSubmission(t *testing.T) {
	baseURL := envOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")
	baseWS := envOrDefault("INTEGRATION_WS_URL", "ws://localhost:8080/ws/matches")

	host := createRegisteredUser(t, baseURL, fmt.Sprintf("host-%d@example.com", time.Now().UnixNano()), "testpassword123")
	player := createGuest(t, baseURL, "Player")

	roomCode := createRoom(t, baseURL, host.AccessToken)

	// Connect player to WebSocket
	playerConn := dialMatchWS(t, baseWS, player.AccessToken)
	defer playerConn.Close()

	// Join room
	joinMsg := wsmsg.Message{
		Type: wsmsg.TypeJoinPrivate,
		Payload: json.RawMessage(fmt.Sprintf(`{"room_code": "%s"}`, roomCode)),
	}
	if err := playerConn.WriteJSON(joinMsg); err != nil {
		t.Fatalf("failed to send join_private: %v", err)
	}

	// Wait for questions
	var questionToken string
	var matchID string
	deadline := time.Now().Add(10 * time.Second)

	for time.Now().Before(deadline) {
		playerConn.SetReadDeadline(time.Now().Add(2 * time.Second))
		var msg wsmsg.Message
		if err := playerConn.ReadJSON(&msg); err != nil {
			continue
		}

		if msg.Type == wsmsg.TypeQuestionBatch {
			var payload wsmsg.QuestionBatchPayload
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				t.Fatalf("decode question batch failed: %v", err)
			}
			matchID = payload.MatchID
			if len(payload.Batch) > 0 {
				questionToken = payload.Batch[0].Token
				break
			}
		}
	}

	if questionToken == "" {
		t.Fatal("no question token received")
	}
	if matchID == "" {
		t.Fatal("no match ID received")
	}

	// Submit answer
	submitMsg := wsmsg.Message{
		Type: wsmsg.TypeSubmitAnswer,
		Payload: json.RawMessage(fmt.Sprintf(`{
			"match_id": "%s",
			"question_token": "%s",
			"answer": "A",
			"client_latency_ms": 100
		}`, matchID, questionToken)),
	}
	if err := playerConn.WriteJSON(submitMsg); err != nil {
		t.Fatalf("failed to send submit_answer: %v", err)
	}

	// Wait for answer acknowledgment
	deadline = time.Now().Add(5 * time.Second)
	var ackReceived bool

	for time.Now().Before(deadline) {
		playerConn.SetReadDeadline(time.Now().Add(2 * time.Second))
		var msg wsmsg.Message
		if err := playerConn.ReadJSON(&msg); err != nil {
			continue
		}

		if msg.Type == wsmsg.TypeAnswerAck {
			var payload wsmsg.AnswerAckPayload
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				t.Fatalf("decode answer ack failed: %v", err)
			}
			if payload.Accepted {
				ackReceived = true
				break
			}
		}
	}

	if !ackReceived {
		t.Fatal("timeout waiting for answer acknowledgment")
	}
}

func TestMatchCompletion(t *testing.T) {
	// This test would require completing a full match, which may be complex
	// For now, we'll skip it as it requires answering all questions
	t.Skip("Match completion test requires full match flow implementation")
}

