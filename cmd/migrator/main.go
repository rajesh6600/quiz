package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	var (
		command = flag.String("command", "up", "Migration command: up, down, or status")
		dir     = flag.String("dir", "db/migrations", "Directory containing migration files")
	)
	flag.Parse()

	// Setup logging
	log.Logger = zerolog.New(os.Stderr).With().Timestamp().Logger()

	// Read database configuration from environment
	pgHost := getEnv("PG_HOST", "localhost")
	pgPort := getEnv("PG_PORT", "5432")
	pgUser := getEnv("PG_USER", "")
	pgPassword := getEnv("PG_PASSWORD", "")
	pgDatabase := getEnv("PG_DATABASE", "")
	pgSSLMode := getEnv("PG_SSL_MODE", "disable")

	if pgUser == "" {
		log.Fatal().Msg("PG_USER environment variable is required")
	}
	if pgPassword == "" {
		log.Fatal().Msg("PG_PASSWORD environment variable is required")
	}
	if pgDatabase == "" {
		log.Fatal().Msg("PG_DATABASE environment variable is required")
	}

	// Build connection string
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		pgHost, pgPort, pgUser, pgPassword, pgDatabase, pgSSLMode)

	// Resolve migration directory (relative to project root)
	migrationDir, err := filepath.Abs(*dir)
	if err != nil {
		log.Fatal().Err(err).Str("dir", *dir).Msg("failed to resolve migration directory")
	}

	if _, err := os.Stat(migrationDir); os.IsNotExist(err) {
		log.Fatal().Str("dir", migrationDir).Msg("migration directory does not exist")
	}

	// Connect to database using pgx via stdlib (database/sql compatible)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatal().Err(err).Str("host", pgHost).Str("port", pgPort).Msg("failed to open database connection")
	}
	defer db.Close()

	// Verify connection
	if err := db.Ping(); err != nil {
		log.Fatal().Err(err).Msg("failed to ping database")
	}

	log.Info().
		Str("host", pgHost).
		Str("port", pgPort).
		Str("database", pgDatabase).
		Str("migration_dir", migrationDir).
		Msg("connected to database")

	// Configure goose
	goose.SetBaseFS(nil)
	goose.SetTableName("goose_db_version")

	// Run migration command
	switch *command {
	case "up":
		if err := goose.Up(db, migrationDir); err != nil {
			log.Fatal().Err(err).Msg("failed to run migrations up")
		}
		log.Info().Msg("migrations applied successfully")

	case "down":
		if err := goose.Down(db, migrationDir); err != nil {
			log.Fatal().Err(err).Msg("failed to run migrations down")
		}
		log.Info().Msg("migrations rolled back successfully")

	case "status":
		if err := goose.Status(db, migrationDir); err != nil {
			log.Fatal().Err(err).Msg("failed to get migration status")
		}

	default:
		log.Fatal().Str("command", *command).Msg("unknown command. Use: up, down, or status")
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

