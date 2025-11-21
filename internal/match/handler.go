package match

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"

	"github.com/gokatarajesh/quiz-platform/internal/auth"
	"github.com/gokatarajesh/quiz-platform/internal/match/queue"
	ws "github.com/gokatarajesh/quiz-platform/pkg/http/ws"
)

// Handler manages WebSocket connections and routes match-related messages.
type Handler struct {
	service  *Service
	hub      *ws.Hub
	authSvc  *auth.Service
	logger   zerolog.Logger
}

// NewHandler creates a match WebSocket handler.
func NewHandler(service *Service, hub *ws.Hub, authSvc *auth.Service, logger zerolog.Logger) *Handler {
	return &Handler{
		service: service,
		hub:     hub,
		authSvc: authSvc,
		logger:  logger,
	}
}

// HandleConnection processes a new WebSocket connection.
// Token should be validated before calling this (extract userID from JWT claims).
func (h *Handler) HandleConnection(conn *websocket.Conn, userID uuid.UUID, displayName string, isGuest bool) {
	wsConn := ws.NewConnection(conn, h.logger)
	h.hub.RegisterConnection(userID, wsConn)

	// Start write pump
	go wsConn.WritePump()

	// Handle incoming messages
	wsConn.ReadPump(func(msg ws.Message) error {
		return h.handleMessage(context.Background(), userID, displayName, isGuest, msg)
	})

	// Cleanup on disconnect
	h.hub.UnregisterConnection(userID)
}

// handleMessage routes incoming WebSocket messages.
func (h *Handler) handleMessage(ctx context.Context, userID uuid.UUID, displayName string, isGuest bool, msg ws.Message) error {
	switch msg.Type {
	case ws.TypeJoinQueue:
		return h.handleJoinQueue(ctx, userID, displayName, isGuest, msg.Payload)
	case ws.TypeCancelQueue:
		return h.handleCancelQueue(ctx, userID, msg.Payload)
	case ws.TypeAcceptBotFill:
		return h.handleAcceptBotFill(ctx, userID, msg.Payload)
	case ws.TypeJoinPrivate:
		return h.handleJoinPrivate(ctx, userID, displayName, isGuest, msg.Payload)
	case ws.TypeReadyState:
		return h.handleReadyState(ctx, userID, msg.Payload)
	case ws.TypeSubmitAnswer:
		return h.handleSubmitAnswer(ctx, userID, msg.Payload)
	case ws.TypeLeaveMatch:
		return h.handleLeaveMatch(ctx, userID, msg.Payload)
	case ws.TypeRequestProgress:
		return h.handleRequestProgress(ctx, userID, msg.Payload)
	default:
		return h.sendError(userID, "unknown_message_type", fmt.Sprintf("Unknown message type: %s", msg.Type))
	}
}

func (h *Handler) handleJoinQueue(ctx context.Context, userID uuid.UUID, displayName string, isGuest bool, payload json.RawMessage) error {
	var req ws.JoinQueuePayload
	if err := json.Unmarshal(payload, &req); err != nil {
		return h.sendError(userID, "invalid_payload", "Invalid join_queue payload")
	}

	// Enqueue player
	queueToken, pair, err := h.service.queueMgr.Enqueue(ctx, queue.MatchmakingRequest{
		UserID:            userID,
		DisplayName:       displayName,
		IsGuest:           isGuest,
		PreferredCategory: "general",
		BotOK:             true,
	})
	if err != nil {
		return h.sendError(userID, "enqueue_failed", err.Error())
	}

	// If match found immediately
	if pair != nil {
		match, questions, err := h.service.CreateRandomMatch(ctx, pair, 5, 15)
		if err != nil {
			return h.sendError(userID, "match_creation_failed", err.Error())
		}

		// Join match in hub
		h.hub.JoinMatch(match.ID, userID)
		if pair.Player1.UserID != userID {
			h.hub.JoinMatch(match.ID, pair.Player1.UserID)
		}
		if pair.Player2.UserID != userID {
			h.hub.JoinMatch(match.ID, pair.Player2.UserID)
		}

		// Broadcast match found
		matchPayload := ws.MatchFoundPayload{
			MatchID:              match.ID.String(),
			Mode:                 match.Mode,
			QuestionCount:        match.QuestionCount,
			PerQuestionSeconds:   match.PerQuestionSeconds,
			GlobalTimeoutSeconds: match.GlobalTimeoutSeconds,
			Players: []ws.Player{
				{UserID: pair.Player1.UserID.String(), DisplayName: pair.Player1.DisplayName},
				{UserID: pair.Player2.UserID.String(), DisplayName: pair.Player2.DisplayName},
			},
		}

		msg := ws.Message{Type: ws.TypeMatchFound}
		msg.Payload, _ = json.Marshal(matchPayload)
		h.hub.BroadcastToMatch(match.ID, msg)

		// Send questions
		h.sendQuestions(match.ID, questions)
		return nil
	}

	// Send queue update
	update := ws.QueueUpdatePayload{
		QueueToken:  queueToken.String(),
		Status:      "waiting",
		Position:    h.service.queueMgr.GetPosition(queueToken),
		WaitSeconds: 0,
	}
	msg := ws.Message{Type: ws.TypeQueueUpdate}
	msg.Payload, _ = json.Marshal(update)
	return h.hub.SendToUser(userID, msg)
}

