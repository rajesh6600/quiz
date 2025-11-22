package leaderboard

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	ws "github.com/gokatarajesh/quiz-platform/pkg/http/ws"
)

// Supported leaderboard windows.
const (
	WindowDaily   = "daily"
	WindowWeekly  = "weekly"
	WindowMonthly = "monthly"
	WindowAllTime = "all_time"
)

var defaultWindows = []string{WindowDaily, WindowWeekly, WindowMonthly, WindowAllTime}

// Entry represents a leaderboard record sent to clients.
type Entry struct {
	UserID        uuid.UUID `json:"user_id"`
	DisplayName   string    `json:"display_name"`
	Score         int       `json:"score"`
	Wins          int       `json:"wins"`
	Games         int       `json:"games"`
	Accuracy      float64   `json:"accuracy"`
	CorrectTotal  int       `json:"-"`
	QuestionTotal int       `json:"-"`
}

// RecordRequest captures the data required to update leaderboard aggregates.
type RecordRequest struct {
	UserID        uuid.UUID
	DisplayName   string
	Score         int
	CorrectCount  int
	QuestionCount int
	Won           bool
	MatchID       uuid.UUID
	Windows       []string
	Eligible      bool
}

// ServiceOptions configures leaderboard service behavior.
type ServiceOptions struct {
	TopN             int
	PubSubChannel    string
	Windows          []string
	ScoreDecay       float64
	EntryTTL         time.Duration
	RedisKeyPrefix   string
	SnapshotTopLimit int
}

// Service manages leaderboard state in Redis and emits updates over Pub/Sub.
type Service struct {
	redis          *redis.Client
	logger         zerolog.Logger
	topN           int
	pubsubChannel  string
	windows        []string
	scoreDecay     float64
	entryTTL       time.Duration
	prefix         string
	snapshotTopLim int
}

// NewService constructs a leaderboard service instance.
func NewService(redis *redis.Client, logger zerolog.Logger, opts ServiceOptions) *Service {
	topN := opts.TopN
	if topN <= 0 {
		topN = 50
	}
	channel := opts.PubSubChannel
	if channel == "" {
		channel = "lb:updates"
	}
	windows := opts.Windows
	if len(windows) == 0 {
		windows = defaultWindows
	}
	prefix := opts.RedisKeyPrefix
	if prefix == "" {
		prefix = "lb"
	}
	snapTop := opts.SnapshotTopLimit
	if snapTop <= 0 {
		snapTop = 100
	}

	return &Service{
		redis:          redis,
		logger:         logger.With().Str("component", "leaderboard").Logger(),
		topN:           topN,
		pubsubChannel:  channel,
		windows:        windows,
		scoreDecay:     opts.ScoreDecay,
		entryTTL:       opts.EntryTTL,
		prefix:         prefix,
		snapshotTopLim: snapTop,
	}
}

// RecordResult updates leaderboard metrics for applicable windows.
func (s *Service) RecordResult(ctx context.Context, req RecordRequest) error {
	if !req.Eligible {
		return nil
	}

	windows := req.Windows
	if len(windows) == 0 {
		windows = s.windows
	}

	accuracy := 0.0
	if req.QuestionCount > 0 {
		accuracy = float64(req.CorrectCount) / float64(req.QuestionCount)
	}

	entry := Entry{
		UserID:        req.UserID,
		DisplayName:   req.DisplayName,
		Score:         req.Score,
		Wins:          boolToInt(req.Won),
		Games:         1,
		Accuracy:      accuracy,
		CorrectTotal:  req.CorrectCount,
		QuestionTotal: req.QuestionCount,
	}

	for _, window := range windows {
		if err := s.updateWindow(ctx, window, entry); err != nil {
			return err
		}
	}

	// Publish aggregate update for WebSocket consumers.
	go s.publishUpdate(context.Background(), req.MatchID, windows)
	return nil
}

