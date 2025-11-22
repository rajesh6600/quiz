package repository

import (
	"context"

	"github.com/gokatarajesh/quiz-platform/internal/db/sqlc"
)

type questionStore interface {
	GetQuestionPool(ctx context.Context, limit int32) ([]sqlcgen.Question, error)
	InsertQuestion(ctx context.Context, arg sqlcgen.InsertQuestionParams) (sqlcgen.Question, error)
}

// QuestionRepository wraps sqlc queries for curated question access.
type QuestionRepository struct {
	store questionStore
}

func NewQuestionRepository(store questionStore) *QuestionRepository {
	return &QuestionRepository{store: store}
}

// FetchPool retrieves verified questions (no filters - type/difficulty/category removed).
func (r *QuestionRepository) FetchPool(ctx context.Context, limit int32) ([]sqlcgen.Question, error) {
	return r.store.GetQuestionPool(ctx, limit)
}

// Insert stores newly verified questions (e.g., from AI fallback) into Postgres.
func (r *QuestionRepository) Insert(ctx context.Context, params sqlcgen.InsertQuestionParams) (sqlcgen.Question, error) {
	return r.store.InsertQuestion(ctx, params)
}
