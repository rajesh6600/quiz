package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/gokatarajesh/quiz-platform/internal/auth"
	"github.com/gokatarajesh/quiz-platform/internal/auth/jwt"
	"github.com/gokatarajesh/quiz-platform/internal/config"
	"github.com/gokatarajesh/quiz-platform/internal/db/repository"
	sqlcgen "github.com/gokatarajesh/quiz-platform/internal/db/sqlc"
	"github.com/gokatarajesh/quiz-platform/internal/leaderboard"
	"github.com/gokatarajesh/quiz-platform/internal/logging"
	"github.com/gokatarajesh/quiz-platform/internal/match"
	matchqueue "github.com/gokatarajesh/quiz-platform/internal/match/queue"
	"github.com/gokatarajesh/quiz-platform/internal/question"
	"github.com/gokatarajesh/quiz-platform/internal/question/ai"
	"github.com/gokatarajesh/quiz-platform/internal/question/external"
	"github.com/gokatarajesh/quiz-platform/internal/server"
	ws "github.com/gokatarajesh/quiz-platform/pkg/http/ws"
)

// Application aggregates shared infrastructure (DB, cache, HTTP server).
type Application struct {
	cfg    *config.App
	logger zerolog.Logger

	pool  *pgxpool.Pool
	redis *redis.Client
	http  *http.Server

	lbBroadcaster  *leaderboard.Broadcaster
	snapshotWorker *leaderboard.SnapshotWorker
	bgCancels      []context.CancelFunc
}

// New bootstraps configs, logger, Postgres, Redis and HTTP server.
func New(ctx context.Context, cfg *config.App) (*Application, error) {
	logger := logging.New(cfg.Name, cfg.Env)
	logger.Info().Msg("starting application bootstrap")

	connString := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s pool_max_conns=10",
		cfg.Postgres.Host, cfg.Postgres.Port, cfg.Postgres.User, cfg.Postgres.Password, cfg.Postgres.Database, cfg.Postgres.SSLMode)

	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		DB:       cfg.Redis.DB,
		PoolSize: cfg.Redis.PoolSize,
	})

	queries := sqlcgen.New(pool)

	userRepo := repository.NewUserRepository(queries)
	questionRepo := repository.NewQuestionRepository(queries)
	matchRepo := repository.NewMatchRepository(queries)

	if cfg.Security.QuestionHMACSecret == "" {
		return nil, fmt.Errorf("QUESTION_HMAC_SECRET must be configured")
	}

	// Initialize auth services
	var authSvc *auth.Service
	var oauthSvc *auth.OAuthService
	var authHandlers *auth.HTTPHandlers
	if cfg.Security.JWTSecret != "" {
		// Create JWT token manager
		tokenCfg := jwt.TokenConfig{
			AccessSecret:  []byte(cfg.Security.JWTSecret),
			RefreshSecret: []byte(cfg.Security.JWTSecret + "_refresh"), // Use same secret with suffix for MVP
			Issuer:        cfg.Name,
		}

		// Create auth service
		authSvc = auth.NewService(userRepo, auth.ServiceOptions{
			TokenConfig: tokenCfg,
		}, logger)

		// Create OAuth service (if configured)
		if cfg.OAuth.GoogleClientID != "" && cfg.OAuth.GoogleClientSecret != "" {
			redirectURL := cfg.OAuth.GoogleRedirectURL
			if redirectURL == "" {
				redirectURL = fmt.Sprintf("http://%s/v1/oauth/google/callback", cfg.HTTPAddr)
			}
			oauthSvc = auth.NewOAuthService(
				cfg.OAuth.GoogleClientID,
				cfg.OAuth.GoogleClientSecret,
				redirectURL,
				logger,
			)
			logger.Info().Msg("OAuth service initialized")
		} else {
			logger.Warn().Msg("OAuth not configured (missing GOOGLE_OAUTH_CLIENT_ID or GOOGLE_OAUTH_CLIENT_SECRET)")
		}

		// Create HTTP handlers
		authHandlers = auth.NewHTTPHandlers(authSvc, oauthSvc, logger)
		logger.Info().Msg("Auth handlers initialized")
	} else {
		logger.Warn().Msg("JWT secret not configured; authentication-dependent APIs disabled")
	}

	if authSvc == nil {
		return nil, fmt.Errorf("authentication service must be configured (set JWT_SECRET)")
	}

	// Core gameplay services
	questionCache := question.NewCache(redisClient, 0)
	opentdbClient := external.NewOpenTDBClient("", nil)
	triviaClient := external.NewTriviaAPIClient("", "", nil)

	var aiGenerator question.AIGenerator
	if cfg.AI.GeneratorURL != "" {
		aiGenerator = ai.NewGenerator(ai.Config{
			GeneratorURL: cfg.AI.GeneratorURL,
			GeneratorKey: cfg.AI.GeneratorKey,
			Timeout:      cfg.AI.HTTPTimeout,
		}, logger)
	}

	questionSvc := question.NewService(
		questionRepo,
		questionCache,
		opentdbClient,
		triviaClient,
		aiGenerator,
		question.ServiceOptions{HMACSecret: []byte(cfg.Security.QuestionHMACSecret)},
	)

	stateMgr := match.NewStateManager(redisClient, logger)
	queueMgr := matchqueue.NewManager(redisClient, logger, 10)
	roomMgr := match.NewRoomManager(redisClient, logger)
	leaderboardSvc := leaderboard.NewService(redisClient, logger, leaderboard.ServiceOptions{})
	wsHub := ws.NewHub(logger)

	matchSvc := match.NewService(
		matchRepo,
		questionSvc,
		stateMgr,
		queueMgr,
		roomMgr,
		leaderboardSvc,
		match.ServiceOptions{
			HMACSecret: []byte(cfg.Security.QuestionHMACSecret),
		},
		logger,
	)

	matchWSHandler := match.NewHandler(matchSvc, wsHub, authSvc, logger)
	lbBroadcaster := leaderboard.NewBroadcaster(redisClient, wsHub, "", logger)
	lbHTTPHandler := leaderboard.NewHTTPHandler(leaderboardSvc, queries, logger)
	var snapshotWorker *leaderboard.SnapshotWorker
	if interval := cfg.Leaderboard.SnapshotInterval; interval > 0 {
		snapshotWorker = leaderboard.NewSnapshotWorker(
			leaderboardSvc,
			queries,
			interval,
			cfg.Leaderboard.SnapshotTopN,
			logger,
		)
	}

	apiServer := server.NewHTTPServer(cfg, logger, pool, redisClient, authHandlers, matchWSHandler.HandleWebSocket, lbHTTPHandler.HandleGet)

	return &Application{
		cfg:            cfg,
		logger:         logger,
		pool:           pool,
		redis:          redisClient,
		http:           apiServer,
		lbBroadcaster:  lbBroadcaster,
		snapshotWorker: snapshotWorker,
		bgCancels:      make([]context.CancelFunc, 0, 2),
	}, nil
}

