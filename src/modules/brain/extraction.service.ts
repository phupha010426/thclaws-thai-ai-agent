import { createHash } from 'node:crypto';
import { z } from 'zod';
import { logger } from '../../libs/logger.js';
import { smlGatewayService } from '../agents/sml-gateway.service.js';
import { redis } from '../../db/redis.js';
import { env } from '../../config/env.js';
import { embeddingService } from './embedding.service.js';
import { redactPii } from './redaction.js';
import { wikiBrainService, type WikiKind } from './wiki-brain.service.js';

const MIN_TEXT_LENGTH = 8;
const DEDUP_TTL_SECONDS = 300;

/**
 * Extracts entities + relations from a chunk of conversation and persists
 * them as wiki pages, aliases, edges, and embeddings — all under the same
 * `namespace`. This is meant to run from a background queue, not on the LINE
 * reply path, because the LLM call is the slowest step.
 */

const extractionSchema = z.object({
  pages: z.array(
    z.object({
      slug: z.string().min(1),
      title: z.string().min(1),
      kind: z.enum(['person', 'place', 'item', 'event', 'concept', 'preference', 'rule']).default('concept'),
      summary: z.string().optional(),
      aliases: z.array(z.string()).optional(),
      importance: z.number().min(0).max(1).optional()
    })
  ).default([]),
  edges: z.array(
    z.object({
      fromSlug: z.string().min(1),
      toSlug: z.string().min(1),
      relation: z.string().min(1),
      weight: z.number().min(0).max(1).optional()
    })
  ).default([])
});

export type ExtractInput = {
  namespace: string;
  ownerId: string;
  agentId?: string;
  text: string;
  causationId?: string;
};

export class ExtractionService {
  async extractAndStore(input: ExtractInput): Promise<{ pages: number; edges: number }> {
    if (!input.namespace) throw new Error('extraction: namespace required');
    const trimmed = input.text.trim();
    if (!trimmed) return { pages: 0, edges: 0 };

    // Throttle 1: skip very short messages — no durable fact will fit in 7 chars.
    if (trimmed.length < MIN_TEXT_LENGTH) return { pages: 0, edges: 0 };

    // Throttle 2: dedup. If we already extracted from the exact same text in
    // this namespace within DEDUP_TTL_SECONDS, skip — saves an LLM call.
    const dedupKey = `brain:extract:${createHash('sha1').update(input.namespace).digest('hex').slice(0, 8)}:${createHash('sha1').update(trimmed.toLowerCase()).digest('hex').slice(0, 16)}`;
    try {
      const set = await redis.set(dedupKey, '1', 'EX', DEDUP_TTL_SECONDS, 'NX');
      if (set === null) {
        logger.debug({ namespace: input.namespace }, 'extraction skipped (dedup)');
        return { pages: 0, edges: 0 };
      }
    } catch (error) {
      logger.warn({ err: (error as Error).message }, 'extraction dedup failed (continuing)');
    }

    const sanitized = redactPii(trimmed);

    const raw = await smlGatewayService.chat({
      namespace: input.namespace,
      // Prefer the strong default model — extraction needs structured JSON
      // output, and the fast/small models often return prose or arrays.
      model: env.DEFAULT_AGENT_MODEL,
      fallbackModels: [env.GENERAL_AGENT_MODEL, env.FAST_AGENT_MODEL],
      maxTokens: 1500,
      strategy: 'fastest',
      maxLatencyMs: env.SMLGATEWAY_MAX_LATENCY_MS,
      preferProviders: env.SMLGATEWAY_PREFER_PROVIDERS,
      excludeProviders: env.SMLGATEWAY_TEXT_EXCLUDE_PROVIDERS,
      messages: [
        {
          role: 'system',
          content: [
            'You build a personal Thai-language knowledge graph for a single user.',
            'Read the user message and emit JSON only. Do not invent facts.',
            'Schema: {"pages":[{"slug","title","kind","summary","aliases","importance"}],"edges":[{"fromSlug","toSlug","relation","weight"}]}.',
            'kind enum: person | place | item | event | concept | preference | rule.',
            'slug must be a kebab-case identifier in Thai or English (no spaces).',
            'Use importance 0.9 when the user explicitly says to remember it; 0.3 otherwise.',
            'Skip the message entirely (return empty arrays) if there is no durable fact.'
          ].join('\n')
        },
        { role: 'user', content: sanitized }
      ]
    });

    const parsed = parseJsonLoosely(raw);
    if (!parsed) {
      logger.warn({ namespace: input.namespace, preview: raw.slice(0, 200) }, 'extraction: model returned non-json');
      return { pages: 0, edges: 0 };
    }

    const validation = extractionSchema.safeParse(parsed);
    if (!validation.success) {
      logger.warn({ namespace: input.namespace, issues: validation.error.issues.slice(0, 3) }, 'extraction: schema mismatch');
      return { pages: 0, edges: 0 };
    }

    const { pages, edges } = validation.data;
    const slugToId = new Map<string, string>();

    for (const page of pages) {
      const persisted = await wikiBrainService.upsertPage({
        namespace: input.namespace,
        slug: page.slug,
        title: page.title,
        kind: page.kind as WikiKind,
        summary: page.summary,
        aliases: page.aliases,
        importance: page.importance
      });
      slugToId.set(page.slug, persisted.id);

      // Embed pages in parallel-friendly fire-forget mode. The embedding API
      // can be slow; we do not block the queue worker on it. If it fails we
      // simply lose semantic recall for that page until the next reflection.
      void embeddingService.upsert({
        namespace: input.namespace,
        sourceType: 'wiki_page',
        sourceId: persisted.id,
        content: [page.title, page.summary, page.aliases?.join(', ')].filter(Boolean).join('\n')
      }).catch((error) => logger.warn({ err: (error as Error).message }, 'embedding upsert failed (non-fatal)'));
    }

    let linkedEdges = 0;
    for (const edge of edges) {
      const fromId = slugToId.get(edge.fromSlug);
      const toId = slugToId.get(edge.toSlug);
      if (!fromId || !toId) continue;
      try {
        await wikiBrainService.linkPages({
          namespace: input.namespace,
          fromPageId: fromId,
          toPageId: toId,
          relation: edge.relation,
          weight: edge.weight
        });
        linkedEdges++;
      } catch (error) {
        logger.warn({ err: (error as Error).message, namespace: input.namespace }, 'edge link failed');
      }
    }

    logger.info({
      namespace: input.namespace,
      pages: pages.length,
      edges: linkedEdges,
      causationId: input.causationId
    }, 'extraction stored');

    return { pages: pages.length, edges: linkedEdges };
  }
}

export const extractionService = new ExtractionService();

function parseJsonLoosely(content: string): unknown {
  const trimmed = content.trim().replace(/^```(?:json)?\s*/i, '').replace(/```$/, '').trim();
  try {
    return JSON.parse(trimmed);
  } catch {
    const start = trimmed.indexOf('{');
    const end = trimmed.lastIndexOf('}');
    if (start < 0 || end < start) return null;
    try {
      return JSON.parse(trimmed.slice(start, end + 1));
    } catch {
      return null;
    }
  }
}
