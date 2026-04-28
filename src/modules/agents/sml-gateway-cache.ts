import { createHash } from 'node:crypto';
import { redis } from '../../db/redis.js';
import { logger } from '../../libs/logger.js';

/**
 * Response cache for the SMLGateway. Cuts cost + latency when the same
 * deterministic prompt is asked repeatedly (summary requests, help text,
 * idempotent tool reasoning).
 *
 * STRICT TENANT ISOLATION: every cache key is derived from `namespace`
 * (= LINE userId scope). Callers that omit namespace bypass the cache
 * entirely — we refuse to share entries across users, even for prompts that
 * look identical, because LLM context can leak through framing.
 */
const PREFIX = 'sml:resp:';
const DEFAULT_TTL_SECONDS = 300;

export type CachableRequest = {
  namespace: string;
  model: string;
  messages: unknown[];
  schema?: unknown;
  strategy?: string;
  preferProviders?: string;
  excludeProviders?: string;
};

export class SmlGatewayCache {
  constructor(private readonly ttlSeconds = DEFAULT_TTL_SECONDS) {}

  /** Returns the cached payload or null. Errors degrade silently. */
  async get<T>(request: CachableRequest): Promise<T | null> {
    if (!request.namespace) return null;
    const key = buildKey(request);
    try {
      const raw = await redis.get(key);
      if (!raw) return null;
      return JSON.parse(raw) as T;
    } catch (error) {
      logger.warn({ err: (error as Error).message }, 'sml cache get failed');
      return null;
    }
  }

  async set<T>(request: CachableRequest, value: T) {
    if (!request.namespace) return;
    const key = buildKey(request);
    try {
      await redis.set(key, JSON.stringify(value), 'EX', this.ttlSeconds);
    } catch (error) {
      logger.warn({ err: (error as Error).message }, 'sml cache set failed');
    }
  }

  /**
   * Drop every cached SMLGateway response for a namespace. Call after any
   * write that changes what an LLM-driven summary should contain
   * (ledger event append, wiki update, memory addition).
   */
  async invalidate(namespace: string) {
    if (!namespace) return;
    const nsHash = createHash('sha1').update(namespace).digest('hex').slice(0, 8);
    const pattern = `${PREFIX}${nsHash}:*`;
    try {
      let cursor = '0';
      do {
        const [next, keys] = await redis.scan(cursor, 'MATCH', pattern, 'COUNT', 200);
        cursor = next;
        if (keys.length > 0) await redis.del(...keys);
      } while (cursor !== '0');
    } catch (error) {
      logger.warn({ err: (error as Error).message, namespace }, 'sml cache invalidate failed');
    }
  }
}

function buildKey(request: CachableRequest): string {
  const normalized = {
    namespace: request.namespace,
    model: request.model,
    messages: request.messages,
    schema: request.schema ?? null,
    strategy: request.strategy ?? 'default',
    prefer: request.preferProviders ?? '',
    exclude: request.excludeProviders ?? ''
  };
  const hash = createHash('sha256').update(JSON.stringify(normalized)).digest('hex').slice(0, 24);
  const nsHash = createHash('sha1').update(normalized.namespace).digest('hex').slice(0, 8);
  return `${PREFIX}${nsHash}:${hash}`;
}

export const smlGatewayCache = new SmlGatewayCache();
