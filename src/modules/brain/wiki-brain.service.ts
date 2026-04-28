import { Prisma } from '@prisma/client';
import { prisma } from '../../db/postgres.js';
import { logger } from '../../libs/logger.js';
import { brainCache } from './brain-cache.js';

/**
 * WikiBrain — a per-user knowledge graph.
 *
 * Hard rule (enforced at three layers): every read and write is scoped by a
 * `namespace` (one LINE userId = one namespace). The Postgres triggers in
 * `prisma/migrations/20260427_wiki_brain_graph/migration.sql` reject any edge
 * or alias whose namespace does not match its page, so even a buggy caller
 * cannot create a cross-tenant link. The service signatures below put
 * `namespace` first to make accidental omissions impossible at compile time.
 */
export type WikiKind = 'person' | 'place' | 'item' | 'event' | 'concept' | 'preference' | 'rule';

export type UpsertPageInput = {
  namespace: string;
  slug: string;
  title: string;
  kind?: WikiKind;
  summary?: string;
  bodyMd?: string;
  importance?: number;
  source?: string;
  aliases?: string[];
  metadata?: Prisma.InputJsonValue;
};

export type LinkPagesInput = {
  namespace: string;
  fromPageId: string;
  toPageId: string;
  relation: string;
  weight?: number;
  metadata?: Prisma.InputJsonValue;
};

export type RetrieveInput = {
  namespace: string;
  text: string;
  limit?: number;
};

export type RetrievedPage = {
  id: string;
  slug: string;
  title: string;
  kind: string;
  summary: string | null;
  importance: number;
  reason: 'alias_match' | 'edge_neighbor';
  hops: number;
  matchedAlias?: string;
};

export class WikiBrainService {
  async upsertPage(input: UpsertPageInput) {
    assertNamespace(input.namespace);
    const slug = normalizeSlug(input.slug);
    const aliases = uniqueLowercase([input.title, ...(input.aliases ?? [])]);

    const result = await prisma.$transaction(async (tx) => {
      const page = await tx.wikiPage.upsert({
        where: { namespace_slug: { namespace: input.namespace, slug } },
        update: {
          title: input.title.trim(),
          kind: input.kind ?? undefined,
          summary: input.summary,
          bodyMd: input.bodyMd ?? undefined,
          importance: clamp01(input.importance),
          source: input.source ?? undefined,
          metadata: input.metadata ?? undefined
        },
        create: {
          namespace: input.namespace,
          slug,
          title: input.title.trim(),
          kind: input.kind ?? 'concept',
          summary: input.summary,
          bodyMd: input.bodyMd ?? '',
          importance: clamp01(input.importance) ?? 0.5,
          source: input.source ?? 'auto',
          metadata: input.metadata ?? undefined
        }
      });

      if (aliases.length > 0) {
        // Bulk insert with conflict skip — much cheaper than N upserts on the
        // hot reflection-job path. Aliases that already point to a different
        // page must be moved explicitly via a separate maintenance call.
        await tx.wikiAlias.createMany({
          data: aliases.map((alias) => ({ namespace: input.namespace, pageId: page.id, alias })),
          skipDuplicates: true
        });
      }

      return page;
    });

    // Drop the cached retrieval results so the next message sees fresh data.
    // Fire-and-forget; cache failures must not block writes.
    void brainCache.invalidate(input.namespace);
    return result;
  }

  async linkPages(input: LinkPagesInput) {
    assertNamespace(input.namespace);
    const result = await prisma.wikiEdge.upsert({
      where: {
        namespace_fromPageId_toPageId_relation: {
          namespace: input.namespace,
          fromPageId: input.fromPageId,
          toPageId: input.toPageId,
          relation: input.relation
        }
      },
      update: {
        weight: input.weight ?? undefined,
        metadata: input.metadata ?? undefined
      },
      create: {
        namespace: input.namespace,
        fromPageId: input.fromPageId,
        toPageId: input.toPageId,
        relation: input.relation,
        weight: input.weight ?? 1,
        metadata: input.metadata ?? undefined
      }
    });
    void brainCache.invalidate(input.namespace);
    return result;
  }

