package brain

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	cachePrefix     = "brain:retrieve:"
	cacheTTL        = 30 * time.Second
)

// BrainCache is the hot-path Redis cache for WikiBrainService.Retrieve.
// TTL is intentionally short — the graph mutates on every important fact and a
// stale answer is worse than a fresh DB round trip.
type BrainCache struct {
	rdb *redis.Client
	log *slog.Logger
	ttl time.Duration
}

// NewBrainCache creates a cache with the default 30-second TTL.
func NewBrainCache(rdb *redis.Client, log *slog.Logger) *BrainCache {
	return &BrainCache{rdb: rdb, log: log, ttl: cacheTTL}
}

// CacheResult holds either a cache hit value or a miss signal.
type CacheResult[T any] struct {
	Hit   bool
	Value T
}

// Get returns a typed value from cache. A miss or Redis error both result in
// Hit=false so callers always fall through to the DB path safely.
func Get[T any](ctx context.Context, c *BrainCache, namespace, text string) CacheResult[T] {
	key := buildCacheKey(namespace, text)
	raw, err := c.rdb.Get(ctx, key).Bytes()
	if err != nil {
		if err != redis.Nil {
			c.log.Warn("brain cache get failed", "err", err)
		}
		return CacheResult[T]{}
	}
	var val T
	if err := json.Unmarshal(raw, &val); err != nil {
		c.log.Warn("brain cache unmarshal failed", "err", err)
		return CacheResult[T]{}
	}
	return CacheResult[T]{Hit: true, Value: val}
}

// Set writes a value into cache. Errors are logged and swallowed — a cache
// write failure must never block the caller.
func Set[T any](ctx context.Context, c *BrainCache, namespace, text string, value T) {
	b, err := json.Marshal(value)
	if err != nil {
		c.log.Warn("brain cache marshal failed", "err", err)
		return
	}
	key := buildCacheKey(namespace, text)
	if err := c.rdb.Set(ctx, key, b, c.ttl).Err(); err != nil {
		c.log.Warn("brain cache set failed", "err", err)
	}
}

// Invalidate deletes all cached retrieval results for a namespace via SCAN +
// DEL so a page upsert or edge link never serves stale data.
func (c *BrainCache) Invalidate(ctx context.Context, namespace string) {
	pattern := fmt.Sprintf("%s%s:*", cachePrefix, hashNamespace(namespace))
	var cursor uint64
	for {
		keys, next, err := c.rdb.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			c.log.Warn("brain cache invalidate scan failed", "namespace", namespace, "err", err)
			return
		}
		if len(keys) > 0 {
			if err := c.rdb.Del(ctx, keys...).Err(); err != nil {
				c.log.Warn("brain cache invalidate del failed", "namespace", namespace, "err", err)
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
}

func buildCacheKey(namespace, text string) string {
	return fmt.Sprintf("%s%s:%s", cachePrefix, hashNamespace(namespace), hashText(text))
}

func hashNamespace(namespace string) string {
	h := sha1.Sum([]byte(namespace))
	return hex.EncodeToString(h[:])[:12]
}

func hashText(text string) string {
	h := sha1.Sum([]byte(strings.TrimSpace(strings.ToLower(text))))
	return hex.EncodeToString(h[:])[:16]
}
