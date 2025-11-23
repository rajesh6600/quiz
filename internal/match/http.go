package match

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/rs/zerolog"

	"github.com/gokatarajesh/quiz-platform/internal/auth/jwt"
	httperrors "github.com/gokatarajesh/quiz-platform/pkg/http/errors"
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
		httperrors.RespondError(w, http.StatusMethodNotAllowed, httperrors.ErrCodeInvalidRequest, "Method not allowed")
		return
	}

	// Extract JWT claims from context (set by auth middleware)
	claims, ok := r.Context().Value("claims").(*jwt.Claims)
	if !ok || claims == nil {
		httperrors.RespondUnauthorized(w, httperrors.ErrCodeAuthenticationRequired, "Authentication required")
		return
	}

	// Validate user is registered (not guest)
	if claims.IsGuest {
		httperrors.RespondForbidden(w, httperrors.ErrCodeGuestsCannotCreateRooms, "Guests cannot create private rooms")
		return
	}

	// Decode request body
	var req CreateRoomRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httperrors.RespondBadRequest(w, httperrors.ErrCodeInvalidRequest, "Invalid JSON payload")
		return
	}

	// Validate request fields
	if err := h.validateCreateRoomRequest(&req); err != nil {
		validationErr, ok := err.(*ValidationError)
		if ok {
			httperrors.RespondValidationError(w, httperrors.ErrCodeValidationFailed, validationErr.Message, validationErr.Field)
		} else {
			httperrors.RespondBadRequest(w, httperrors.ErrCodeValidationFailed, err.Error())
		}
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
		httperrors.RespondInternalError(w, err.Error())
		return
	}

	// Convert room to response format
	response := h.roomToResponse(roomCode, room)

	h.respondJSON(w, http.StatusCreated, response)
}

// GetRoom handles GET /v1/rooms/{room_code}
func (h *HTTPHandlers) GetRoom(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httperrors.RespondError(w, http.StatusMethodNotAllowed, httperrors.ErrCodeInvalidRequest, "Method not allowed")
		return
	}

	// Extract room code from path: /v1/rooms/{room_code}
	path := r.URL.Path
	roomCode := strings.TrimPrefix(path, "/v1/rooms/")
	roomCode = strings.TrimSuffix(roomCode, "/")

	// Validate room code format (6 digits)
	if len(roomCode) != 6 {
		httperrors.RespondValidationError(w, httperrors.ErrCodeInvalidRoomCode, "Room code must be 6 digits", "room_code")
		return
	}

	// Check if all characters are digits
	for _, char := range roomCode {
		if char < '0' || char > '9' {
			httperrors.RespondValidationError(w, httperrors.ErrCodeInvalidRoomCode, "Room code must be numeric", "room_code")
			return
		}
	}

	// Get room
	room, err := h.service.GetRoom(r.Context(), roomCode)
	if err != nil {
		if err.Error() == "room not found" {
			httperrors.RespondNotFound(w, httperrors.ErrCodeRoomNotFound, "Room not found")
			return
		}
		h.logger.Error().Err(err).Str("room_code", roomCode).Msg("failed to get room")
		httperrors.RespondInternalError(w, err.Error())
		return
	}

	// Convert room to response format
	response := h.roomToResponse(roomCode, room)

	h.respondJSON(w, http.StatusOK, response)
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

