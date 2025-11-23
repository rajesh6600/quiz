package match

import (
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog"

	"github.com/gokatarajesh/quiz-platform/internal/auth/jwt"
)

// HTTPHandlers provides REST endpoints for match operations.
type HTTPHandlers struct {
	service *Service
	logger  zerolog.Logger
}

// NewHTTPHandlers creates HTTP handlers for match endpoints.
func NewHTTPHandlers(service *Service, logger zerolog.Logger) *HTTPHandlers {
	return &HTTPHandlers{
		service: service,
		logger:  logger.With().Str("component", "match_http").Logger(),
	}
}

// CreateRoom handles POST /v1/rooms
func (h *HTTPHandlers) CreateRoom(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract JWT claims from context (set by auth middleware)
	claims, ok := r.Context().Value("claims").(*jwt.Claims)
	if !ok || claims == nil {
		h.respondError(w, http.StatusUnauthorized, "authentication_required", "Authentication required")
		return
	}

	// Validate user is registered (not guest)
	if claims.IsGuest {
		h.respondError(w, http.StatusForbidden, "guests_cannot_create_rooms", "Guests cannot create private rooms")
		return
	}

	// Decode request body
	var req CreateRoomRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON payload")
		return
	}

	// Validate request fields
	if err := h.validateCreateRoomRequest(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}

	// Build PrivateRoomRequest from HTTP request and JWT claims
	privateRoomReq := PrivateRoomRequest{
		HostID:             claims.UserID,
		Username:           claims.Username,
		IsGuest:            false, // Already validated above
		MatchName:          req.MatchName,
		MaxPlayers:         req.MaxPlayers,
		QuestionCount:      req.QuestionCount,
		PerQuestionSeconds: req.PerQuestionSeconds,
		Category:           req.Category,
	}

	// Create room
	roomCode, room, err := h.service.CreateRoom(r.Context(), privateRoomReq)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", claims.UserID.String()).Msg("failed to create room")
		h.respondError(w, http.StatusInternalServerError, "room_creation_failed", err.Error())
		return
	}

	// Convert room to response format
	response := h.roomToResponse(roomCode, room)

	h.respondJSON(w, http.StatusCreated, response)
}

// validateCreateRoomRequest validates the CreateRoomRequest payload.
func (h *HTTPHandlers) validateCreateRoomRequest(req *CreateRoomRequest) error {
	if req.MatchName == "" {
		return &ValidationError{Field: "match_name", Message: "match_name is required"}
	}

	if req.MaxPlayers != 2 {
		return &ValidationError{Field: "max_players", Message: "max_players must be 2 for 1v1 matches"}
	}

	if req.QuestionCount != 5 && req.QuestionCount != 10 && req.QuestionCount != 15 {
		return &ValidationError{Field: "question_count", Message: "question_count must be 5, 10, or 15"}
	}

	if req.PerQuestionSeconds <= 0 {
		return &ValidationError{Field: "per_question_seconds", Message: "per_question_seconds must be a positive integer"}
	}

	return nil
}

// roomToResponse converts a PrivateRoom to HTTP response format.
func (h *HTTPHandlers) roomToResponse(roomCode string, room *PrivateRoom) map[string]interface{} {
	players := make([]map[string]interface{}, len(room.Players))
	for i, p := range room.Players {
		players[i] = map[string]interface{}{
			"user_id":   p.UserID.String(),
			"username":  p.Username,
			"is_host":   p.IsHost,
			"joined_at": p.JoinedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
	}

	return map[string]interface{}{
		"room_code":           roomCode,
		"match_name":          room.MatchName,
		"max_players":         room.MaxPlayers,
		"question_count":      room.QuestionCount,
		"per_question_seconds": room.PerQuestionSeconds,
		"category":            room.Category,
		"status":              room.Status,
		"players":             players,
		"slots_remaining":     room.MaxPlayers - len(room.Players),
		"created_at":          room.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

// ValidationError represents a validation error.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

func (h *HTTPHandlers) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (h *HTTPHandlers) respondError(w http.ResponseWriter, status int, code, message string) {
	h.respondJSON(w, status, map[string]interface{}{
		"error":   code,
		"message": message,
	})
}