func (h *Handler) handleCancelQueue(ctx context.Context, userID uuid.UUID, payload json.RawMessage) error {
	var req ws.CancelQueuePayload
	if err := json.Unmarshal(payload, &req); err != nil {
		return h.sendError(userID, "invalid_payload", "Invalid cancel_queue payload")
	}

	queueToken, err := uuid.Parse(req.QueueToken)
	if err != nil {
		return h.sendError(userID, "invalid_token", "Invalid queue token")
	}

	return h.service.queueMgr.Dequeue(ctx, queueToken)
}

func (h *Handler) handleAcceptBotFill(ctx context.Context, userID uuid.UUID, payload json.RawMessage) error {
	// Bot fill logic - for MVP, return error (not implemented yet)
	return h.sendError(userID, "not_implemented", "Bot fill not yet implemented")
}

func (h *Handler) handleJoinPrivate(ctx context.Context, userID uuid.UUID, displayName string, isGuest bool, payload json.RawMessage) error {
	var req ws.JoinPrivatePayload
	if err := json.Unmarshal(payload, &req); err != nil {
		return h.sendError(userID, "invalid_payload", "Invalid join_private payload")
	}

	room, err := h.service.roomMgr.JoinRoom(ctx, req.RoomCode, userID, displayName, isGuest)
	if err != nil {
		return h.sendError(userID, "join_failed", err.Error())
	}

	// Convert players
	players := make([]ws.Player, len(room.Players))
	for i, p := range room.Players {
		players[i] = ws.Player{
			UserID:      p.UserID.String(),
			DisplayName: p.DisplayName,
		}
	}

	update := ws.PrivateRoomUpdatePayload{
		MatchID:        "",
		RoomCode:       room.RoomCode,
		Players:        players,
		SlotsRemaining: room.MaxPlayers - len(room.Players),
	}
	if room.MatchID != nil {
		update.MatchID = room.MatchID.String()
	}

	msg := ws.Message{Type: ws.TypePrivateRoomUpdate}
	msg.Payload, _ = json.Marshal(update)
	return h.hub.SendToUser(userID, msg)
}

func (h *Handler) handleReadyState(ctx context.Context, userID uuid.UUID, payload json.RawMessage) error {
	// Ready state for private rooms - not implemented in MVP
	return nil
}

func (h *Handler) handleSubmitAnswer(ctx context.Context, userID uuid.UUID, payload json.RawMessage) error {
	var req ws.SubmitAnswerPayload
	if err := json.Unmarshal(payload, &req); err != nil {
		return h.sendError(userID, "invalid_payload", "Invalid submit_answer payload")
	}

	matchID, err := uuid.Parse(req.MatchID)
	if err != nil {
		return h.sendError(userID, "invalid_match_id", "Invalid match ID")
	}

	submittedAt := time.Now()
	if err := h.service.SubmitAnswer(ctx, matchID, userID, req.QuestionToken, req.Answer, submittedAt); err != nil {
		return h.sendError(userID, "submit_failed", err.Error())
	}

	// Send acknowledgment
	ack := ws.AnswerAckPayload{
		MatchID:          req.MatchID,
		QuestionOrder:    0, // TODO: extract from token
		Accepted:         true,
		ServerReceivedAt: submittedAt.Format(time.RFC3339),
	}
	msg := ws.Message{Type: ws.TypeAnswerAck}
	msg.Payload, _ = json.Marshal(ack)
	return h.hub.SendToUser(userID, msg)
}

func (h *Handler) handleLeaveMatch(ctx context.Context, userID uuid.UUID, payload json.RawMessage) error {
	var req ws.LeaveMatchPayload
	if err := json.Unmarshal(payload, &req); err != nil {
		return h.sendError(userID, "invalid_payload", "Invalid leave_match payload")
	}

	matchID, err := uuid.Parse(req.MatchID)
	if err != nil {
		return h.sendError(userID, "invalid_match_id", "Invalid match ID")
	}

	h.hub.LeaveMatch(matchID, userID)
	return nil
}

func (h *Handler) handleRequestProgress(ctx context.Context, userID uuid.UUID, payload json.RawMessage) error {
	var req ws.RequestProgressPayload
	if err := json.Unmarshal(payload, &req); err != nil {
		return h.sendError(userID, "invalid_payload", "Invalid request_progress payload")
	}

	// TODO: fetch and send progress
	return nil
}

func (h *Handler) sendQuestions(matchID uuid.UUID, questions []QuestionPackItem) {
	wsQuestions := make([]ws.QuestionPayload, len(questions))
	for i, q := range questions {
		wsQuestions[i] = ws.QuestionPayload{
			Order:      q.Order,
			ID:         q.ID,
			Prompt:     q.Prompt,
			Type:       q.Type,
			Options:    q.Options,
			Token:      q.Token,
			Difficulty: q.Difficulty,
		}
	}

	batch := ws.QuestionBatchPayload{
		MatchID:  matchID.String(),
		Batch:    wsQuestions,
		Seed:     "", // TODO: get from match
		IssuedAt: time.Now().Format(time.RFC3339),
	}

	msg := ws.Message{Type: ws.TypeQuestionBatch}
	msg.Payload, _ = json.Marshal(batch)
	h.hub.BroadcastToMatch(matchID, msg)
}

func (h *Handler) sendError(userID uuid.UUID, code, message string) error {
	errPayload := ws.ErrorPayload{
		Code:    code,
		Message: message,
	}
	msg := ws.Message{Type: ws.TypeError}
	msg.Payload, _ = json.Marshal(errPayload)
	return h.hub.SendToUser(userID, msg)
}
