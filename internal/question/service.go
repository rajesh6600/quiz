package question

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/gokatarajesh/quiz-platform/internal/db/repository"
	sqlcgen "github.com/gokatarajesh/quiz-platform/internal/db/sqlc"
	"github.com/gokatarajesh/quiz-platform/internal/question/external"
)

// PackCache defines cache behavior (implemented by Redis-backed Cache).
type PackCache interface {
	Get(ctx context.Context, req PackRequest) (*PackResponse, error)
	Set(ctx context.Context, req PackRequest, resp PackResponse) error
}

// AIGenerator produces fallback questions (requires env AI_GENERATOR_API_KEY).
type AIGenerator interface {
	GeneratePack(ctx context.Context, req AIGenerateRequest) ([]Question, error)
	EnqueuePack(ctx context.Context, req AIGenerateRequest) error
}

type AIGenerateRequest struct {
	Category   string
	Difficulty string
	Count      int
	Seed       string
}

// Service orchestrates access to curated DB, external APIs, and AI fallback.
type opentdbProvider interface {
	Fetch(ctx context.Context, amount int, difficulty, qType string) ([]external.OpenTDBQuestion, error)
}

type triviaProvider interface {
	Fetch(ctx context.Context, amount int, category, difficulty string) ([]external.TriviaAPIQuestion, error)
}

type Service struct {
	repo      *repository.QuestionRepository
	cache     PackCache
	opentdb   opentdbProvider
	triviaAPI triviaProvider
	ai        AIGenerator
	hmacKey   []byte
}

type ServiceOptions struct {
	HMACSecret []byte
}

func NewService(repo *repository.QuestionRepository, cache PackCache, opentdb opentdbProvider, trivia triviaProvider, ai AIGenerator, opts ServiceOptions) *Service {
	return &Service{
		repo:      repo,
		cache:     cache,
		opentdb:   opentdb,
		triviaAPI: trivia,
		ai:        ai,
		hmacKey:   opts.HMACSecret,
	}
}

// FetchPack returns a question set, respecting the priority: curated DB -> external APIs -> AI.
func (s *Service) FetchPack(ctx context.Context, req PackRequest) (PackResponse, error) {
	if cached, err := s.cache.Get(ctx, req); err == nil && cached != nil {
		return *cached, nil
	}

	var result []Question
	for diff, count := range req.DifficultyCounts {
		chunk := make([]Question, 0, count)

		curated, err := s.fetchCurated(ctx, req.Category, diff, count)
		if err != nil {
			return PackResponse{}, err
		}
		chunk = append(chunk, curated...)

		if len(chunk) < count {
			if externalQs, err := s.fetchExternal(ctx, diff, req.Category, count-len(chunk)); err == nil {
				chunk = append(chunk, externalQs...)
			}
		}

		if len(chunk) < count {
			aiQs, err := s.fetchAI(ctx, diff, req.Category, count-len(chunk), req.Seed)
			if err != nil {
				return PackResponse{}, fmt.Errorf("ai fallback failed: %w", err)
			}
			chunk = append(chunk, aiQs...)
		}

		if len(chunk) < count {
			return PackResponse{}, fmt.Errorf("insufficient %s questions: need %d got %d", diff, count, len(chunk))
		}

		result = append(result, chunk[:count]...)
	}

	if len(result) < req.TotalQuestions {
		return PackResponse{}, fmt.Errorf("insufficient questions: need %d got %d", req.TotalQuestions, len(result))
	}
	result = result[:req.TotalQuestions]

	resp := PackResponse{
		Questions: result,
		Seed:      req.Seed,
		ExpiresAt: time.Now().Add(10 * time.Minute).Unix(),
	}

	// Redis is tuned with maxmemory-policy=allkeys-lru so saving the pack respects LRU eviction.
	_ = s.cache.Set(ctx, req, resp)

	return resp, nil
}

