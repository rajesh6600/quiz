package match

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// StateManager handles ephemeral match state in Redis with atomic locks.
type StateManager struct {
	redis  *redis.Client
	logger zerolog.Logger
}

// NewStateManager creates a state manager backed by Redis.
func NewStateManager(redis *redis.Client, logger zerolog.Logger) *StateManager {
	return &StateManager{
		redis:  redis,
		logger: logger,
	}
}

// LockMatch acquires a distributed lock for match state transitions.
// Returns unlock function and error. Lock expires after 30s.
func (s *StateManager) LockMatch(ctx context.Context, matchID uuid.UUID) (func() error, error) {
	key := fmt.Sprintf("match:lock:%s", matchID.String())
	lockValue := uuid.New().String()

	acquired, err := s.redis.SetNX(ctx, key, lockValue, 30*time.Second).Result()
	if err != nil {
		return nil, fmt.Errorf("acquire lock: %w", err)
	}
	if !acquired {
		return nil, fmt.Errorf("lock already held")
	}

	unlock := func() error {
		// Lua script ensures we only delete our own lock
		script := `
			if redis.call("get", KEYS[1]) == ARGV[1] then
				return redis.call("del", KEYS[1])
			else
				return 0
			end
		`
		return s.redis.Eval(ctx, script, []string{key}, lockValue).Err()
	}

	return unlock, nil
}

// StorePlayerState saves player's current answers and status.
func (s *StateManager) StorePlayerState(ctx context.Context, matchID uuid.UUID, userID uuid.UUID, state PlayerState) error {
	key := fmt.Sprintf("match:player:%s:%s", matchID.String(), userID.String())
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	// TTL matches global timeout + padding
	ttl := 2 * time.Hour // generous for match completion + review
	return s.redis.Set(ctx, key, data, ttl).Err()
}

// GetPlayerState retrieves a player's state.
func (s *StateManager) GetPlayerState(ctx context.Context, matchID uuid.UUID, userID uuid.UUID) (*PlayerState, error) {
	key := fmt.Sprintf("match:player:%s:%s", matchID.String(), userID.String())
	data, err := s.redis.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get state: %w", err)
	}

	var state PlayerState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("unmarshal state: %w", err)
	}
	return &state, nil
}

// GetAllPlayerStates returns all players for a match.
func (s *StateManager) GetAllPlayerStates(ctx context.Context, matchID uuid.UUID) ([]PlayerState, error) {
	pattern := fmt.Sprintf("match:player:%s:*", matchID.String())
	keys, err := s.redis.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("list keys: %w", err)
	}

	var states []PlayerState
	for _, key := range keys {
		data, err := s.redis.Get(ctx, key).Bytes()
		if err != nil {
			s.logger.Warn().Err(err).Str("key", key).Msg("skip corrupted player state")
			continue
		}

		var state PlayerState
		if err := json.Unmarshal(data, &state); err != nil {
			s.logger.Warn().Err(err).Str("key", key).Msg("skip unmarshal error")
			continue
		}
		states = append(states, state)
	}

	return states, nil
}

// StoreMatchQuestions caches the question pack for a match (HMAC-signed tokens included).
func (s *StateManager) StoreMatchQuestions(ctx context.Context, matchID uuid.UUID, questions []QuestionPackItem) error {
	key := fmt.Sprintf("match:questions:%s", matchID.String())
	data, err := json.Marshal(questions)
	if err != nil {
		return fmt.Errorf("marshal questions: %w", err)
	}

	ttl := 2 * time.Hour
	return s.redis.Set(ctx, key, data, ttl).Err()
}

// GetMatchQuestions retrieves the question pack.
func (s *StateManager) GetMatchQuestions(ctx context.Context, matchID uuid.UUID) ([]QuestionPackItem, error) {
	key := fmt.Sprintf("match:questions:%s", matchID.String())
	data, err := s.redis.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get questions: %w", err)
	}

	var questions []QuestionPackItem
	if err := json.Unmarshal(data, &questions); err != nil {
		return nil, fmt.Errorf("unmarshal questions: %w", err)
	}
	return questions, nil
}

// QuestionPackItem represents a question with its signed token.
type QuestionPackItem struct {
	Order         int      `json:"order"`
	ID            string   `json:"id"`
	Prompt        string   `json:"prompt"`
	Type          string   `json:"type"`
	Options       []string `json:"options"`
	Token         string   `json:"token"` // HMAC-signed
	Difficulty    string   `json:"difficulty"`
	CorrectAnswer string   `json:"correct_answer"` // server-side only
}
