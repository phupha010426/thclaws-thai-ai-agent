import { createHash } from 'node:crypto';
import { redis } from '../../db/redis.js';
import { logger } from '../../libs/logger.js';
import { metrics } from '../metrics/metrics.service.js';

/**
 * Hot-path cache for the WikiBrain retrieval. Each entry is keyed by
 * (namespace, sha1(text)) so different users never share results, even on
 * collisions. TTL is short on purpose — the brain mutates whenever the user
 * sends a new important fact, and a stale answer is worse than a fresh DB
 * round trip.
 */
const PREFIX = 'brain:retrieve:';
const DEFAULT_TTL_SECONDS = 30;

export type CacheEntry<T> = { hit: true; value: T } | { hit: false };

export class BrainCache {
  constructor(private readonly ttlSeconds = DEFAULT_TTL_SECONDS) {}

  async get<T>(namespace: string, text: string): Promise<CacheEntry<T>> {
    const key = buildKey(namespace, text);
    try {
      const raw = await redis.get(key);
      if (!raw) {
        metrics.inc('brain_cache_miss_total');
        return { hit: false };
      }
      metrics.inc('brain_cache_hit_total');
      return { hit: true, value: JSON.parse(raw) as T };
    } catch (error) {
      logger.warn({ err: (error as Error).message }, 'brain cache get failed');
      return { hit: false };
    }
  }

  async set<T>(namespace: string, text: string, value: T) {
    const key = buildKey(namespace, text);
    try {
      await redis.set(key, JSON.stringify(value), 'EX', this.ttlSeconds);
    } catch (error) {
      logger.warn({ err: (error as Error).message }, 'brain cache set failed');
    }
  }

  /** Wipe every cached retrieval for a namespace, e.g. after the user adds a new memory. */
  async invalidate(namespace: string) {
    const pattern = `${PREFIX}${hashNamespace(namespace)}:*`;
    try {
      let cursor = '0';
      do {
        const [next, keys] = await redis.scan(cursor, 'MATCH', pattern, 'COUNT', 100);
        cursor = next;
        if (keys.length > 0) await redis.del(...keys);
      } while (cursor !== '0');
    } catch (error) {
      logger.warn({ err: (error as Error).message, namespace }, 'brain cache invalidate failed');
    }
  }
}

function buildKey(namespace: string, text: string) {
  return `${PREFIX}${hashNamespace(namespace)}:${hashText(text)}`;
}

function hashNamespace(namespace: string) {
  return createHash('sha1').update(namespace).digest('hex').slice(0, 12);
}

function hashText(text: string) {
  return createHash('sha1').update(text.trim().toLowerCase()).digest('hex').slice(0, 16);
}

export const brainCache = new BrainCache();
