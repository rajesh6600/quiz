package leaderboard

import (
	"context"
	"encoding/json"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	ws "github.com/gokatarajesh/quiz-platform/pkg/http/ws"
)

// Broadcaster listens for Redis Pub/Sub leaderboard updates and forwards them to all clients.
type Broadcaster struct {
	redis   *redis.Client
	hub     *ws.Hub
	channel string
	logger  zerolog.Logger
}

// NewBroadcaster creates a Pub/Sub powered leaderboard broadcaster.
func NewBroadcaster(redis *redis.Client, hub *ws.Hub, channel string, logger zerolog.Logger) *Broadcaster {
	if channel == "" {
		channel = "lb:updates"
	}
	return &Broadcaster{
		redis:   redis,
		hub:     hub,
		channel: channel,
		logger:  logger.With().Str("component", "leaderboard_broadcaster").Logger(),
	}
}

// Run subscribes to the update channel and blocks until the context is cancelled.
func (b *Broadcaster) Run(ctx context.Context) error {
	if b.redis == nil || b.hub == nil {
		return nil
	}

	sub := b.redis.Subscribe(ctx, b.channel)
	defer sub.Close()

	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-ch:
			if !ok {
				return nil
			}
			b.forward(msg.Payload)
		}
	}
}

func (b *Broadcaster) forward(payload string) {
	var evt ws.LeaderboardUpdatePayload
	if err := json.Unmarshal([]byte(payload), &evt); err != nil {
		b.logger.Warn().Err(err).Msg("failed to decode leaderboard update payload")
		return
	}

	raw, err := json.Marshal(evt)
	if err != nil {
		b.logger.Warn().Err(err).Msg("failed to marshal leaderboard WS payload")
		return
	}

	msg := ws.Message{
		Type:    ws.TypeLeaderboardUpdate,
		Payload: raw,
	}
	if err := b.hub.BroadcastAll(msg); err != nil {
		b.logger.Warn().Err(err).Msg("failed to broadcast leaderboard update")
	}
}
