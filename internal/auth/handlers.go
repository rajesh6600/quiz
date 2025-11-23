package auth

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog"

	"github.com/gokatarajesh/quiz-platform/internal/auth/jwt"
	httperrors "github.com/gokatarajesh/quiz-platform/pkg/http/errors"
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
		httperrors.RespondError(w, http.StatusMethodNotAllowed, httperrors.ErrCodeInvalidRequest, "Method not allowed")
		return
	}

	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httperrors.RespondBadRequest(w, httperrors.ErrCodeInvalidRequest, "Invalid JSON payload")
		return
	}

	user, tokens, err := h.authSvc.Register(r.Context(), req)
	if err != nil {
		httperrors.RespondBadRequest(w, httperrors.ErrCodeRegistrationFailed, err.Error())
		return
	}

	usernameRequired := user.Username == ""

	h.respondJSON(w, http.StatusCreated, map[string]interface{}{
		"user_id":          user.ID.String(),
		"access_token":     tokens.AccessToken,
		"refresh_token":    tokens.RefreshToken,
		"expires_in":       tokens.ExpiresIn,
		"username_required": usernameRequired,
	})
}

// Login handles POST /v1/auth/login
func (h *HTTPHandlers) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httperrors.RespondError(w, http.StatusMethodNotAllowed, httperrors.ErrCodeInvalidRequest, "Method not allowed")
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httperrors.RespondBadRequest(w, httperrors.ErrCodeInvalidRequest, "Invalid JSON payload")
		return
	}

	user, tokens, err := h.authSvc.Login(r.Context(), req)
	if err != nil {
		httperrors.RespondUnauthorized(w, httperrors.ErrCodeLoginFailed, err.Error())
		return
	}

	usernameRequired := user.Username == ""

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"user_id":          user.ID.String(),
		"access_token":     tokens.AccessToken,
		"refresh_token":    tokens.RefreshToken,
		"expires_in":       tokens.ExpiresIn,
		"username_required": usernameRequired,
	})
}

// CreateGuest handles POST /v1/auth/guest
func (h *HTTPHandlers) CreateGuest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httperrors.RespondError(w, http.StatusMethodNotAllowed, httperrors.ErrCodeInvalidRequest, "Method not allowed")
		return
	}

	var req GuestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httperrors.RespondBadRequest(w, httperrors.ErrCodeInvalidRequest, "Invalid JSON payload")
		return
	}

	user, tokens, err := h.authSvc.CreateGuest(r.Context(), req)
	if err != nil {
		httperrors.RespondBadRequest(w, httperrors.ErrCodeGuestCreationFailed, err.Error())
		return
	}

	h.respondJSON(w, http.StatusCreated, map[string]interface{}{
		"guest_id":      user.ID.String(),
		"username":      user.Username,
		"access_token":  tokens.AccessToken,
		"refresh_token": tokens.RefreshToken,
		"expires_in":    tokens.ExpiresIn,
	})
}

// ConvertGuest handles POST /v1/auth/convert
func (h *HTTPHandlers) ConvertGuest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httperrors.RespondError(w, http.StatusMethodNotAllowed, httperrors.ErrCodeInvalidRequest, "Method not allowed")
		return
	}

	var req ConvertGuestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httperrors.RespondBadRequest(w, httperrors.ErrCodeInvalidRequest, "Invalid JSON payload")
		return
	}

	user, tokens, err := h.authSvc.ConvertGuest(r.Context(), req)
	if err != nil {
		httperrors.RespondBadRequest(w, httperrors.ErrCodeConversionFailed, err.Error())
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
		httperrors.RespondError(w, http.StatusMethodNotAllowed, httperrors.ErrCodeInvalidRequest, "Method not allowed")
		return
	}

	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httperrors.RespondBadRequest(w, httperrors.ErrCodeInvalidRequest, "Invalid JSON payload")
		return
	}

	tokens, err := h.authSvc.RefreshToken(r.Context(), req.RefreshToken)
	if err != nil {
		httperrors.RespondUnauthorized(w, httperrors.ErrCodeRefreshFailed, err.Error())
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
		httperrors.RespondError(w, http.StatusMethodNotAllowed, httperrors.ErrCodeInvalidRequest, "Method not allowed")
		return
	}

	if h.oauthSvc == nil {
		httperrors.RespondServiceUnavailable(w, httperrors.ErrCodeOAuthNotConfigured, "OAuth is not configured")
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
		httperrors.RespondBadRequest(w, httperrors.ErrCodeOAuthStartFailed, err.Error())
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
		httperrors.RespondError(w, http.StatusMethodNotAllowed, httperrors.ErrCodeInvalidRequest, "Method not allowed")
		return
	}

	if h.oauthSvc == nil {
		httperrors.RespondServiceUnavailable(w, httperrors.ErrCodeOAuthNotConfigured, "OAuth is not configured")
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
		httperrors.RespondBadRequest(w, httperrors.ErrCodeOAuthMissingCode, "Authorization code required")
		return
	}

	// Validate state (CSRF protection)
	cookie, err := r.Cookie("oauth_state")
	if err != nil || cookie.Value != state {
		httperrors.RespondBadRequest(w, httperrors.ErrCodeOAuthInvalidState, "Invalid or missing state parameter")
		return
	}

	// Exchange code for user info
	userInfo, err := h.oauthSvc.HandleOAuthCallback(r.Context(), provider, code, state)
	if err != nil {
		httperrors.RespondBadRequest(w, httperrors.ErrCodeOAuthCallbackFailed, err.Error())
		return
	}

	// Create or get user
	user, tokens, err := h.oauthSvc.CreateOrGetOAuthUser(r.Context(), h.authSvc, provider, userInfo)
	if err != nil {
		httperrors.RespondInternalError(w, err.Error())
		return
	}

	// Clear state cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    "",
		MaxAge:   -1,
		HttpOnly: true,
	})

	usernameRequired := user.Username == ""

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"user_id":          user.ID.String(),
		"access_token":     tokens.AccessToken,
		"refresh_token":    tokens.RefreshToken,
		"expires_in":       tokens.ExpiresIn,
		"username_required": usernameRequired,
	})
}

