package auth

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/gokatarajesh/quiz-platform/internal/auth/jwt"
)

// HTTPHandlers provides REST endpoints for authentication.
type HTTPHandlers struct {
	authSvc  *Service
	oauthSvc *OAuthService
	logger   zerolog.Logger
}

// NewHTTPHandlers creates HTTP handlers for auth endpoints.
func NewHTTPHandlers(authSvc *Service, oauthSvc *OAuthService, logger zerolog.Logger) *HTTPHandlers {
	return &HTTPHandlers{
		authSvc:  authSvc,
		oauthSvc: oauthSvc,
		logger:   logger,
	}
}

// Register handles POST /v1/auth/register
func (h *HTTPHandlers) Register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON payload")
		return
	}

	user, tokens, err := h.authSvc.Register(r.Context(), req)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "registration_failed", err.Error())
		return
	}

	h.respondJSON(w, http.StatusCreated, map[string]interface{}{
		"user_id":       user.ID.String(),
		"access_token":  tokens.AccessToken,
		"refresh_token": tokens.RefreshToken,
		"expires_in":    tokens.ExpiresIn,
	})
}

// Login handles POST /v1/auth/login
func (h *HTTPHandlers) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON payload")
		return
	}

	user, tokens, err := h.authSvc.Login(r.Context(), req)
	if err != nil {
		h.respondError(w, http.StatusUnauthorized, "login_failed", err.Error())
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"user_id":       user.ID.String(),
		"access_token":  tokens.AccessToken,
		"refresh_token": tokens.RefreshToken,
		"expires_in":    tokens.ExpiresIn,
	})
}

// CreateGuest handles POST /v1/auth/guest
func (h *HTTPHandlers) CreateGuest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req GuestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON payload")
		return
	}

	if req.DisplayName == "" {
		req.DisplayName = "Guest"
	}

	user, tokens, err := h.authSvc.CreateGuest(r.Context(), req)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "guest_creation_failed", err.Error())
		return
	}

	h.respondJSON(w, http.StatusCreated, map[string]interface{}{
		"guest_id":      user.ID.String(),
		"access_token":  tokens.AccessToken,
		"refresh_token": tokens.RefreshToken,
		"expires_in":    tokens.ExpiresIn,
	})
}

// ConvertGuest handles POST /v1/auth/convert
func (h *HTTPHandlers) ConvertGuest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ConvertGuestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON payload")
		return
	}

	user, tokens, err := h.authSvc.ConvertGuest(r.Context(), req)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "conversion_failed", err.Error())
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"user_id":       user.ID.String(),
		"access_token":  tokens.AccessToken,
		"refresh_token": tokens.RefreshToken,
		"expires_in":    tokens.ExpiresIn,
		"converted":     true,
	})
}

// RefreshToken handles POST /v1/auth/refresh
func (h *HTTPHandlers) RefreshToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON payload")
		return
	}

	tokens, err := h.authSvc.RefreshToken(r.Context(), req.RefreshToken)
	if err != nil {
		h.respondError(w, http.StatusUnauthorized, "refresh_failed", err.Error())
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"access_token": tokens.AccessToken,
		"expires_in":   tokens.ExpiresIn,
	})
}

// OAuthStart handles GET /v1/oauth/{provider}/start
func (h *HTTPHandlers) OAuthStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.oauthSvc == nil {
		h.respondError(w, http.StatusServiceUnavailable, "oauth_not_configured", "OAuth is not configured")
		return
	}

	// Extract provider from URL path (e.g., /v1/oauth/google/start)
	provider := extractProviderFromPath(r.URL.Path)
	if provider == "" {
		provider = OAuthProviderGoogle // default
	}

	// Generate CSRF state token
	state := uuid.New().String()

	authURL, err := h.oauthSvc.StartOAuthFlow(provider, state)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "oauth_start_failed", err.Error())
		return
	}

	// Store state in session/cookie for validation (simplified for MVP)
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600, // 10 minutes
	})

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"auth_url": authURL,
		"state":    state,
	})
}

// OAuthCallback handles GET /v1/oauth/{provider}/callback
func (h *HTTPHandlers) OAuthCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.oauthSvc == nil {
		h.respondError(w, http.StatusServiceUnavailable, "oauth_not_configured", "OAuth is not configured")
		return
	}

	// Extract provider from URL path (e.g., /v1/oauth/google/callback)
	provider := extractProviderFromPath(r.URL.Path)
	if provider == "" {
		provider = OAuthProviderGoogle
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	if code == "" {
		h.respondError(w, http.StatusBadRequest, "missing_code", "Authorization code required")
		return
	}

	// Validate state (CSRF protection)
	cookie, err := r.Cookie("oauth_state")
	if err != nil || cookie.Value != state {
		h.respondError(w, http.StatusBadRequest, "invalid_state", "Invalid or missing state parameter")
		return
	}

	// Exchange code for user info
	userInfo, err := h.oauthSvc.HandleOAuthCallback(r.Context(), provider, code, state)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "oauth_callback_failed", err.Error())
		return
	}

	// Create or get user
	user, tokens, err := h.oauthSvc.CreateOrGetOAuthUser(r.Context(), h.authSvc, provider, userInfo)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "user_creation_failed", err.Error())
		return
	}

	// Clear state cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    "",
		MaxAge:   -1,
		HttpOnly: true,
	})

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"user_id":       user.ID.String(),
		"access_token":  tokens.AccessToken,
		"refresh_token": tokens.RefreshToken,
		"expires_in":    tokens.ExpiresIn,
	})
}

// GetMe handles GET /v1/users/me (requires auth middleware)
func (h *HTTPHandlers) GetMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract user from context (set by auth middleware)
	claims, ok := r.Context().Value("claims").(*jwt.Claims)
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "unauthorized", "Invalid or missing token")
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"user_id":      claims.UserID.String(),
		"email":        claims.Email,
		"display_name": claims.DisplayName,
		"user_type":    claims.UserID,
		"is_guest":     claims.IsGuest,
	})
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

// extractProviderFromPath extracts provider name from URL path.
// Example: /v1/oauth/google/start -> "google"
func extractProviderFromPath(path string) string {
	// Simple extraction: look for /oauth/{provider}/ pattern
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if part == "oauth" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}