// Top retrieves the top N entries for a given window.
func (s *Service) Top(ctx context.Context, window string, limit int) ([]Entry, error) {
	if limit <= 0 || limit > s.topN {
		limit = s.topN
	}

	zKey := s.leaderboardKey(window)
	results, err := s.redis.ZRevRangeWithScores(ctx, zKey, 0, int64(limit-1)).Result()
	if err != nil {
		return nil, fmt.Errorf("fetch leaderboard: %w", err)
	}

	entries := make([]Entry, 0, len(results))
	for _, z := range results {
		meta, err := s.readMeta(ctx, window, z.Member.(string))
		if err != nil {
			s.logger.Warn().Err(err).Msg("failed to read leaderboard metadata")
			continue
		}
		meta.Score = int(z.Score)
		entries = append(entries, *meta)
	}
	return entries, nil
}

// SnapshotTop returns the configured snapshot size for persistence jobs.
func (s *Service) SnapshotTop(ctx context.Context, window string) ([]Entry, error) {
	return s.Top(ctx, window, s.snapshotTopLim)
}

func (s *Service) updateWindow(ctx context.Context, window string, entry Entry) error {
	zKey := s.leaderboardKey(window)
	metaKey := s.metaKey(window, entry.UserID)

	pipe := s.redis.TxPipeline()
	pipe.ZIncrBy(ctx, zKey, float64(entry.Score), entry.UserID.String())
	pipe.HIncrBy(ctx, metaKey, "wins", int64(entry.Wins))
	pipe.HIncrBy(ctx, metaKey, "games", int64(entry.Games))
	pipe.HIncrBy(ctx, metaKey, "correct", int64(entry.CorrectTotal))
	pipe.HIncrBy(ctx, metaKey, "questions", int64(entry.QuestionTotal))
	pipe.HSet(ctx, metaKey, map[string]interface{}{
		"display_name": entry.DisplayName,
	})
	if s.entryTTL > 0 && window != WindowAllTime {
		pipe.Expire(ctx, zKey, s.entryTTL)
		pipe.Expire(ctx, metaKey, s.entryTTL)
	}

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("update leaderboard window %s: %w", window, err)
	}
	return nil
}

func (s *Service) publishUpdate(ctx context.Context, matchID uuid.UUID, windows []string) {
	for _, window := range windows {
		entries, err := s.Top(ctx, window, 10)
		if err != nil {
			s.logger.Warn().Err(err).Str("window", window).Msg("failed to collect leaderboard update")
			continue
		}
		if len(entries) == 0 {
			continue
		}
		wsEntries := toWSEntries(entries)

		payload := ws.LeaderboardUpdatePayload{
			Window:  window,
			MatchID: matchID.String(),
			Top:     wsEntries,
		}
		data, err := json.Marshal(payload)
		if err != nil {
			s.logger.Warn().Err(err).Msg("failed to marshal leaderboard update")
			continue
		}
		if err := s.redis.Publish(ctx, s.pubsubChannel, data).Err(); err != nil {
			s.logger.Warn().Err(err).Msg("failed to publish leaderboard update")
		}
	}
}

func (s *Service) readMeta(ctx context.Context, window string, userIDStr string) (*Entry, error) {
	metaKey := s.metaKey(window, uuid.MustParse(userIDStr))
	data, err := s.redis.HGetAll(ctx, metaKey).Result()
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		// No metadata yet; fallback minimal entry.
		return &Entry{UserID: uuid.MustParse(userIDStr)}, nil
	}

	entry := &Entry{UserID: uuid.MustParse(userIDStr)}
	entry.DisplayName = data["display_name"]
	entry.Wins = parseInt(data["wins"])
	entry.Games = parseInt(data["games"])
	entry.CorrectTotal = parseInt(data["correct"])
	entry.QuestionTotal = parseInt(data["questions"])
	if entry.QuestionTotal > 0 {
		entry.Accuracy = float64(entry.CorrectTotal) / float64(entry.QuestionTotal)
	}
	return entry, nil
}

func (s *Service) leaderboardKey(window string) string {
	return fmt.Sprintf("%s:%s", s.prefix, window)
}

func (s *Service) metaKey(window string, userID uuid.UUID) string {
	return fmt.Sprintf("%s:%s:meta:%s", s.prefix, window, userID.String())
}