// Run starts the HTTP server and waits for termination signals.
func (a *Application) Run(ctx context.Context) error {
	errCh := make(chan error, 1)

	a.startBackgroundWorkers(ctx)

	go func() {
		a.logger.Info().Str("addr", a.cfg.HTTPAddr).Msg("http server listening")
		if err := a.http.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		a.logger.Info().Str("signal", sig.String()).Msg("shutdown signal received")
	case err := <-errCh:
		return fmt.Errorf("http server error: %w", err)
	case <-ctx.Done():
		a.logger.Warn().Msg("context canceled")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.GracefulShutdownTimeout)
	defer cancel()

	if err := a.http.Shutdown(shutdownCtx); err != nil {
		a.logger.Error().Err(err).Msg("http shutdown error")
	}

	for _, cancel := range a.bgCancels {
		cancel()
	}

	a.pool.Close()
	if err := a.redis.Close(); err != nil {
		a.logger.Error().Err(err).Msg("redis shutdown error")
	}

	a.logger.Info().Msg("shutdown complete")
	return nil
}

func (a *Application) startBackgroundWorkers(ctx context.Context) {
	if a.lbBroadcaster != nil {
		bgCtx, cancel := context.WithCancel(ctx)
		a.bgCancels = append(a.bgCancels, cancel)
		go func() {
			if err := a.lbBroadcaster.Run(bgCtx); err != nil && err != context.Canceled {
				a.logger.Warn().Err(err).Msg("leaderboard broadcaster stopped")
			}
		}()
	}

	if a.snapshotWorker != nil {
		bgCtx, cancel := context.WithCancel(ctx)
		a.bgCancels = append(a.bgCancels, cancel)
		go func() {
			if err := a.snapshotWorker.Run(bgCtx); err != nil && err != context.Canceled {
				a.logger.Warn().Err(err).Msg("leaderboard snapshot worker stopped")
			}
		}()
	}
}
