package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/gokatarajesh/quiz-platform/internal/auth"
	"github.com/gokatarajesh/quiz-platform/internal/config"
	httperrors "github.com/gokatarajesh/quiz-platform/pkg/http/errors"
)

// WSUpgrader handles WebSocket upgrades (configure CORS/security as needed).
// CheckOrigin will be set dynamically based on CORS config.
var WSUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// corsMiddleware creates a CORS middleware handler.
func corsMiddleware(cfg config.CORS, logger zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// Check if origin is allowed
			allowedOrigin := ""
			if origin != "" {
				for _, allowed := range cfg.AllowedOrigins {
					if origin == allowed {
						allowedOrigin = origin
						break
					}
				}
			}

			// Set CORS headers
			if allowedOrigin != "" {
				w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
			}

			// Set other CORS headers
			if len(cfg.AllowedMethods) > 0 {
				w.Header().Set("Access-Control-Allow-Methods", strings.Join(cfg.AllowedMethods, ","))
			}
			if len(cfg.AllowedHeaders) > 0 {
				w.Header().Set("Access-Control-Allow-Headers", strings.Join(cfg.AllowedHeaders, ","))
			}
			if cfg.AllowCredentials {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			if cfg.MaxAge > 0 {
				w.Header().Set("Access-Control-Max-Age", fmt.Sprintf("%d", cfg.MaxAge))
			}

			// Handle preflight OPTIONS request
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			// Log CORS violations
			if origin != "" && allowedOrigin == "" {
				logger.Warn().
					Str("origin", origin).
					Strs("allowed_origins", cfg.AllowedOrigins).
					Msg("CORS: blocked request from disallowed origin")
			}

			next.ServeHTTP(w, r)
		})
	}
}

// createWSOriginChecker creates a function to check WebSocket origins based on CORS config.
func createWSOriginChecker(cfg config.CORS) func(*http.Request) bool {
	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			// Allow requests without Origin header (same-origin or non-browser clients)
			return true
		}

		// Check against allowed origins
		for _, allowed := range cfg.AllowedOrigins {
			if origin == allowed {
				return true
			}
		}

		return false
	}
}

// NewHTTPServer wires base routes (health, metrics) for the API service.
// authHandlers can be nil if auth is not yet initialized.
func NewHTTPServer(cfg *config.App, logger zerolog.Logger, pool *pgxpool.Pool, redis *redis.Client, authHandlers *auth.HTTPHandlers, matchGetRoomHandler http.HandlerFunc, matchRoomHandler http.Handler, matchWSHandler http.HandlerFunc, leaderboardHandler http.HandlerFunc) *http.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	mux.Handle("/metrics", promhttp.Handler())

	// Placeholder root handler demonstrating dependency availability.
	mux.HandleFunc("/v1/ping", func(w http.ResponseWriter, r *http.Request) {
		ctx := loggingContext(r.Context(), logger)
		if err := pingDependencies(ctx, pool, redis); err != nil {
			logger.Error().Err(err).Msg("dependency ping failed")
			httperrors.RespondError(w, http.StatusBadGateway, httperrors.ErrCodeUpstreamError, "Upstream error")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"pong":true}`))
	})

	// Auth endpoints
	if authHandlers != nil {
		mux.HandleFunc("/v1/auth/register", authHandlers.Register)
		mux.HandleFunc("/v1/auth/login", authHandlers.Login)
		mux.HandleFunc("/v1/auth/guest", authHandlers.CreateGuest)
		mux.HandleFunc("/v1/auth/convert", authHandlers.ConvertGuest)
		mux.HandleFunc("/v1/auth/refresh", authHandlers.RefreshToken)
		mux.HandleFunc("/v1/auth/forgot-password", authHandlers.ForgotPassword)
		mux.HandleFunc("/v1/auth/reset-password", authHandlers.ResetPassword)
		mux.HandleFunc("/v1/oauth/{provider}/start", authHandlers.OAuthStart)
		mux.HandleFunc("/v1/oauth/{provider}/callback", authHandlers.OAuthCallback)
		mux.HandleFunc("/v1/users/me", authHandlers.GetMe)
		mux.HandleFunc("/v1/users/me/username", authHandlers.SetUsername)
	}

	// WebSocket endpoint
	if matchWSHandler != nil {
		mux.HandleFunc("/ws/matches", matchWSHandler)
	} else {
		mux.HandleFunc("/ws/matches", func(w http.ResponseWriter, r *http.Request) {
			httperrors.RespondError(w, http.StatusNotImplemented, httperrors.ErrCodeNotImplemented, "WebSocket handler not yet integrated")
		})
	}

	if leaderboardHandler != nil {
		mux.HandleFunc("/v1/leaderboards/", leaderboardHandler)
		// Private room leaderboard endpoint (must be before the general one to match first)
		mux.HandleFunc("/v1/leaderboards/private/", leaderboardHandler)
	}

	// Match endpoints (rooms)
	// POST /v1/rooms - Create room (requires auth, wrapped with middleware)
	if matchRoomHandler != nil {
		mux.Handle("/v1/rooms", matchRoomHandler)
	}
	// GET /v1/rooms/{room_code} - Get room details (public, no auth)
	if matchGetRoomHandler != nil {
		mux.HandleFunc("/v1/rooms/", matchGetRoomHandler)
	}

	// Apply CORS middleware to all routes
	handler := corsMiddleware(cfg.CORS, logger)(mux)

	// Update WebSocket upgrader with CORS origin checking
	WSUpgrader.CheckOrigin = createWSOriginChecker(cfg.CORS)

	return &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: handler,
	}
}

func pingDependencies(ctx context.Context, pool *pgxpool.Pool, redis *redis.Client) error {
	if err := pool.Ping(ctx); err != nil {
		return err
	}
	if err := redis.Ping(ctx).Err(); err != nil {
		return err
	}
	return nil
}

func loggingContext(ctx context.Context, logger zerolog.Logger) context.Context {
	return context.WithValue(ctx, ctxLoggerKey{}, logger)
}

type ctxLoggerKey struct{}
