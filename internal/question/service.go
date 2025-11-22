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
	"github.com/redis/go-redis/v9"

	"github.com/gokatarajesh/quiz-platform/internal/db/repository"
	sqlcgen "github.com/gokatarajesh/quiz-platform/internal/db/sqlc"
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
	Category         string
	Difficulty       string // Legacy/Fallback
	Count            int
	Seed             string
	DifficultyCounts map[string]int
}

type Service struct {
	repo    *repository.QuestionRepository
	cache   PackCache
	ai      AIGenerator
	redis   *redis.Client
	hmacKey []byte
}

type ServiceOptions struct {
	HMACSecret []byte
	Redis      *redis.Client
}

func NewService(repo *repository.QuestionRepository, cache PackCache, ai AIGenerator, opts ServiceOptions) *Service {
	return &Service{
		repo:    repo,
		cache:   cache,
		ai:      ai,
		redis:   opts.Redis,
		hmacKey: opts.HMACSecret,
	}
}

// FetchPack returns a question set, respecting the priority: curated DB -> AI.
// External APIs (OpenTDB, TriviaAPI) have been removed in favor of a single-call AI optimization.
func (s *Service) FetchPack(ctx context.Context, req PackRequest) (PackResponse, error) {
	if cached, err := s.cache.Get(ctx, req); err == nil && cached != nil {
		return *cached, nil
	}

	result := make([]Question, 0, req.TotalQuestions)
	remainingNeeds := make(map[string]int)
	
	// 1. Fetch Curated Questions
	// We still fetch curated questions per difficulty because the DB query is optimized for that.
	// This is fast enough to keep sequential or we could parallelize if needed.
	for diff, count := range req.DifficultyCounts {
		curated, err := s.fetchCurated(ctx, req.Category, diff, count)
		if err != nil {
			return PackResponse{}, err
		}
		result = append(result, curated...)
		
		needed := count - len(curated)
		if needed > 0 {
			remainingNeeds[diff] = needed
		}
	}

	// 2. AI Fallback (Single Call Optimization)
	totalNeeded := 0
	for _, count := range remainingNeeds {
		totalNeeded += count
	}

	if totalNeeded > 0 {
		aiQs, err := s.fetchAIMixed(ctx, req.Category, remainingNeeds, req.Seed)
		if err != nil {
			return PackResponse{}, fmt.Errorf("ai fallback failed: %w", err)
		}
		
		// Distribute AI questions to results
		// Note: AI might return slightly different distribution, we take what we get
		result = append(result, aiQs...)
	}

	// 3. Check for within-match duplicates (Level 1 - Mandatory for all matches)
	questionIDs := make([]string, len(result))
	for i, q := range result {
		questionIDs[i] = q.ID
	}
	
	uniqueIDs, hasDuplicates := checkWithinMatchDuplicates(questionIDs)
	if hasDuplicates {
		// Filter out duplicates and regenerate if needed
		uniqueMap := make(map[string]bool)
		for _, id := range uniqueIDs {
			uniqueMap[id] = true
		}
		
		filtered := make([]Question, 0, len(uniqueIDs))
		for _, q := range result {
			if uniqueMap[q.ID] {
				filtered = append(filtered, q)
			}
		}
		
		// If we lost questions due to duplicates, regenerate
		needed := req.TotalQuestions - len(filtered)
		if needed > 0 {
			// Regenerate additional questions to fill the gap
			// Use same difficulty distribution proportionally
			regenerateNeeds := make(map[string]int)
			for diff, count := range req.DifficultyCounts {
				// Calculate proportional need
				proportion := float64(count) / float64(req.TotalQuestions)
				regenerateNeeds[diff] = int(float64(needed) * proportion)
			}
			
			// Ensure we have at least one question per difficulty if original had it
			for diff, origCount := range req.DifficultyCounts {
				if origCount > 0 && regenerateNeeds[diff] == 0 {
					regenerateNeeds[diff] = 1
				}
			}
			
			additionalQs, err := s.fetchAIMixed(ctx, req.Category, regenerateNeeds, fmt.Sprintf("%s-retry", req.Seed))
			if err == nil {
				// Check new questions for duplicates with existing
				existingIDMap := make(map[string]bool)
				for _, q := range filtered {
					existingIDMap[q.ID] = true
				}
				
				for _, q := range additionalQs {
					if !existingIDMap[q.ID] && len(filtered) < req.TotalQuestions {
						filtered = append(filtered, q)
						existingIDMap[q.ID] = true
					}
				}
			}
		}
		
		result = filtered
	}

	// 4. Final Validation
	if len(result) < req.TotalQuestions {
		return PackResponse{}, fmt.Errorf("insufficient questions: need %d got %d", req.TotalQuestions, len(result))
	}
	
	// Trim if we somehow got too many (unlikely with this logic but safe)
	if len(result) > req.TotalQuestions {
		result = result[:req.TotalQuestions]
	}

	// 5. Cross-match uniqueness check (Level 2 - Only for 1v1 matches)
	// OPTIMIZED: Check both players using batch pipeline for fairness and performance
	if req.MatchMode == "random_1v1" && len(req.UserIDs) >= 2 && req.UserIDs[0] != nil && req.UserIDs[1] != nil {
		player1ID := *req.UserIDs[0]
		player2ID := *req.UserIDs[1]

		finalIDs := make([]string, len(result))
		for i, q := range result {
			finalIDs[i] = q.ID
		}

		// Batch check both players' histories (1 Redis round-trip)
		unseenByBoth, dup1Count, dup2Count := s.checkBothPlayersHistory(ctx, player1ID, player2ID, finalIDs)
		maxDup := dup1Count
		if dup2Count > maxDup {
			maxDup = dup2Count
		}

		// Allow 1-3 duplicates from history (based on max of both players)
		if maxDup > 3 {
			// Too many duplicates - filter to questions unseen by both players
			unseenMap := make(map[string]bool)
			for _, id := range unseenByBoth {
				unseenMap[id] = true
			}

			filtered := make([]Question, 0, len(unseenByBoth))
			seenInMatch := make(map[string]bool) // Track what we've already added
			for _, q := range result {
				if unseenMap[q.ID] && !seenInMatch[q.ID] {
					filtered = append(filtered, q)
					seenInMatch[q.ID] = true
				}
			}

			// Regenerate to replace duplicates
			needed := req.TotalQuestions - len(filtered)
			if needed > 0 {
				regenerateNeeds := make(map[string]int)
				for diff, count := range req.DifficultyCounts {
					proportion := float64(count) / float64(req.TotalQuestions)
					regenerateNeeds[diff] = int(float64(needed) * proportion)
				}

				// Ensure minimum 1 per difficulty if original had it
				for diff, origCount := range req.DifficultyCounts {
					if origCount > 0 && regenerateNeeds[diff] == 0 {
						regenerateNeeds[diff] = 1
					}
				}

				additionalQs, err := s.fetchAIMixed(ctx, req.Category, regenerateNeeds, fmt.Sprintf("%s-unique", req.Seed))
				if err == nil {
					// Fast in-memory validation: check against both players AND current match
					for _, q := range additionalQs {
						// Check if unseen by both players (quick check)
						unseen, _, _ := s.checkBothPlayersHistory(ctx, player1ID, player2ID, []string{q.ID})
						if len(unseen) > 0 && !seenInMatch[q.ID] && len(filtered) < req.TotalQuestions {
							filtered = append(filtered, q)
							seenInMatch[q.ID] = true
						}
					}
				}

				result = filtered
			}
		} else if maxDup > 0 {
			// 1-3 duplicates allowed - filter to unseen by both, but allow some duplicates if needed
			unseenMap := make(map[string]bool)
			for _, id := range unseenByBoth {
				unseenMap[id] = true
			}

			filtered := make([]Question, 0, len(unseenByBoth))
			for _, q := range result {
				if unseenMap[q.ID] {
					filtered = append(filtered, q)
				}
			}

			// Fill remaining slots if needed (allow some duplicates)
			needed := req.TotalQuestions - len(filtered)
			if needed > 0 && needed <= maxDup {
				// Add back some duplicates to reach required count
				duplicateMap := make(map[string]Question)
				for _, q := range result {
					if !unseenMap[q.ID] {
						duplicateMap[q.ID] = q
					}
				}

				for _, q := range duplicateMap {
					if len(filtered) < req.TotalQuestions {
						filtered = append(filtered, q)
					}
				}
			}

			result = filtered
		}

		// Ensure we have enough questions
		if len(result) < req.TotalQuestions {
			return PackResponse{}, fmt.Errorf("insufficient unique questions after filtering: need %d got %d", req.TotalQuestions, len(result))
		}

		if len(result) > req.TotalQuestions {
			result = result[:req.TotalQuestions]
		}
	} else if req.MatchMode == "random_1v1" && req.UserID != nil && *req.UserID != uuid.Nil {
		// Fallback: Support old single UserID for backward compatibility
		finalIDs := make([]string, len(result))
		for i, q := range result {
			finalIDs[i] = q.ID
		}

		unseenIDs, duplicateCount := s.checkUserQuestionHistory(ctx, *req.UserID, finalIDs)

		// Allow 1-3 duplicates from history
		if duplicateCount > 3 {
			// Too many duplicates - filter them out and regenerate
			unseenMap := make(map[string]bool)
			for _, id := range unseenIDs {
				unseenMap[id] = true
			}

			filtered := make([]Question, 0, len(unseenIDs))
			for _, q := range result {
				if unseenMap[q.ID] {
					filtered = append(filtered, q)
				}
			}

			// Regenerate to replace duplicates
			needed := req.TotalQuestions - len(filtered)
			if needed > 0 {
				regenerateNeeds := make(map[string]int)
				for diff, count := range req.DifficultyCounts {
					proportion := float64(count) / float64(req.TotalQuestions)
					regenerateNeeds[diff] = int(float64(needed) * proportion)
				}

				additionalQs, err := s.fetchAIMixed(ctx, req.Category, regenerateNeeds, fmt.Sprintf("%s-unique", req.Seed))
				if err == nil {
					existingIDMap := make(map[string]bool)
					for _, q := range filtered {
						existingIDMap[q.ID] = true
					}

					// Also check against user history for new questions
					for _, q := range additionalQs {
						unseen, _ := s.checkUserQuestionHistory(ctx, *req.UserID, []string{q.ID})
						if len(unseen) > 0 && !existingIDMap[q.ID] && len(filtered) < req.TotalQuestions {
							filtered = append(filtered, q)
							existingIDMap[q.ID] = true
						}
					}
				}

				result = filtered
			}
		} else if duplicateCount > 0 {
			// 1-3 duplicates allowed - filter them but keep rest
			unseenMap := make(map[string]bool)
			for _, id := range unseenIDs {
				unseenMap[id] = true
			}

			filtered := make([]Question, 0, len(unseenIDs))
			for _, q := range result {
				if unseenMap[q.ID] {
					filtered = append(filtered, q)
				}
			}

			// Fill remaining slots if needed (allow some duplicates)
			needed := req.TotalQuestions - len(filtered)
			if needed > 0 && needed <= duplicateCount {
				// Add back some duplicates to reach required count
				duplicateMap := make(map[string]Question)
				for _, q := range result {
					if !unseenMap[q.ID] {
						duplicateMap[q.ID] = q
					}
				}

				for _, q := range duplicateMap {
					if len(filtered) < req.TotalQuestions {
						filtered = append(filtered, q)
					}
				}
			}

			result = filtered
		}

		// Ensure we have enough questions
		if len(result) < req.TotalQuestions {
			return PackResponse{}, fmt.Errorf("insufficient unique questions after filtering: need %d got %d", req.TotalQuestions, len(result))
		}

		if len(result) > req.TotalQuestions {
			result = result[:req.TotalQuestions]
		}
	}

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
	// Note: category and difficulty parameters are ignored - columns removed from DB
	rows, err := s.repo.FetchPool(ctx, int32(limit))
	if err != nil {
		return nil, err
	}
	var qs []Question
	for _, row := range rows {
		qs = append(qs, s.toDomain(row))
	}
	return qs, nil
}

