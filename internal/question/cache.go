package question

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const defaultCacheTTL = 5 * time.Minute

// Cache provides Redis-backed question pack caching to offload DB/API calls.
type Cache struct {
	client *redis.Client
	ttl    time.Duration
}

var _ PackCache = (*Cache)(nil)

func NewCache(client *redis.Client, ttl time.Duration) *Cache {
	if ttl <= 0 {
		ttl = defaultCacheTTL
	}
	return &Cache{client: client, ttl: ttl}
}

func (c *Cache) key(req PackRequest) string {
	var diffParts []string
	for k, v := range req.DifficultyCounts {
		diffParts = append(diffParts, k+":"+fmt.Sprint(v))
	}
	sort.Strings(diffParts)
	return strings.Join([]string{
		"questionpack",
		req.Category,
		req.Seed,
		fmt.Sprint(req.TotalQuestions),
		strings.Join(diffParts, "|"),
	}, ":")
}

func (c *Cache) Get(ctx context.Context, req PackRequest) (*PackResponse, error) {
	data, err := c.client.Get(ctx, c.key(req)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}
	var resp PackResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Cache) Set(ctx context.Context, req PackRequest, resp PackResponse) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, c.key(req), data, c.ttl).Err()
}
