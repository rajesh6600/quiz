package server

import (
	"context"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/gokatarajesh/quiz-platform/internal/auth"
	"github.com/gokatarajesh/quiz-platform/internal/config"
)

// WSUpgrader handles WebSocket upgrades (configure CORS/security as needed).
var WSUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// TODO: implement proper origin checking for production
		return true
	},
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// NewHTTPServer wires base routes (health, metrics) for the API service.
// authHandlers can be nil if auth is not yet initialized.
func NewHTTPServer(cfg *config.App, logger zerolog.Logger, pool *pgxpool.Pool, redis *redis.Client, authHandlers *auth.HTTPHandlers, matchWSHandler http.HandlerFunc, leaderboardHandler http.HandlerFunc) *http.Server {
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
			http.Error(w, "upstream error", http.StatusBadGateway)
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
	}

	// WebSocket endpoint
	if matchWSHandler != nil {
		mux.HandleFunc("/ws/matches", matchWSHandler)
	} else {
		mux.HandleFunc("/ws/matches", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "WebSocket handler not yet integrated", http.StatusNotImplemented)
		})
	}

	if leaderboardHandler != nil {
		mux.HandleFunc("/v1/leaderboards/", leaderboardHandler)
		// Private room leaderboard endpoint (must be before the general one to match first)
		mux.HandleFunc("/v1/leaderboards/private/", leaderboardHandler)
	}

	return &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: mux,
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
