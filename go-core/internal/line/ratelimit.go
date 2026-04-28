package line

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	defaultRateLimit      = 30
	defaultRateWindowSecs = 60
)

// Metrics is a minimal counter interface so the metrics backend (Prometheus or
// a stub) can be injected without creating a hard dependency.
type Metrics interface {
	Inc(name string)
}

// RateLimiter enforces per-LINE-userId sliding window rate limits using a
// Redis INCR counter.  The window is a fixed bucket, not a true sliding window.
// This is intentional: the goal is stopping spammers from draining LLM budget,
// not perfect accounting at bucket boundaries.
type RateLimiter struct {
	redis         *redis.Client
	defaultLimit  int
	defaultWindow time.Duration
	logger        *slog.Logger
	metrics       Metrics
}

// RateLimitResult carries the decision and metadata for the caller.
type RateLimitResult struct {
	Allowed      bool
	Remaining    int
	ResetSeconds int
}

// NewRateLimiter constructs a RateLimiter.  Pass 0 for limit/window to use
// the defaults (30 req / 60 s).
func NewRateLimiter(
	redisClient *redis.Client,
	defaultLimit int,
	defaultWindow time.Duration,
	logger *slog.Logger,
	metrics Metrics,
) *RateLimiter {
	if defaultLimit <= 0 {
		defaultLimit = defaultRateLimit
	}
	if defaultWindow <= 0 {
		defaultWindow = time.Duration(defaultRateWindowSecs) * time.Second
	}
	return &RateLimiter{
		redis:         redisClient,
		defaultLimit:  defaultLimit,
		defaultWindow: defaultWindow,
		logger:        logger,
		metrics:       metrics,
	}
}

// Check evaluates the rate limit for the given LINE userId.  It always
// returns allowed=true on Redis errors to avoid breaking the webhook when
// Redis is temporarily unavailable (fail-open policy).
func (r *RateLimiter) Check(ctx context.Context, lineUserID string) RateLimitResult {
	if lineUserID == "" {
		return RateLimitResult{Allowed: true, Remaining: -1}
	}

	key := fmt.Sprintf("ratelimit:line:%s", lineUserID)
	windowSecs := int(r.defaultWindow.Seconds())

	count, err := r.redis.Incr(ctx, key).Result()
	if err != nil {
		// Fail open — Redis hiccup must not break the webhook.
		r.logger.Warn("rate limit redis error; failing open",
			"err", err,
			"lineUserIdHash", lineUserID[:min(8, len(lineUserID))],
		)
		return RateLimitResult{Allowed: true, Remaining: -1}
	}

	if count == 1 {
		// First request in window; set expiry.  Ignore error: the worst
		// case is that the key never expires, which over-limits the user
		// rather than allowing bypass.
		_ = r.redis.Expire(ctx, key, r.defaultWindow).Err()
	}

	r.metrics.Inc("line_ratelimit_hit_total")

	if int(count) > r.defaultLimit {
		r.metrics.Inc("line_ratelimit_blocked_total")
		r.logger.Warn("line rate limit blocked",
			"lineUserIdHash", lineUserID[:min(8, len(lineUserID))],
			"count", count,
			"limit", r.defaultLimit,
		)
		return RateLimitResult{Allowed: false, Remaining: 0, ResetSeconds: windowSecs}
	}

	return RateLimitResult{
		Allowed:      true,
		Remaining:    r.defaultLimit - int(count),
		ResetSeconds: windowSecs,
	}
}

