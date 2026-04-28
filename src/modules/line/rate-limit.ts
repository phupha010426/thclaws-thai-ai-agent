import { redis } from '../../db/redis.js';
import { logger } from '../../libs/logger.js';
import { metrics } from '../metrics/metrics.service.js';

/**
 * Per-LINE-userId sliding window rate limit. Caddy cannot do this because
 * the userId only appears inside the signed body. We enforce after signature
 * verification, before the message is enqueued, so spam from a single user
 * cannot drain LLM budget for the rest of the tenants.
 */

export type RateLimitDecision = { allowed: boolean; remaining: number; resetSeconds: number };

const DEFAULT_LIMIT = 30;
const DEFAULT_WINDOW_SECONDS = 60;

export async function checkLineRateLimit(lineUserId: string, options?: { limit?: number; windowSeconds?: number }): Promise<RateLimitDecision> {
  if (!lineUserId) return { allowed: true, remaining: -1, resetSeconds: 0 };
  const limit = options?.limit ?? DEFAULT_LIMIT;
  const windowSeconds = options?.windowSeconds ?? DEFAULT_WINDOW_SECONDS;

  // Token-bucket-ish counter: INCR a key that expires after `windowSeconds`.
  // We do not use a fancy sliding window because the API tolerates +/- one
  // bucket boundary; the goal is "stop a spammer", not perfect accounting.
  const key = `ratelimit:line:${lineUserId}`;
  try {
    const count = await redis.incr(key);
    if (count === 1) await redis.expire(key, windowSeconds);
    metrics.inc('line_ratelimit_hit_total');
    if (count > limit) {
      metrics.inc('line_ratelimit_blocked_total');
      logger.warn({ lineUserIdHash: lineUserId.slice(0, 8), count, limit }, 'line rate limit blocked');
      return { allowed: false, remaining: 0, resetSeconds: windowSeconds };
    }
    return { allowed: true, remaining: limit - count, resetSeconds: windowSeconds };
  } catch (error) {
    // Fail open — Redis hiccup must not break the webhook.
    logger.warn({ err: (error as Error).message }, 'rate limit check failed; fail open');
    return { allowed: true, remaining: -1, resetSeconds: 0 };
  }
}
