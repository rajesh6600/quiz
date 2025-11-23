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
	httperrors "github.com/gokatarajesh/quiz-platform/pkg/http/errors"
)

// Handler manages WebSocket connections and routes match-related messages.
type Handler struct {
	service *Service
	hub     *ws.Hub
	authSvc *auth.Service
	logger  zerolog.Logger
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
func (h *Handler) HandleConnection(conn *websocket.Conn, userID uuid.UUID, username string, isGuest bool) {
	wsConn := ws.NewConnection(conn, h.logger)
	h.hub.RegisterConnection(userID, wsConn)

	// Start write pump
	go wsConn.WritePump()

	// Handle incoming messages
	wsConn.ReadPump(func(msg ws.Message) error {
		return h.handleMessage(context.Background(), userID, username, isGuest, msg)
	})

	// Cleanup on disconnect
	h.hub.UnregisterConnection(userID)
}

// handleMessage routes incoming WebSocket messages.
func (h *Handler) handleMessage(ctx context.Context, userID uuid.UUID, username string, isGuest bool, msg ws.Message) error {
	switch msg.Type {
	case ws.TypeJoinQueue:
		return h.handleJoinQueue(ctx, userID, username, isGuest, msg.Payload)
	case ws.TypeCancelQueue:
		return h.handleCancelQueue(ctx, userID, msg.Payload)
	case ws.TypeAcceptBotFill:
		return h.handleAcceptBotFill(ctx, userID, msg.Payload)
	case ws.TypeJoinPrivate:
		return h.handleJoinPrivate(ctx, userID, username, isGuest, msg.Payload)
	case ws.TypeReadyState:
		return h.handleReadyState(ctx, userID, msg.Payload)
	case ws.TypeSubmitAnswer:
		return h.handleSubmitAnswer(ctx, userID, msg.Payload)
	case ws.TypeLeaveMatch:
		return h.handleLeaveMatch(ctx, userID, msg.Payload)
	case ws.TypeRequestProgress:
		return h.handleRequestProgress(ctx, userID, msg.Payload)
	default:
		return h.sendError(userID, httperrors.ErrCodeUnknownMessageType, fmt.Sprintf("Unknown message type: %s", msg.Type))
	}
}

func (h *Handler) handleJoinQueue(ctx context.Context, userID uuid.UUID, username string, isGuest bool, payload json.RawMessage) error {
	var req ws.JoinQueuePayload
	if err := json.Unmarshal(payload, &req); err != nil {
		return h.sendError(userID, httperrors.ErrCodeInvalidPayload, "Invalid join_queue payload")
	}

	// Get category from client, default to "general"
	category := req.Category
	if category == "" {
		category = "general"
	}

	// Enqueue player
		queueToken, pair, err := h.service.queueMgr.Enqueue(ctx, queue.MatchmakingRequest{
		UserID:            userID,
		Username:          username,
		IsGuest:           isGuest,
		PreferredCategory: category,
		BotOK:             true,
	})
	if err != nil {
		return h.sendError(userID, httperrors.ErrCodeEnqueueFailed, err.Error())
	}

	// If match found immediately
	if pair != nil {
		questionCount := req.QuestionCount
		if questionCount == 0 {
			questionCount = 10 // default
		}
		// Validate question count (must be 5, 10, or 15)
		if questionCount != 5 && questionCount != 10 && questionCount != 15 {
			questionCount = 10 // fallback to default
		}
		match, questions, err := h.service.CreateRandomMatch(ctx, pair, questionCount, 15, category)
		if err != nil {
			return h.sendError(userID, httperrors.ErrCodeMatchCreationFailed, err.Error())
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
				{UserID: pair.Player1.UserID.String(), Username: pair.Player1.Username},
				{UserID: pair.Player2.UserID.String(), Username: pair.Player2.Username},
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
		return h.sendError(userID, httperrors.ErrCodeInvalidPayload, "Invalid cancel_queue payload")
	}

	queueToken, err := uuid.Parse(req.QueueToken)
	if err != nil {
		return h.sendError(userID, httperrors.ErrCodeInvalidQueueToken, "Invalid queue token")
	}

	return h.service.queueMgr.Dequeue(ctx, queueToken)
}

func (h *Handler) handleAcceptBotFill(ctx context.Context, userID uuid.UUID, payload json.RawMessage) error {
	// TODO: Implement bot fill feature
	// When a player has waited in queue for a configured duration (default 10s),
	// offer them a bot opponent. If accepted, create a match with a bot player.
	// Bot should have configurable difficulty and answer questions with realistic timing.
	return h.sendError(userID, httperrors.ErrCodeFeatureNotAvailable, "Bot fill feature is not yet available")
}

func (h *Handler) handleJoinPrivate(ctx context.Context, userID uuid.UUID, username string, isGuest bool, payload json.RawMessage) error {
	var req ws.JoinPrivatePayload
	if err := json.Unmarshal(payload, &req); err != nil {
		return h.sendError(userID, httperrors.ErrCodeInvalidPayload, "Invalid join_private payload")
	}

	room, err := h.service.roomMgr.JoinRoom(ctx, req.RoomCode, userID, username, isGuest)
	if err != nil {
		return h.sendError(userID, httperrors.ErrCodeJoinFailed, err.Error())
	}

	// Check if this is the first non-host player joining (trigger question generation)
	wasWaiting := len(room.Players) == 2
	isNonHost := userID != room.HostID

	if wasWaiting && isNonHost && room.MatchID == nil {
		// First non-host player joined - generate questions
		players := make([]RoomPlayer, len(room.Players))
		for i, p := range room.Players {
			players[i] = RoomPlayer{
				UserID:   p.UserID,
				Username: p.Username,
				IsGuest:  p.IsGuest,
				IsHost:   p.IsHost,
			}
		}

		match, questions, err := h.service.CreatePrivateMatch(ctx, req.RoomCode, players, room.QuestionCount, room.PerQuestionSeconds, room.Category)
		if err != nil {
			return h.sendError(userID, httperrors.ErrCodeMatchCreationFailed, err.Error())
		}

		// Update room with match ID
		_, err = h.service.roomMgr.StartRoom(ctx, req.RoomCode, match.ID, room.StartCountdown)
		if err != nil {
			return h.sendError(userID, httperrors.ErrCodeRoomStartFailed, err.Error())
		}

		// Join all players to match in hub
		for _, p := range room.Players {
			h.hub.JoinMatch(match.ID, p.UserID)
		}

		// Store questions in room state (they'll be sent when match actually starts)
		// For now, we'll send them immediately after countdown
		// TODO: Implement countdown logic and send questions after countdown
		h.sendQuestions(match.ID, questions)
	}

	// Convert players
	wsPlayers := make([]ws.Player, len(room.Players))
	for i, p := range room.Players {
		wsPlayers[i] = ws.Player{
			UserID:   p.UserID.String(),
			Username: p.Username,
		}
	}

	update := ws.PrivateRoomUpdatePayload{
		MatchID:        "",
		RoomCode:       room.RoomCode,
		Players:        wsPlayers,
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
	// TODO: Implement ready state feature for private rooms
	// Both players should be able to indicate they're ready before the match starts.
	// Once both players are ready, start a countdown and then begin the match.
	// This allows players to prepare before questions are sent.
	return h.sendError(userID, httperrors.ErrCodeFeatureNotAvailable, "Ready state feature is not yet available")
}

func (h *Handler) handleSubmitAnswer(ctx context.Context, userID uuid.UUID, payload json.RawMessage) error {
	var req ws.SubmitAnswerPayload
	if err := json.Unmarshal(payload, &req); err != nil {
		return h.sendError(userID, httperrors.ErrCodeInvalidPayload, "Invalid submit_answer payload")
	}

	matchID, err := uuid.Parse(req.MatchID)
	if err != nil {
		return h.sendError(userID, httperrors.ErrCodeInvalidMatchID, "Invalid match ID")
	}

	submittedAt := time.Now()
	if err := h.service.SubmitAnswer(ctx, matchID, userID, req.QuestionToken, req.Answer, submittedAt); err != nil {
		return h.sendError(userID, httperrors.ErrCodeSubmitFailed, err.Error())
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
		return h.sendError(userID, httperrors.ErrCodeInvalidPayload, "Invalid leave_match payload")
	}

	matchID, err := uuid.Parse(req.MatchID)
	if err != nil {
		return h.sendError(userID, httperrors.ErrCodeInvalidMatchID, "Invalid match ID")
	}

	h.hub.LeaveMatch(matchID, userID)
	return nil
}

// FinalizeAndBroadcastMatch finalizes a match and broadcasts results to all players.
// This should be called when a match ends (all questions answered or timeout).
func (h *Handler) FinalizeAndBroadcastMatch(ctx context.Context, matchID uuid.UUID) error {
	payload, err := h.service.FinalizeMatch(ctx, matchID)
	if err != nil {
		h.logger.Error().Err(err).Str("match_id", matchID.String()).Msg("failed to finalize match")
		return err
	}

	// Broadcast match complete event to all players in the match
	msg := ws.Message{Type: ws.TypeMatchComplete}
	msg.Payload, _ = json.Marshal(payload)
	h.hub.BroadcastToMatch(matchID, msg)

	h.logger.Info().
		Str("match_id", matchID.String()).
		Int("player_count", len(payload.Results)).
		Msg("match finalized and results broadcasted")

	return nil
}

func (h *Handler) handleRequestProgress(ctx context.Context, userID uuid.UUID, payload json.RawMessage) error {
	var req ws.RequestProgressPayload
	if err := json.Unmarshal(payload, &req); err != nil {
		return h.sendError(userID, httperrors.ErrCodeInvalidPayload, "Invalid request_progress payload")
	}

	// TODO: fetch and send progress
	return nil
}

func (h *Handler) sendQuestions(matchID uuid.UUID, questions []QuestionPackItem) {
	wsQuestions := make([]ws.QuestionPayload, len(questions))
	for i, q := range questions {
		wsQuestions[i] = ws.QuestionPayload{
			Order:   q.Order,
			ID:      q.ID,
			Prompt:  q.Prompt,
			Options: q.Options,
			Token:   q.Token,
			// Type, Difficulty, Category removed - not needed by client
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