func (s *Service) fetchCurated(ctx context.Context, category, difficulty string, limit int) ([]Question, error) {
	params := sqlcgen.GetQuestionPoolParams{
		Limit:        int32(limit),
		Difficulties: []string{difficulty},
		Categories:   []string{category},
	}
	rows, err := s.repo.FetchPool(ctx, params)
	if err != nil {
		return nil, err
	}
	var qs []Question
	for _, row := range rows {
		qs = append(qs, s.toDomain(row))
	}
	return qs, nil
}

func (s *Service) fetchExternal(ctx context.Context, difficulty, category string, limit int) ([]Question, error) {
	var combined []Question
	if s.opentdb != nil {
		if ot, err := s.opentdb.Fetch(ctx, limit, difficulty, "multiple"); err == nil {
			for _, q := range ot {
				combined = append(combined, normalizeOpenTDB(q, s.hmacKey))
				if len(combined) >= limit {
					return combined[:limit], nil
				}
			}
		}
	}
	if s.triviaAPI != nil && len(combined) < limit {
		if tv, err := s.triviaAPI.Fetch(ctx, limit-len(combined), category, difficulty); err == nil {
			for _, q := range tv {
				combined = append(combined, normalizeTriviaAPI(q, s.hmacKey))
				if len(combined) >= limit {
					break
				}
			}
		}
	}
	return combined, nil
}

func (s *Service) fetchAI(ctx context.Context, difficulty, category string, limit int, seed string) ([]Question, error) {
	if s.ai == nil {
		return nil, fmt.Errorf("ai generator unavailable")
	}
	return s.ai.GeneratePack(ctx, AIGenerateRequest{
		Category:   category,
		Difficulty: difficulty,
		Count:      limit,
		Seed:       seed,
	})
}

func (s *Service) toDomain(row sqlcgen.Question) Question {
	id := uuidFrom(row.QuestionID)
	return Question{
		ID:         id,
		Type:       row.Type,
		Prompt:     row.Prompt,
		Options:    row.Options,
		Answer:     row.CorrectAnswer,
		Difficulty: row.Difficulty,
		Category:   row.Category,
		Source:     row.Source,
		Token:      s.signToken(id),
	}
}

func normalizeOpenTDB(q external.OpenTDBQuestion, key []byte) Question {
	options := append(q.IncorrectAnswer, q.CorrectAnswer)
	return Question{
		ID:         uuid.NewString(),
		Type:       adaptType(q.Type),
		Prompt:     q.Question,
		Options:    options,
		Answer:     q.CorrectAnswer,
		Difficulty: q.Difficulty,
		Category:   q.Category,
		Source:     "opentdb",
		Token:      signTempToken(key, q.Question),
	}
}

func normalizeTriviaAPI(q external.TriviaAPIQuestion, key []byte) Question {
	options := append(q.Incorrect, q.Correct)
	return Question{
		ID:         q.ID,
		Type:       adaptType(q.Type),
		Prompt:     q.Question,
		Options:    options,
		Answer:     q.Correct,
		Difficulty: q.Difficulty,
		Category:   q.Category,
		Source:     "triviaapi",
		Token:      signTempToken(key, q.ID),
	}
}

func adaptType(t string) string {
	if t == "boolean" {
		return TypeTrueFalse
	}
	return TypeMCQ
}

func (s *Service) signToken(questionID string) string {
	if len(s.hmacKey) == 0 {
		return questionID
	}
	mac := hmac.New(sha256.New, s.hmacKey)
	mac.Write([]byte(questionID))
	return hex.EncodeToString(mac.Sum(nil))
}

func signTempToken(key []byte, payload string) string {
	if len(key) == 0 {
		return payload
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func uuidFrom(id pgtype.UUID) string {
	u, err := uuid.FromBytes(id.Bytes[:])
	if err != nil {
		return uuid.NewString()
	}
	return u.String()
}
