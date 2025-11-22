package leaderboard

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"

	sqlcgen "github.com/gokatarajesh/quiz-platform/internal/db/sqlc"
	ws "github.com/gokatarajesh/quiz-platform/pkg/http/ws"
)

// HTTPHandler exposes REST endpoints for leaderboard queries.
type HTTPHandler struct {
	svc     *Service
	queries *sqlcgen.Queries
	logger  zerolog.Logger
}

// NewHTTPHandler constructs a leaderboard HTTP handler.
func NewHTTPHandler(svc *Service, queries *sqlcgen.Queries, logger zerolog.Logger) *HTTPHandler {
	return &HTTPHandler{
		svc:     svc,
		queries: queries,
		logger:  logger.With().Str("component", "leaderboard_http").Logger(),
	}
}

// HandleGet responds with the current leaderboard for a given window or private room.
// Routes: GET /v1/leaderboards/{window}?limit=10
//         GET /v1/leaderboards/private/{room_code}?limit=10
func (h *HTTPHandler) HandleGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/v1/leaderboards/")
	path = strings.TrimSuffix(path, "/")
	
	// Check if it's a private room request
	if strings.HasPrefix(path, "private/") {
		h.HandleGetPrivateRoom(w, r)
		return
	}

	// Otherwise, treat as window-based leaderboard
	window := path
	if window == "" || !isValidWindow(window) {
		http.Error(w, "unknown leaderboard window", http.StatusNotFound)
		return
	}

	limit := 10
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	ctx := r.Context()
	var (
		top    []ws.LeaderboardEntry
		source = "redis"
	)

	if h.svc != nil {
		if entries, err := h.svc.Top(ctx, window, limit); err == nil {
			top = toWSEntries(entries)
		} else {
			h.logger.Warn().Err(err).Str("window", window).Msg("redis leaderboard fetch failed")
		}
	}

	if len(top) == 0 {
		source = "snapshot"
		top = h.snapshotFallback(ctx, window, limit)
	}

	resp := map[string]interface{}{
		"window":      window,
		"top":         top,
		"source":      source,
		"retrievedAt": time.Now().UTC().Format(time.RFC3339),
	}

	writeJSON(w, resp)
}

func (h *HTTPHandler) snapshotFallback(ctx context.Context, window string, limit int) []ws.LeaderboardEntry {
	if h.queries == nil {
		return nil
	}
	rows, err := h.queries.ListRecentSnapshots(ctx, sqlcgen.ListRecentSnapshotsParams{
		TimeWindow: window,
		Limit:      1,
	})
	if err != nil || len(rows) == 0 {
		if err != nil {
			h.logger.Warn().Err(err).Str("window", window).Msg("snapshot fetch failed")
		}
		return nil
	}

	var entries []ws.LeaderboardEntry
	if err := json.Unmarshal(rows[0].Entries, &entries); err != nil {
		h.logger.Warn().Err(err).Msg("snapshot payload decode failed")
		return nil
	}
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries
}

func isValidWindow(window string) bool {
	switch window {
	case WindowDaily, WindowWeekly, WindowMonthly, WindowAllTime:
		return true
	default:
		return false
	}
}

// HandleGetPrivateRoom responds with the leaderboard for a specific private room.
// Route: GET /v1/leaderboards/private/{room_code}?limit=10
func (h *HTTPHandler) HandleGetPrivateRoom(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract room code from path: /v1/leaderboards/private/{room_code}
	path := strings.TrimPrefix(r.URL.Path, "/v1/leaderboards/private/")
	roomCode := strings.TrimSuffix(path, "/")
	if roomCode == "" {
		http.Error(w, "room code required", http.StatusBadRequest)
		return
	}

	limit := 10
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	ctx := r.Context()
	var top []ws.LeaderboardEntry

	if h.svc != nil {
		if entries, err := h.svc.GetPrivateRoomLeaderboard(ctx, roomCode, limit); err == nil {
			top = toWSEntries(entries)
		} else {
			h.logger.Warn().Err(err).Str("room_code", roomCode).Msg("private room leaderboard fetch failed")
			http.Error(w, "failed to fetch leaderboard", http.StatusInternalServerError)
			return
		}
	}

	resp := map[string]interface{}{
		"room_code":   roomCode,
		"top":         top,
		"retrievedAt": time.Now().UTC().Format(time.RFC3339),
	}

	writeJSON(w, resp)
}

func writeJSON(w http.ResponseWriter, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}
