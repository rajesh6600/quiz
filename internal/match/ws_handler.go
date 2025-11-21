package match

import (
	"net/http"

	"github.com/gokatarajesh/quiz-platform/internal/server"
)

// HandleWebSocket upgrades HTTP connection to WebSocket and authenticates user.
func (h *Handler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Extract and validate token from query parameter
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "Missing token", http.StatusUnauthorized)
		return
	}

	// Validate token and extract claims
	claims, err := h.authSvc.ValidateToken(token)
	if err != nil {
		h.logger.Warn().Err(err).Msg("WebSocket token validation failed")
		http.Error(w, "Invalid token", http.StatusUnauthorized)
		return
	}

	// Upgrade to WebSocket
	conn, err := server.WSUpgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error().Err(err).Msg("WebSocket upgrade failed")
		return
	}

	// Extract user info from claims
	userID := claims.UserID
	displayName := claims.DisplayName
	isGuest := claims.IsGuest

	// Handle connection
	h.HandleConnection(conn, userID, displayName, isGuest)
}
