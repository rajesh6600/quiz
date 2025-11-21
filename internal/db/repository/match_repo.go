package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	sqlcgen "github.com/gokatarajesh/quiz-platform/internal/db/sqlc"
)

type matchStore interface {
	CreateMatch(ctx context.Context, arg sqlcgen.CreateMatchParams) (sqlcgen.Match, error)
	UpdateMatchStatus(ctx context.Context, arg sqlcgen.UpdateMatchStatusParams) error
	CreatePlayerMatchState(ctx context.Context, arg sqlcgen.CreatePlayerMatchStateParams) error
	UpdatePlayerMatchResult(ctx context.Context, arg sqlcgen.UpdatePlayerMatchResultParams) error
	GetPlayerStatesByMatch(ctx context.Context, matchID pgtype.UUID) ([]sqlcgen.PlayerMatchState, error)
	GetMatchForSummary(ctx context.Context, matchID pgtype.UUID) (sqlcgen.Match, error)
}

// MatchRepository contains DB helpers for matches and player states.
type MatchRepository struct {
	store matchStore
}

// NewMatchRepository constructs a new match repository.
func NewMatchRepository(store matchStore) *MatchRepository {
	return &MatchRepository{store: store}
}

// Create persists a new match row.
func (r *MatchRepository) Create(ctx context.Context, params sqlcgen.CreateMatchParams) (sqlcgen.Match, error) {
	return r.store.CreateMatch(ctx, params)
}

// UpdateStatus transitions a match and timestamps.
func (r *MatchRepository) UpdateStatus(ctx context.Context, params sqlcgen.UpdateMatchStatusParams) error {
	return r.store.UpdateMatchStatus(ctx, params)
}

// UpsertPlayerState creates the initial player state row.
func (r *MatchRepository) UpsertPlayerState(ctx context.Context, params sqlcgen.CreatePlayerMatchStateParams) error {
	return r.store.CreatePlayerMatchState(ctx, params)
}

// FinalizePlayerState updates final score + stats for a player.
func (r *MatchRepository) FinalizePlayerState(ctx context.Context, params sqlcgen.UpdatePlayerMatchResultParams) error {
	return r.store.UpdatePlayerMatchResult(ctx, params)
}

// ListPlayerStates returns all players for a match.
func (r *MatchRepository) ListPlayerStates(ctx context.Context, matchID pgtype.UUID) ([]sqlcgen.PlayerMatchState, error) {
	return r.store.GetPlayerStatesByMatch(ctx, matchID)
}

// GetSummary fetches match metadata for leaderboard / reporting.
func (r *MatchRepository) GetSummary(ctx context.Context, matchID uuid.UUID) (sqlcgen.Match, error) {
	var pgMatchID pgtype.UUID
	if err := pgMatchID.Scan(matchID); err != nil {
		return sqlcgen.Match{}, err
	}
	return r.store.GetMatchForSummary(ctx, pgMatchID)
}
