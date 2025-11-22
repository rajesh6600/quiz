package match

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// RoomManager handles private room creation, joining, and lifecycle.
type RoomManager struct {
	redis  *redis.Client
	logger zerolog.Logger
	mu     sync.RWMutex
	rooms  map[string]*PrivateRoom
}

// PrivateRoom represents a private match room.
type PrivateRoom struct {
	RoomCode           string
	MatchID            *uuid.UUID // set when match starts
	HostID             uuid.UUID
	MatchName          string
	MaxPlayers         int
	QuestionCount      int
	PerQuestionSeconds int
	Category           string // e.g., "general", "science", "history"
	Players            []RoomPlayer
	Status             string // "waiting", "starting", "active"
	CreatedAt          time.Time
	StartCountdown     int // seconds (3-5)
}

// RoomPlayer in a private room.
type RoomPlayer struct {
	UserID      uuid.UUID
	DisplayName string
	IsGuest     bool
	IsHost      bool
	JoinedAt    time.Time
}

const (
	RoomStatusWaiting  = "waiting"
	RoomStatusStarting = "starting"
	RoomStatusActive   = "active"
)

// NewRoomManager creates a private room manager.
func NewRoomManager(redis *redis.Client, logger zerolog.Logger) *RoomManager {
	return &RoomManager{
		redis:  redis,
		logger: logger,
		rooms:  make(map[string]*PrivateRoom),
	}
}

// CreateRoom generates a unique 6-character code and initializes a room.
func (r *RoomManager) CreateRoom(ctx context.Context, req PrivateRoomRequest) (string, *PrivateRoom, error) {
	code := r.generateRoomCode()
	// Default category to "general" if not provided
	category := req.Category
	if category == "" {
		category = "general"
	}
	
	room := &PrivateRoom{
		RoomCode:           code,
		HostID:             req.HostID,
		MatchName:          req.MatchName,
		MaxPlayers:         req.MaxPlayers,
		QuestionCount:      req.QuestionCount,
		PerQuestionSeconds: req.PerQuestionSeconds,
		Category:           category,
		Players: []RoomPlayer{
			{
				UserID:      req.HostID,
				DisplayName: req.DisplayName,
				IsHost:      true,
				JoinedAt:    time.Now(),
			},
		},
		Status:         RoomStatusWaiting,
		CreatedAt:      time.Now(),
		StartCountdown: 5, // default
	}

	r.mu.Lock()
	r.rooms[code] = room
	r.mu.Unlock()

	// Persist to Redis for multi-instance support
	// For MVP, we'll use in-memory; Redis persistence can be added later
	_ = r.redis

	r.logger.Info().
		Str("room_code", code).
		Str("host_id", req.HostID.String()).
		Msg("private room created")

	return code, room, nil
}

// JoinRoom adds a player to an existing room.
func (r *RoomManager) JoinRoom(ctx context.Context, roomCode string, userID uuid.UUID, displayName string, isGuest bool) (*PrivateRoom, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	room, exists := r.rooms[roomCode]
	if !exists {
		return nil, fmt.Errorf("room not found")
	}

	if room.Status != RoomStatusWaiting {
		return nil, fmt.Errorf("room not accepting players")
	}

	if len(room.Players) >= room.MaxPlayers {
		return nil, fmt.Errorf("room full")
	}

	// Check if already joined (prevent self-matching/duplicate joins)
	for _, p := range room.Players {
		if p.UserID == userID {
			return nil, fmt.Errorf("user already in room") // prevent duplicate joins
		}
	}
	
	// Prevent host from joining their own room again
	if userID == room.HostID {
		return nil, fmt.Errorf("host cannot join their own room again")
	}

	room.Players = append(room.Players, RoomPlayer{
		UserID:      userID,
		DisplayName: displayName,
		IsGuest:     isGuest,
		JoinedAt:    time.Now(),
	})

	r.logger.Info().
		Str("room_code", roomCode).
		Str("user_id", userID.String()).
		Int("player_count", len(room.Players)).
		Msg("player joined room")

	return room, nil
}

// GetRoom retrieves room by code.
func (r *RoomManager) GetRoom(roomCode string) (*PrivateRoom, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	room, exists := r.rooms[roomCode]
	if !exists {
		return nil, fmt.Errorf("room not found")
	}
	return room, nil
}

// StartRoom initiates countdown and transitions room to starting state.
func (r *RoomManager) StartRoom(ctx context.Context, roomCode string, matchID uuid.UUID, countdownSeconds int) (*PrivateRoom, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	room, exists := r.rooms[roomCode]
	if !exists {
		return nil, fmt.Errorf("room not found")
	}

	if room.Status != RoomStatusWaiting {
		return nil, fmt.Errorf("room cannot be started")
	}

	if len(room.Players) < 2 {
		return nil, fmt.Errorf("need at least 2 players")
	}

	room.MatchID = &matchID
	room.Status = RoomStatusStarting
	if countdownSeconds > 0 {
		room.StartCountdown = countdownSeconds
	}

	r.logger.Info().
		Str("room_code", roomCode).
		Str("match_id", matchID.String()).
		Int("countdown", room.StartCountdown).
		Msg("room starting")

	return room, nil
}

// generateRoomCode creates a 6-digit numeric code (000000-999999).
func (r *RoomManager) generateRoomCode() string {
	for {
		// Generate random number between 100000 and 999999
		// Using 100000-999999 to ensure 6 digits (avoid leading zeros)
		num := 100000 + rand.Intn(900000)
		code := fmt.Sprintf("%06d", num)
		
		// Ensure uniqueness
		r.mu.RLock()
		_, exists := r.rooms[code]
		r.mu.RUnlock()
		if !exists {
			return code
		}
	}
}
