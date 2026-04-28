package cache

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	gatewayPrefix = "sml:resp:"

	GatewayTTL = 300 * time.Second
)

type cachePayload struct {
	Namespace string `json:"namespace"`
	Model     string `json:"model"`
	Messages  any    `json:"messages"`
	Schema    any    `json:"schema"`
	Strategy  string `json:"strategy"`
	Prefer    string `json:"prefer"`
	Exclude   string `json:"exclude"`
}

type baseCache struct {
	rdb    *redis.Client
	prefix string
	ttl    time.Duration
	log    *slog.Logger
}

func (c *baseCache) buildKey(namespace, model string, messages, schema any, strategy, prefer, exclude string) string {
	p := cachePayload{
		Namespace: namespace,
		Model:     model,
		Messages:  messages,
		Schema:    schema,
		Strategy:  strategy,
		Prefer:    prefer,
		Exclude:   exclude,
	}
	data, _ := json.Marshal(p)
	payloadHash := fmt.Sprintf("%x", sha256.Sum256(data))[:24]
	nsHash := fmt.Sprintf("%x", sha1.Sum([]byte(namespace)))[:8]
	return fmt.Sprintf("%s%s:%s", c.prefix, nsHash, payloadHash)
}

func (c *baseCache) nsPattern(namespace string) string {
	nsHash := fmt.Sprintf("%x", sha1.Sum([]byte(namespace)))[:8]
	return fmt.Sprintf("%s%s:*", c.prefix, nsHash)
}

func (c *baseCache) get(ctx context.Context, key string) (string, bool) {
	raw, err := c.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", false
	}
	if err != nil {
		c.log.Warn("cache get failed", "err", err)
		return "", false
	}
	return raw, true
}

func (c *baseCache) set(ctx context.Context, key, value string) {
	if err := c.rdb.Set(ctx, key, value, c.ttl).Err(); err != nil {
		c.log.Warn("cache set failed", "err", err)
	}
}

func (c *baseCache) invalidateNamespace(ctx context.Context, namespace string) {
	if namespace == "" {
		return
	}
	pattern := c.nsPattern(namespace)
	var cursor uint64
	for {
		keys, next, err := c.rdb.Scan(ctx, cursor, pattern, 200).Result()
		if err != nil {
			c.log.Warn("cache invalidate scan failed", "err", err, "namespace", namespace)
			return
		}
		if len(keys) > 0 {
			if err := c.rdb.Del(ctx, keys...).Err(); err != nil {
				c.log.Warn("cache invalidate del failed", "err", err, "namespace", namespace)
				return
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
}

// SmlGatewayCache is the response cache for SMLGateway chat calls.
// Every key is scoped to a namespace (LINE userId). Callers that pass an
// empty namespace bypass the cache entirely so we never mix tenant data.
type SmlGatewayCache struct {
	base baseCache
}

func NewSmlGatewayCache(rdb *redis.Client, log *slog.Logger) *SmlGatewayCache {
	return &SmlGatewayCache{base: baseCache{rdb: rdb, prefix: gatewayPrefix, ttl: GatewayTTL, log: log}}
}

func (c *SmlGatewayCache) Get(ctx context.Context, namespace, model string, messages, schema any, strategy, prefer, exclude string) (string, bool) {
	if namespace == "" {
		return "", false
	}
	key := c.base.buildKey(namespace, model, messages, schema, strategy, prefer, exclude)
	return c.base.get(ctx, key)
}

func (c *SmlGatewayCache) Set(ctx context.Context, namespace, model string, messages, schema any, strategy, prefer, exclude, value string) {
	if namespace == "" {
		return
	}
	key := c.base.buildKey(namespace, model, messages, schema, strategy, prefer, exclude)
	c.base.set(ctx, key, value)
}

func (c *SmlGatewayCache) InvalidateNamespace(ctx context.Context, namespace string) {
	c.base.invalidateNamespace(ctx, namespace)
}