// GetMe handles GET /v1/users/me (requires auth middleware)
func (h *HTTPHandlers) GetMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httperrors.RespondError(w, http.StatusMethodNotAllowed, httperrors.ErrCodeInvalidRequest, "Method not allowed")
		return
	}

	// Extract user from context (set by auth middleware)
	claims, ok := r.Context().Value("claims").(*jwt.Claims)
	if !ok {
		httperrors.RespondUnauthorized(w, httperrors.ErrCodeUnauthorized, "Invalid or missing token")
		return
	}

	// Fetch full user data from DB to check username
	userID := claims.UserID
	username := claims.Username // Default to token username (works for guests)
	usernameRequired := false
	
	// For registered users, check DB to see if username is set
	if !claims.IsGuest {
		var pgUserID pgtype.UUID
		pgUserID.Scan(claims.UserID)
		dbUser, err := h.authSvc.userRepo.GetByID(r.Context(), pgUserID)
	if err == nil {
		if dbUser.Username.Valid && dbUser.Username.String != "" {
			username = dbUser.Username.String
			usernameRequired = false
		} else {
			username = ""
			usernameRequired = true
		}
	}
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"user_id":          userID.String(),
		"email":            claims.Email,
		"username":         username,
		"username_required": usernameRequired,
		"user_type":        claims.UserType,
		"is_guest":         claims.IsGuest,
	})
}

// SetUsername handles POST /v1/users/me/username (requires auth middleware)
func (h *HTTPHandlers) SetUsername(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httperrors.RespondError(w, http.StatusMethodNotAllowed, httperrors.ErrCodeInvalidRequest, "Method not allowed")
		return
	}

	// Extract user from context (set by auth middleware)
	claims, ok := r.Context().Value("claims").(*jwt.Claims)
	if !ok {
		httperrors.RespondUnauthorized(w, httperrors.ErrCodeUnauthorized, "Invalid or missing token")
		return
	}

	var req SetUsernameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httperrors.RespondBadRequest(w, httperrors.ErrCodeInvalidRequest, "Invalid JSON payload")
		return
	}

	if req.Username == "" {
		httperrors.RespondValidationError(w, httperrors.ErrCodeMissingField, "Username required", "username")
		return
	}

	user, err := h.authSvc.SetUsername(r.Context(), claims.UserID, req.Username)
	if err != nil {
		// Check if it's a username_taken error with suggestions
		errStr := err.Error()
		if strings.Contains(errStr, "username_taken") {
			// Extract suggestions from error message (format: "username_taken: [suggestion1 suggestion2 suggestion3]")
			parts := strings.Split(errStr, ": ")
			if len(parts) > 1 {
				// Parse the suggestions array from the string
				suggestionsStr := strings.Trim(parts[1], "[]")
				suggestions := strings.Fields(suggestionsStr)
				httperrors.RespondErrorWithDetails(w, http.StatusConflict, httperrors.ErrCodeUsernameTaken, "Username is already taken", map[string]interface{}{
					"suggestions": suggestions,
				})
				return
			}
		}
		httperrors.RespondBadRequest(w, httperrors.ErrCodeSetUsernameFailed, err.Error())
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"user_id":   user.ID.String(),
		"username":  user.Username,
		"email":     user.Email,
		"user_type": user.UserType,
		"is_guest":  user.IsGuest,
	})
}

// ForgotPassword handles POST /v1/auth/forgot-password
func (h *HTTPHandlers) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httperrors.RespondError(w, http.StatusMethodNotAllowed, httperrors.ErrCodeInvalidRequest, "Method not allowed")
		return
	}

	var req ForgotPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httperrors.RespondBadRequest(w, httperrors.ErrCodeInvalidRequest, "Invalid JSON payload")
		return
	}

	if req.Email == "" {
		httperrors.RespondValidationError(w, httperrors.ErrCodeMissingField, "Email required", "email")
		return
	}

	// Request password reset (always returns success for security)
	if err := h.authSvc.RequestPasswordReset(r.Context(), req.Email); err != nil {
		h.logger.Warn().Err(err).Str("email", req.Email).Msg("password reset request failed")
		// Don't reveal error to client - security best practice
	}

	// Always return success to prevent email enumeration
	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"message": "If an account exists with this email, a password reset link has been sent",
	})
}

// ResetPassword handles POST /v1/auth/reset-password
func (h *HTTPHandlers) ResetPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httperrors.RespondError(w, http.StatusMethodNotAllowed, httperrors.ErrCodeInvalidRequest, "Method not allowed")
		return
	}

	var req ResetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httperrors.RespondBadRequest(w, httperrors.ErrCodeInvalidRequest, "Invalid JSON payload")
		return
	}

	if req.Token == "" || req.NewPassword == "" {
		httperrors.RespondValidationError(w, httperrors.ErrCodeMissingField, "Token and new password required", "")
		return
	}

	if err := h.authSvc.ResetPassword(r.Context(), req.Token, req.NewPassword); err != nil {
		httperrors.RespondBadRequest(w, httperrors.ErrCodeResetFailed, err.Error())
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Password reset successfully",
	})
}

func (h *HTTPHandlers) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
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
