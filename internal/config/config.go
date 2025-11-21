package config

import (
	"context"
	"fmt"
	"time"

	"github.com/caarlos0/env/v10"
)

// App holds core runtime configuration shared across services.
type App struct {
	Name                    string        `env:"APP_NAME" envDefault:"quiz-platform"`
	Env                     string        `env:"APP_ENV" envDefault:"development"`
	HTTPAddr                string        `env:"HTTP_ADDR" envDefault:"0.0.0.0:8080"`
	GracefulShutdownTimeout time.Duration `env:"GRACEFUL_SHUTDOWN_SECONDS" envDefault:"20s"`

	Postgres    Postgres
	Redis       Redis
	Security    Security
	Runtime     Runtime
	OAuth       OAuth
	Leaderboard Leaderboard
	AI          AI
}

// Postgres captures connection info for the SQL database.
type Postgres struct {
	Host     string `env:"PG_HOST,notEmpty"`
	Port     int    `env:"PG_PORT" envDefault:"5433"`
	User     string `env:"PG_USER,notEmpty"`
	Password string `env:"PG_PASSWORD,notEmpty"`
	Database string `env:"PG_DATABASE,notEmpty"`
	SSLMode  string `env:"PG_SSL_MODE" envDefault:"disable"`
}

// Redis holds cache + queue configuration.
type Redis struct {
	Addr     string `env:"REDIS_ADDR,notEmpty"`
	DB       int    `env:"REDIS_DB" envDefault:"0"`
	PoolSize int    `env:"REDIS_POOL_SIZE" envDefault:"20"`
}

// Security stores secrets for signing and auth.
type Security struct {
	JWTSecret          string `env:"JWT_SECRET,notEmpty"`
	QuestionHMACSecret string `env:"QUESTION_HMAC_SECRET,notEmpty"`
}

// Runtime groups gameplay defaults.
type Runtime struct {
	QuestionFetchTimeout   time.Duration `env:"QUESTION_FETCH_TIMEOUT_SECONDS" envDefault:"4s"`
	DefaultQuestionCount   int           `env:"DEFAULT_QUESTION_COUNT" envDefault:"5"`
	DefaultQuestionSeconds int           `env:"DEFAULT_PER_QUESTION_SECONDS" envDefault:"15"`
	GlobalPaddingSeconds   int           `env:"GLOBAL_TIMEOUT_PADDING_SECONDS" envDefault:"20s"`
}

// Leaderboard governs snapshotting and broadcast behavior.
type Leaderboard struct {
	SnapshotInterval time.Duration `env:"LEADERBOARD_SNAPSHOT_INTERVAL" envDefault:"5m"`
	SnapshotTopN     int           `env:"LEADERBOARD_SNAPSHOT_TOP" envDefault:"50"`
}

// OAuth holds OAuth provider configuration.
type OAuth struct {
	GoogleClientID     string `env:"GOOGLE_OAUTH_CLIENT_ID"`
	GoogleClientSecret string `env:"GOOGLE_OAUTH_CLIENT_SECRET"`
	GoogleRedirectURL  string `env:"GOOGLE_OAUTH_REDIRECT_URL"`
}

// AI configures the AI generator service.
type AI struct {
	GeneratorURL string        `env:"AI_GENERATOR_URL"`
	GeneratorKey string        `env:"AI_GENERATOR_API_KEY"`
	HTTPTimeout  time.Duration `env:"AI_HTTP_TIMEOUT" envDefault:"6s"`
}

// Load parses environment variables into App config.
func Load(ctx context.Context) (*App, error) {
	cfg := &App{}
	if err := env.ParseWithOptions(cfg, env.Options{RequiredIfNoDef: true}); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}