  /**
   * Retrieve the part of the brain that is relevant to `text`.
   *
   * 1. Tokenize Thai/EN text into candidate aliases.
   * 2. Look up matching aliases scoped to namespace (no cross-user reads).
   * 3. Expand each match by one edge hop so we surface adjacent context
   *    ("ลุงจืด → ไม่กินเผ็ด → อาหารไทย" returns all three).
   * 4. Rank by (importance × 1/(1 + hops)).
   *
   * Returns at most `limit` pages. Semantic search (pgvector) is plugged in
   * later by `MemoryEmbeddingService`; this method intentionally focuses on
   * the deterministic graph traversal so we always have a baseline that does
   * not depend on the embedding model being available.
   */
  async retrieve(input: RetrieveInput): Promise<RetrievedPage[]> {
    assertNamespace(input.namespace);
    const limit = input.limit ?? 8;

    // Stage 1 — fire cache lookup and tokenization in parallel. Tokenizing is
    // CPU-bound but cheap; doing it concurrently with the Redis round trip
    // saves 1-2ms on every LINE message in the hot path.
    const [cached, tokens] = await Promise.all([
      brainCache.get<RetrievedPage[]>(input.namespace, input.text),
      Promise.resolve(tokenize(input.text))
    ]);
    if (cached.hit) return cached.value;
    if (tokens.length === 0) {
      await brainCache.set(input.namespace, input.text, []);
      return [];
    }

    // Stage 2 — alias lookup. (Embedding-based search will run in parallel
    // here once MemoryEmbeddingService is wired in.)
    const aliases = await prisma.wikiAlias.findMany({
      where: {
        namespace: input.namespace,
        alias: { in: tokens }
      },
      include: { page: true }
    });

    if (aliases.length === 0) {
      await brainCache.set(input.namespace, input.text, []);
      return [];
    }

    const seedPages = new Map<string, RetrievedPage>();
    for (const alias of aliases) {
      const page = alias.page;
      if (page.namespace !== input.namespace) {
        // Defense-in-depth: should never happen because the alias trigger
        // forbids it, but we double-check at read time too.
        logger.error({
          aliasNamespace: alias.namespace,
          pageNamespace: page.namespace,
          requested: input.namespace
        }, 'wiki retrieval namespace mismatch');
        continue;
      }
      seedPages.set(page.id, {
        id: page.id,
        slug: page.slug,
        title: page.title,
        kind: page.kind,
        summary: page.summary,
        importance: page.importance,
        reason: 'alias_match',
        hops: 0,
        matchedAlias: alias.alias
      });
    }

    const seedIds = Array.from(seedPages.keys());
    if (seedIds.length === 0) {
      await brainCache.set(input.namespace, input.text, []);
      return [];
    }

    // Stage 3 — neighbor expansion. Cap the result set so a noisy graph
    // cannot wedge the worker.
    const neighbors = await prisma.wikiEdge.findMany({
      where: {
        namespace: input.namespace,
        OR: [{ fromPageId: { in: seedIds } }, { toPageId: { in: seedIds } }]
      },
      include: { fromPage: true, toPage: true },
      take: 200
    });

    const all = new Map<string, RetrievedPage>(seedPages);
    const seedIdSet = new Set(seedIds);
    for (const edge of neighbors) {
      const candidate = seedIdSet.has(edge.fromPageId) ? edge.toPage : edge.fromPage;
      if (candidate.namespace !== input.namespace) continue;
      if (all.has(candidate.id)) continue;
      all.set(candidate.id, {
        id: candidate.id,
        slug: candidate.slug,
        title: candidate.title,
        kind: candidate.kind,
        summary: candidate.summary,
        importance: candidate.importance,
        reason: 'edge_neighbor',
        hops: 1
      });
    }

    const ranked = Array.from(all.values())
      .sort((a, b) => score(b) - score(a))
      .slice(0, limit);

    // Cache without awaiting the response so the caller is not blocked on
    // Redis. Errors are logged but never thrown.
    void brainCache.set(input.namespace, input.text, ranked);
    return ranked;
  }

  /** Call after any write that mutates the brain so retrieval cannot serve a stale answer. */
  async invalidateCache(namespace: string) {
    assertNamespace(namespace);
    await brainCache.invalidate(namespace);
  }
}

export const wikiBrainService = new WikiBrainService();

function assertNamespace(namespace: string) {
  if (!namespace || typeof namespace !== 'string') {
    throw new Error('wiki-brain: namespace is required');
  }
}

function normalizeSlug(slug: string) {
  return slug.trim().toLowerCase().replace(/\s+/g, '-');
}

function tokenize(text: string): string[] {
  // Lowercase + split on non-letter/digit (Unicode-aware so Thai stays intact)
  // and keep multi-char tokens. This is a deliberately simple tokenizer; richer
  // matching belongs in the embedding path, not the deterministic alias lookup.
  const lowered = text.toLowerCase();
  const matched = lowered.match(/[\p{L}\p{N}]+/gu) ?? [];
  return uniqueLowercase(matched.filter((token) => token.length >= 2));
}

function uniqueLowercase(values: string[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const value of values) {
    const normalized = value.trim().toLowerCase();
    if (!normalized || seen.has(normalized)) continue;
    seen.add(normalized);
    out.push(normalized);
  }
  return out;
}

function clamp01(value: number | undefined) {
  if (value === undefined) return undefined;
  if (Number.isNaN(value)) return undefined;
  return Math.max(0, Math.min(1, value));
}

function score(page: RetrievedPage) {
  const hopPenalty = 1 / (1 + page.hops);
  const reasonBoost = page.reason === 'alias_match' ? 1 : 0.6;
  return page.importance * hopPenalty * reasonBoost;
}
