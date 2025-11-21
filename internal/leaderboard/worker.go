package leaderboard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog"

	sqlcgen "github.com/gokatarajesh/quiz-platform/internal/db/sqlc"
)

// SnapshotWorker periodically persists Redis leaderboards into Postgres.
type SnapshotWorker struct {
	svc      *Service
	queries  *sqlcgen.Queries
	logger   zerolog.Logger
	interval time.Duration
	topN     int
}

func NewSnapshotWorker(svc *Service, queries *sqlcgen.Queries, interval time.Duration, topN int, logger zerolog.Logger) *SnapshotWorker {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	if topN <= 0 {
		topN = 50
	}
	return &SnapshotWorker{
		svc:      svc,
		queries:  queries,
		logger:   logger.With().Str("component", "leaderboard_snapshot_worker").Logger(),
		interval: interval,
		topN:     topN,
	}
}

// Run blocks until context cancellation.
func (w *SnapshotWorker) Run(ctx context.Context) error {
	if w.svc == nil || w.queries == nil {
		return nil
	}

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	// run immediately
	w.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

func (w *SnapshotWorker) tick(ctx context.Context) {
	for _, window := range defaultWindows {
		if err := w.snapshotWindow(ctx, window); err != nil {
			w.logger.Warn().Err(err).Str("window", window).Msg("snapshot failed")
		}
	}
}

func (w *SnapshotWorker) snapshotWindow(ctx context.Context, window string) error {
	entries, err := w.svc.Top(ctx, window, w.topN)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}

	wsEntries := toWSEntries(entries)
	data, err := json.Marshal(wsEntries)
	if err != nil {
		return err
	}

	sourceHash := sha256.Sum256(data)
	now := time.Now().UTC()

	params := sqlcgen.InsertLeaderboardSnapshotParams{
		TimeWindow: window,
		GeneratedAt: pgtype.Timestamptz{
			Time:  now,
			Valid: true,
		},
		Entries:    data,
		SourceHash: hex.EncodeToString(sourceHash[:]),
	}

	if _, err := w.queries.InsertLeaderboardSnapshot(ctx, params); err != nil {
		return err
	}

	w.logger.Info().
		Str("window", window).
		Int("entries", len(wsEntries)).
		Time("generated_at", now).
		Msg("leaderboard snapshot persisted")

	return nil
}
