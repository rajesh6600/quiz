package question

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"testing"
	"time"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"

	"github.com/gokatarajesh/quiz-platform/internal/db/repository"
	sqlcgen "github.com/gokatarajesh/quiz-platform/internal/db/sqlc"
	"github.com/gokatarajesh/quiz-platform/internal/question/external"
)

type stubQuestionStore struct {
	fetch func(ctx context.Context, params sqlcgen.GetQuestionPoolParams) ([]sqlcgen.Question, error)
}

func (s *stubQuestionStore) GetQuestionPool(ctx context.Context, params sqlcgen.GetQuestionPoolParams) ([]sqlcgen.Question, error) {
	return s.fetch(ctx, params)
}

func (s *stubQuestionStore) InsertQuestion(ctx context.Context, params sqlcgen.InsertQuestionParams) (sqlcgen.Question, error) {
	return sqlcgen.Question{}, errors.New("not implemented")
}

type memoryCache struct {
	store map[string]PackResponse
}

func newMemoryCache() *memoryCache {
	return &memoryCache{store: map[string]PackResponse{}}
}

func (c *memoryCache) key(req PackRequest) string {
	var diffParts []string
	for k, v := range req.DifficultyCounts {
		diffParts = append(diffParts, fmt.Sprintf("%s:%d", k, v))
	}
	sort.Strings(diffParts)
	return strings.Join([]string{
		"mem",
		req.Category,
		req.Seed,
		fmt.Sprint(req.TotalQuestions),
		strings.Join(diffParts, "|"),
	}, ":")
}

func (c *memoryCache) Get(_ context.Context, req PackRequest) (*PackResponse, error) {
	if val, ok := c.store[c.key(req)]; ok {
		return &val, nil
	}
	return nil, nil
}

func (c *memoryCache) Set(_ context.Context, req PackRequest, resp PackResponse) error {
	c.store[c.key(req)] = resp
	return nil
}

type stubOpentdb struct {
	questions []external.OpenTDBQuestion
}

func (s *stubOpentdb) Fetch(_ context.Context, amount int, difficulty, qType string) ([]external.OpenTDBQuestion, error) {
	return s.questions[:min(amount, len(s.questions))], nil
}

type stubTrivia struct {
	questions []external.TriviaAPIQuestion
}

func (s *stubTrivia) Fetch(_ context.Context, amount int, category, difficulty string) ([]external.TriviaAPIQuestion, error) {
	return s.questions[:min(amount, len(s.questions))], nil
}

type stubAI struct {
	mu        sync.Mutex
	generated []Question
	enqueue   int
}

func (s *stubAI) GeneratePack(ctx context.Context, req AIGenerateRequest) ([]Question, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.generated[:min(req.Count, len(s.generated))], nil
}

func (s *stubAI) EnqueuePack(ctx context.Context, req AIGenerateRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enqueue++
	return nil
}
func (s *stubAI) EnqueueCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enqueue
}
func TestFetchPackUsesCache(t *testing.T) {
	repo := repository.NewQuestionRepository(&stubQuestionStore{
		fetch: func(ctx context.Context, params sqlcgen.GetQuestionPoolParams) ([]sqlcgen.Question, error) {
			return []sqlcgen.Question{sqlQuestion("curated-1", DifficultyEasy)}, nil
		},
	})
	cache := newMemoryCache()
	ai := &stubAI{}
	service := NewService(repo, cache, nil, nil, ai, ServiceOptions{HMACSecret: []byte("secret")})

	req := PackRequest{
		Category: "general",
		DifficultyCounts: map[string]int{
			DifficultyEasy: 1,
		},
		TotalQuestions: 1,
		Seed:           "seed",
	}

	resp, err := service.FetchPack(context.Background(), req)
	assert.NoError(t, err)
	assert.Len(t, resp.Questions, 1)

	_, err = service.FetchPack(context.Background(), req)
	assert.NoError(t, err)
	assert.Len(t, cache.store, 1, "cache should store one entry")
}

func TestFetchPackFallsBackToExternalAndAI(t *testing.T) {
	repo := repository.NewQuestionRepository(&stubQuestionStore{
		fetch: func(ctx context.Context, params sqlcgen.GetQuestionPoolParams) ([]sqlcgen.Question, error) {
			return nil, nil
		},
	})
	cache := newMemoryCache()

	opentdb := &stubOpentdb{
		questions: []external.OpenTDBQuestion{
			{Question: "OT Question", CorrectAnswer: "A", IncorrectAnswer: []string{"B", "C", "D"}, Difficulty: DifficultyEasy, Category: "general", Type: "multiple"},
		},
	}
	ai := &stubAI{
		generated: []Question{
			{ID: "ai-1", Prompt: "AI Question", Options: []string{"T", "F"}, Answer: "T", Difficulty: DifficultyEasy, Category: "general"},
		},
	}

	service := NewService(repo, cache, opentdb, nil, ai, ServiceOptions{HMACSecret: []byte("secret")})
	req := PackRequest{
		Category: "general",
		DifficultyCounts: map[string]int{
			DifficultyEasy: 2,
		},
		TotalQuestions: 2,
		Seed:           "seed",
	}

	resp, err := service.FetchPack(context.Background(), req)
	assert.NoError(t, err)
	assert.Len(t, resp.Questions, 2)
	assert.Equal(t, "opentdb", resp.Questions[0].Source)
	assert.Equal(t, "ai-1", resp.Questions[1].ID)
}

func TestFetcherWorkerEnqueueAIOnFailure(t *testing.T) {
	repo := repository.NewQuestionRepository(&stubQuestionStore{
		fetch: func(ctx context.Context, params sqlcgen.GetQuestionPoolParams) ([]sqlcgen.Question, error) {
			return nil, errors.New("db down")
		},
	})
	cache := newMemoryCache()
	ai := &stubAI{
		generated: []Question{{ID: "ai"}},
	}
	service := NewService(repo, cache, nil, nil, ai, ServiceOptions{HMACSecret: []byte("secret")})

	queue := make(chan PackRequest, 1)
	queue <- PackRequest{
		Category: "general",
		DifficultyCounts: map[string]int{
			DifficultyEasy: 1,
		},
		TotalQuestions: 1,
		Seed:           "seed",
	}

	logger := zerolog.New(io.Discard)
	worker := NewFetcherWorker(service, ai, queue, logger, time.Millisecond*10)

	go func() {
		worker.Run()
	}()

	time.Sleep(20 * time.Millisecond)
	worker.Stop()

	assert.Greater(t, ai.EnqueueCount(), 0, "AI enqueue should be invoked on failure")
}

func sqlQuestion(id string, difficulty string) sqlcgen.Question {
	uid := uuid.New()
	var pgUUID pgtype.UUID
	copy(pgUUID.Bytes[:], uid[:])
	pgUUID.Valid = true
	return sqlcgen.Question{
		QuestionID:    pgUUID,
		Type:          TypeMCQ,
		Prompt:        "Prompt " + id,
		Options:       []string{"A", "B", "C", "D"},
		CorrectAnswer: "A",
		Difficulty:    difficulty,
		Category:      "general",
		Source:        "curated",
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
