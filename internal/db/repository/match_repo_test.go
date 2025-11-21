package repository

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	sqlcgen "github.com/gokatarajesh/quiz-platform/internal/db/sqlc"
)

type mockMatchStore struct {
	mock.Mock
}

func (m *mockMatchStore) CreateMatch(ctx context.Context, arg sqlcgen.CreateMatchParams) (sqlcgen.Match, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(sqlcgen.Match), args.Error(1)
}

func (m *mockMatchStore) UpdateMatchStatus(ctx context.Context, arg sqlcgen.UpdateMatchStatusParams) error {
	return m.Called(ctx, arg).Error(0)
}

func (m *mockMatchStore) CreatePlayerMatchState(ctx context.Context, arg sqlcgen.CreatePlayerMatchStateParams) error {
	return m.Called(ctx, arg).Error(0)
}

func (m *mockMatchStore) UpdatePlayerMatchResult(ctx context.Context, arg sqlcgen.UpdatePlayerMatchResultParams) error {
	return m.Called(ctx, arg).Error(0)
}

func (m *mockMatchStore) GetPlayerStatesByMatch(ctx context.Context, matchID pgtype.UUID) ([]sqlcgen.PlayerMatchState, error) {
	args := m.Called(ctx, matchID)
	return args.Get(0).([]sqlcgen.PlayerMatchState), args.Error(1)
}

func (m *mockMatchStore) GetMatchForSummary(ctx context.Context, matchID pgtype.UUID) (sqlcgen.Match, error) {
	args := m.Called(ctx, matchID)
	return args.Get(0).(sqlcgen.Match), args.Error(1)
}

func TestMatchRepository_Create(t *testing.T) {
	store := new(mockMatchStore)
	repo := NewMatchRepository(store)

	params := sqlcgen.CreateMatchParams{
		Mode:                 "random_1v1",
		QuestionCount:        5,
		PerQuestionSeconds:   15,
		GlobalTimeoutSeconds: 95,
		SeedHash:             "seed",
		LeaderboardEligible:  true,
		Status:               "pending",
	}
	expect := sqlcgen.Match{MatchID: uuidFromByte(1), Mode: "random_1v1"}
	store.On("CreateMatch", mock.Anything, params).Return(expect, nil)

	got, err := repo.Create(context.Background(), params)
	assert.NoError(t, err)
	assert.Equal(t, expect, got)
	store.AssertExpectations(t)
}

func TestMatchRepository_UpdateStatus(t *testing.T) {
	store := new(mockMatchStore)
	repo := NewMatchRepository(store)

	params := sqlcgen.UpdateMatchStatusParams{
		Status:  "active",
		MatchID: uuidFromByte(2),
	}
	store.On("UpdateMatchStatus", mock.Anything, params).Return(nil)

	err := repo.UpdateStatus(context.Background(), params)
	assert.NoError(t, err)
	store.AssertExpectations(t)
}

func TestMatchRepository_PlayerStateOps(t *testing.T) {
	store := new(mockMatchStore)
	repo := NewMatchRepository(store)

	createParams := sqlcgen.CreatePlayerMatchStateParams{
		MatchID: uuidFromByte(3),
		UserID:  uuidFromByte(4),
		Status:  "queued",
	}
	updateParams := sqlcgen.UpdatePlayerMatchResultParams{
		MatchID: createParams.MatchID,
		UserID:  createParams.UserID,
		Status:  "completed",
	}
	expectedStates := []sqlcgen.PlayerMatchState{{MatchID: createParams.MatchID, UserID: createParams.UserID}}

	store.On("CreatePlayerMatchState", mock.Anything, createParams).Return(nil)
	store.On("UpdatePlayerMatchResult", mock.Anything, updateParams).Return(nil)
	store.On("GetPlayerStatesByMatch", mock.Anything, createParams.MatchID).Return(expectedStates, nil)

	assert.NoError(t, repo.UpsertPlayerState(context.Background(), createParams))
	assert.NoError(t, repo.FinalizePlayerState(context.Background(), updateParams))

	states, err := repo.ListPlayerStates(context.Background(), createParams.MatchID)
	assert.NoError(t, err)
	assert.Equal(t, expectedStates, states)
	store.AssertExpectations(t)
}