// RecordPrivateRoomResult records a result to a room-specific leaderboard (separate from main leaderboard).
func (s *Service) RecordPrivateRoomResult(ctx context.Context, roomCode string, req RecordRequest) error {
	if !req.Eligible {
		return nil
	}

	accuracy := 0.0
	if req.QuestionCount > 0 {
		accuracy = float64(req.CorrectCount) / float64(req.QuestionCount)
	}

	entry := Entry{
		UserID:        req.UserID,
		DisplayName:   req.DisplayName,
		Score:         req.Score,
		Wins:          boolToInt(req.Won),
		Games:         1,
		Accuracy:      accuracy,
		CorrectTotal:  req.CorrectCount,
		QuestionTotal: req.QuestionCount,
	}

	// Use room-specific keys
	zKey := s.privateRoomLeaderboardKey(roomCode)
	metaKey := s.privateRoomMetaKey(roomCode, entry.UserID)

	pipe := s.redis.TxPipeline()
	pipe.ZIncrBy(ctx, zKey, float64(entry.Score), entry.UserID.String())
	pipe.HIncrBy(ctx, metaKey, "wins", int64(entry.Wins))
	pipe.HIncrBy(ctx, metaKey, "games", int64(entry.Games))
	pipe.HIncrBy(ctx, metaKey, "correct", int64(entry.CorrectTotal))
	pipe.HIncrBy(ctx, metaKey, "questions", int64(entry.QuestionTotal))
	pipe.HSet(ctx, metaKey, map[string]interface{}{
		"display_name": entry.DisplayName,
	})
	// Private room leaderboards expire after 7 days of inactivity
	pipe.Expire(ctx, zKey, 7*24*time.Hour)
	pipe.Expire(ctx, metaKey, 7*24*time.Hour)

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("update private room leaderboard %s: %w", roomCode, err)
	}

	return nil
}

// GetPrivateRoomLeaderboard retrieves the top N entries for a private room.
func (s *Service) GetPrivateRoomLeaderboard(ctx context.Context, roomCode string, limit int) ([]Entry, error) {
	if limit <= 0 || limit > s.topN {
		limit = s.topN
	}

	zKey := s.privateRoomLeaderboardKey(roomCode)
	results, err := s.redis.ZRevRangeWithScores(ctx, zKey, 0, int64(limit-1)).Result()
	if err != nil {
		return nil, fmt.Errorf("fetch private room leaderboard: %w", err)
	}

	entries := make([]Entry, 0, len(results))
	for _, z := range results {
		meta, err := s.readPrivateRoomMeta(ctx, roomCode, z.Member.(string))
		if err != nil {
			s.logger.Warn().Err(err).Msg("failed to read private room leaderboard metadata")
			continue
		}
		meta.Score = int(z.Score)
		entries = append(entries, *meta)
	}
	return entries, nil
}

func (s *Service) privateRoomLeaderboardKey(roomCode string) string {
	return fmt.Sprintf("%s:private_room:%s", s.prefix, roomCode)
}

func (s *Service) privateRoomMetaKey(roomCode string, userID uuid.UUID) string {
	return fmt.Sprintf("%s:private_room:%s:meta:%s", s.prefix, roomCode, userID.String())
}

func (s *Service) readPrivateRoomMeta(ctx context.Context, roomCode string, userIDStr string) (*Entry, error) {
	metaKey := s.privateRoomMetaKey(roomCode, uuid.MustParse(userIDStr))
	data, err := s.redis.HGetAll(ctx, metaKey).Result()
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return &Entry{UserID: uuid.MustParse(userIDStr)}, nil
	}

	entry := &Entry{UserID: uuid.MustParse(userIDStr)}
	entry.DisplayName = data["display_name"]
	entry.Wins = parseInt(data["wins"])
	entry.Games = parseInt(data["games"])
	entry.CorrectTotal = parseInt(data["correct"])
	entry.QuestionTotal = parseInt(data["questions"])
	if entry.QuestionTotal > 0 {
		entry.Accuracy = float64(entry.CorrectTotal) / float64(entry.QuestionTotal)
	}
	return entry, nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func parseFloat(val string) float64 {
	if val == "" {
		return 0
	}
	f, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return 0
	}
	return f
}

func parseInt(val string) int {
	if val == "" {
		return 0
	}
	i, err := strconv.Atoi(val)
	if err != nil {
		return 0
	}
	return i
}