func (s *Service) fetchAIMixed(ctx context.Context, category string, needs map[string]int, seed string) ([]Question, error) {
	if s.ai == nil {
		return nil, fmt.Errorf("ai generator unavailable")
	}
	
	total := 0
	for _, c := range needs {
		total += c
	}

	return s.ai.GeneratePack(ctx, AIGenerateRequest{
		Category:         category,
		Count:            total,
		Seed:             seed,
		DifficultyCounts: needs,
	})
}

func (s *Service) toDomain(row sqlcgen.Question) Question {
	id := uuidFrom(row.QuestionID)
	return Question{
		ID:      id,
		Prompt:  row.Prompt,
		Options: row.Options,
		Answer:  row.CorrectAnswer,
		Source:  row.Source,
		Token:   s.signToken(id),
		// Type, Difficulty, Category from DB are ignored - not needed
	}
}

func adaptType(t string) string {
	// Only MCQ questions are supported
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

func uuidFrom(id pgtype.UUID) string {
	u, err := uuid.FromBytes(id.Bytes[:])
	if err != nil {
		return uuid.NewString()
	}
	return u.String()
}

// checkWithinMatchDuplicates checks for duplicate question IDs within a batch.
// Returns unique IDs and a flag indicating if duplicates were found.
func checkWithinMatchDuplicates(questionIDs []string) ([]string, bool) {
	seen := make(map[string]bool)
	unique := make([]string, 0, len(questionIDs))
	hasDuplicates := false

	for _, id := range questionIDs {
		if seen[id] {
			hasDuplicates = true
			continue
		}
		seen[id] = true
		unique = append(unique, id)
	}

	return unique, hasDuplicates
}

// checkUserQuestionHistory checks question IDs against user's history in Redis.
// Returns unseen IDs and count of duplicates.
// Only used for 1v1 matches.
func (s *Service) checkUserQuestionHistory(ctx context.Context, userID uuid.UUID, questionIDs []string) ([]string, int) {
	if s.redis == nil || userID == uuid.Nil {
		// No Redis or invalid user - return all as unseen
		return questionIDs, 0
	}

	key := fmt.Sprintf("user:questions:%s", userID.String())
	unseen := make([]string, 0, len(questionIDs))
	duplicateCount := 0

	for _, id := range questionIDs {
		exists, err := s.redis.SIsMember(ctx, key, id).Result()
		if err != nil {
			// On error, assume unseen (fail open)
			unseen = append(unseen, id)
			continue
		}
		if exists {
			duplicateCount++
		} else {
			unseen = append(unseen, id)
		}
	}

	return unseen, duplicateCount
}

// checkBothPlayersHistory checks question IDs against both players' histories using Redis pipeline.
// Returns questions unseen by BOTH players and the duplicate counts for each player.
// This is optimized for performance - uses pipeline to batch all Redis calls into 1 round-trip.
func (s *Service) checkBothPlayersHistory(ctx context.Context, player1ID, player2ID uuid.UUID, questionIDs []string) (unseenByBoth []string, dup1Count, dup2Count int) {
	if s.redis == nil || player1ID == uuid.Nil || player2ID == uuid.Nil {
		// No Redis or invalid users - return all as unseen
		return questionIDs, 0, 0
	}

	if len(questionIDs) == 0 {
		return []string{}, 0, 0
	}

	key1 := fmt.Sprintf("user:questions:%s", player1ID.String())
	key2 := fmt.Sprintf("user:questions:%s", player2ID.String())

	// Use pipeline to batch all operations (1 network round-trip instead of N*2)
	pipe := s.redis.Pipeline()
	cmds1 := make([]*redis.BoolCmd, len(questionIDs))
	cmds2 := make([]*redis.BoolCmd, len(questionIDs))

	for i, id := range questionIDs {
		cmds1[i] = pipe.SIsMember(ctx, key1, id)
		cmds2[i] = pipe.SIsMember(ctx, key2, id)
	}

	// Execute all commands at once
	_, err := pipe.Exec(ctx)
	if err != nil {
		// On error, fail open - return all as unseen
		return questionIDs, 0, 0
	}

	// Process results
	unseenByBoth = make([]string, 0, len(questionIDs))
	for i, id := range questionIDs {
		seen1 := cmds1[i].Val()
		seen2 := cmds2[i].Val()

		if seen1 {
			dup1Count++
		}
		if seen2 {
			dup2Count++
		}

		// Only add if unseen by BOTH players
		if !seen1 && !seen2 {
			unseenByBoth = append(unseenByBoth, id)
		}
	}

	return unseenByBoth, dup1Count, dup2Count
}

// AddUserQuestionHistory adds question IDs to user's Redis set with 10-day TTL.
// Only used for 1v1 matches.
func (s *Service) AddUserQuestionHistory(ctx context.Context, userID uuid.UUID, questionIDs []string) error {
	if s.redis == nil || userID == uuid.Nil || len(questionIDs) == 0 {
		return nil
	}

	key := fmt.Sprintf("user:questions:%s", userID.String())
	
	// Add all question IDs to set
	members := make([]interface{}, len(questionIDs))
	for i, id := range questionIDs {
		members[i] = id
	}
	
	if err := s.redis.SAdd(ctx, key, members...).Err(); err != nil {
		return fmt.Errorf("add question history: %w", err)
	}
	
	// Set 10-day TTL
	if err := s.redis.Expire(ctx, key, 10*24*time.Hour).Err(); err != nil {
		return fmt.Errorf("set question history TTL: %w", err)
	}
	
	return nil
}
