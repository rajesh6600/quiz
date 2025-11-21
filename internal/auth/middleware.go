package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/rs/zerolog"

	"github.com/gokatarajesh/quiz-platform/internal/auth/jwt"
)

// AuthMiddleware validates JWT tokens and injects user claims into request context.
func AuthMiddleware(authSvc *Service, logger zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract token from Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				next.ServeHTTP(w, r) // Allow unauthenticated requests
				return
			}

			// Parse "Bearer <token>"
			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || parts[0] != "Bearer" {
				http.Error(w, "Invalid authorization header", http.StatusUnauthorized)
				return
			}

			token := parts[1]
			claims, err := authSvc.ValidateToken(token)
			if err != nil {
				logger.Warn().Err(err).Msg("token validation failed")
				http.Error(w, "Invalid or expired token", http.StatusUnauthorized)
				return
			}

			// Inject claims into context
			ctx := context.WithValue(r.Context(), "claims", claims)
			ctx = context.WithValue(ctx, "user_id", claims.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAuth ensures the request is authenticated.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value("claims").(*jwt.Claims)
		if !ok || claims == nil {
			http.Error(w, "Authentication required", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireRegistered ensures the user is not a guest.
func RequireRegistered(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value("claims").(*jwt.Claims)
		if !ok || claims == nil || claims.IsGuest {
			http.Error(w, "Registered account required", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
