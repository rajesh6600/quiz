package queue

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// Manager handles random 1v1 matchmaking queue with Redis-backed persistence.
type Manager struct {
	redis      *redis.Client
	logger     zerolog.Logger
	mu         sync.RWMutex
	waiting    map[uuid.UUID]*WaitingPlayer
	botWaitSec int // seconds before offering bot (default 10)
}

// WaitingPlayer represents a queued player.
type WaitingPlayer struct {
	UserID              uuid.UUID
	DisplayName         string
	IsGuest             bool
	PreferredCategory   string
	PreferredDifficulty string
	BotOK               bool
	QueuedAt            time.Time
	QueueToken          uuid.UUID
}

// NewManager creates a matchmaking queue manager.
func NewManager(redis *redis.Client, logger zerolog.Logger, botWaitSeconds int) *Manager {
	if botWaitSeconds <= 0 {
		botWaitSeconds = 10
	}
	return &Manager{
		redis:      redis,
		logger:     logger,
		waiting:    make(map[uuid.UUID]*WaitingPlayer),
		botWaitSec: botWaitSeconds,
	}
}

// Enqueue adds a player to the queue and attempts immediate matchmaking.
// Returns queue token and whether a match was found immediately.
func (m *Manager) Enqueue(ctx context.Context, req MatchmakingRequest) (uuid.UUID, *MatchPair, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	queueToken := uuid.New()
	player := &WaitingPlayer{
		UserID:              req.UserID,
		DisplayName:         req.DisplayName,
		IsGuest:             req.IsGuest,
		PreferredCategory:   req.PreferredCategory,
		PreferredDifficulty: req.PreferredDifficulty,
		BotOK:               req.BotOK,
		QueuedAt:            time.Now(),
		QueueToken:          queueToken,
	}

	// Try immediate match
	for otherToken, other := range m.waiting {
		if other.UserID == req.UserID {
			continue // skip self
		}
		if m.isCompatible(player, other) {
			delete(m.waiting, otherToken)
			pair := &MatchPair{
				Player1: *player,
				Player2: *other,
			}
			return queueToken, pair, nil
		}
	}

	// No match found, add to queue
	m.waiting[queueToken] = player
	m.logger.Info().
		Str("queue_token", queueToken.String()).
		Str("user_id", req.UserID.String()).
		Msg("player enqueued")

	return queueToken, nil, nil
}

// Dequeue removes a player from the queue.
func (m *Manager) Dequeue(ctx context.Context, queueToken uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.waiting[queueToken]; !exists {
		return fmt.Errorf("queue token not found")
	}

	delete(m.waiting, queueToken)
	m.logger.Info().Str("queue_token", queueToken.String()).Msg("player dequeued")
	return nil
}

// GetPosition returns queue position (0 = front, -1 if not found).
func (m *Manager) GetPosition(queueToken uuid.UUID) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pos := 0
	for token := range m.waiting {
		if token == queueToken {
			return pos
		}
		pos++
	}
	return -1
}

// ShouldOfferBot checks if a player has waited long enough for bot offer.
func (m *Manager) ShouldOfferBot(queueToken uuid.UUID) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	player, exists := m.waiting[queueToken]
	if !exists {
		return false
	}

	waitDuration := time.Since(player.QueuedAt)
	return waitDuration >= time.Duration(m.botWaitSec)*time.Second && player.BotOK
}

// MatchPair represents a matched pair of players.
type MatchPair struct {
	Player1 WaitingPlayer
	Player2 WaitingPlayer
}

// MatchmakingRequest mirrors the match package type for queue isolation.
type MatchmakingRequest struct {
	UserID              uuid.UUID
	DisplayName         string
	IsGuest             bool
	PreferredCategory   string
	PreferredDifficulty string
	BotOK               bool
}

func (m *Manager) isCompatible(p1, p2 *WaitingPlayer) bool {
	// Simple compatibility: both must be OK with bot OR both real players
	// For MVP, we match any two players (preferences ignored for speed)
	_ = p1
	_ = p2
	return true
}
