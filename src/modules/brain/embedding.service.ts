import { Prisma } from '@prisma/client';
import { prisma } from '../../db/postgres.js';
import { logger } from '../../libs/logger.js';
import { smlGatewayService } from '../agents/sml-gateway.service.js';
import { env } from '../../config/env.js';
import { redactPii } from './redaction.js';

/**
 * MemoryEmbedding service. Stores 1024-dim embeddings produced by the
 * SMLGateway (BGE-M3 / Cohere v3 multilingual class) inside Postgres pgvector.
 *
 * STRICT: every public method requires `namespace`. Vector queries are scoped
 * by `WHERE namespace = $1` before the ANN operator (`<=>`) runs, so the
 * HNSW index never returns a row from a different LINE userId.
 */
export type EmbedSourceType = 'memory_item' | 'wiki_page' | 'summary' | 'message';

export type UpsertEmbeddingInput = {
  namespace: string;
  sourceType: EmbedSourceType;
  sourceId: string;
  content: string;
  metadata?: Prisma.InputJsonValue;
};

export type SearchInput = {
  namespace: string;
  query: string;
  limit?: number;
  minScore?: number;
  sourceTypes?: EmbedSourceType[];
};

export type SearchHit = {
  id: string;
  sourceType: string;
  sourceId: string;
  content: string;
  similarity: number; // 1 - cosine_distance
};

export class EmbeddingService {
  async upsert(input: UpsertEmbeddingInput) {
    assertNamespace(input.namespace);
    // Strip identifier-shaped tokens before they reach the embedding model
    // and before they are stored in the vector table.
    const cleaned = redactPii(input.content);
    const vector = await smlGatewayService.embed({
      model: env.EMBEDDING_MODEL,
      fallbackModels: env.EMBEDDING_FALLBACK_MODELS.split(',').map((m) => m.trim()).filter(Boolean),
      text: cleaned
    });
    const literal = toVectorLiteral(vector);

    // Prisma cannot type the vector column natively, so we use a parameterized
    // raw upsert. Namespace is bound; never interpolate it into SQL.
    await prisma.$executeRaw`
      INSERT INTO memory_embeddings (id, namespace, "sourceType", "sourceId", content, embedding, metadata, "createdAt", "updatedAt")
      VALUES (gen_random_uuid()::text, ${input.namespace}, ${input.sourceType}, ${input.sourceId}, ${cleaned}, ${literal}::vector, ${input.metadata ?? null}::jsonb, NOW(), NOW())
      ON CONFLICT ("namespace", "sourceType", "sourceId")
      DO UPDATE SET content = EXCLUDED.content, embedding = EXCLUDED.embedding, metadata = EXCLUDED.metadata, "updatedAt" = NOW()
    `;
    logger.info({ namespace: input.namespace, sourceType: input.sourceType, sourceId: input.sourceId }, 'memory embedding upserted');
  }

  async search(input: SearchInput): Promise<SearchHit[]> {
    assertNamespace(input.namespace);
    const limit = Math.min(Math.max(input.limit ?? 6, 1), 50);
    const vector = await smlGatewayService.embed({
      model: env.EMBEDDING_MODEL,
      fallbackModels: env.EMBEDDING_FALLBACK_MODELS.split(',').map((m) => m.trim()).filter(Boolean),
      text: input.query
    });
    const literal = toVectorLiteral(vector);

    // Filter by namespace BEFORE the ANN operator so HNSW never traverses
    // other tenants' rows. We also filter by sourceType when provided.
    const sourceTypeFilter = input.sourceTypes && input.sourceTypes.length > 0
      ? Prisma.sql`AND "sourceType" = ANY(${input.sourceTypes}::text[])`
      : Prisma.empty;

    const rows = await prisma.$queryRaw<Array<{ id: string; sourceType: string; sourceId: string; content: string; distance: number }>>`
      SELECT id, "sourceType", "sourceId", content, (embedding <=> ${literal}::vector) AS distance
      FROM memory_embeddings
      WHERE namespace = ${input.namespace}
      ${sourceTypeFilter}
      ORDER BY embedding <=> ${literal}::vector
      LIMIT ${limit}
    `;

    const minScore = input.minScore ?? 0;
    return rows
      .map((row) => ({
        id: row.id,
        sourceType: row.sourceType,
        sourceId: row.sourceId,
        content: row.content,
        similarity: 1 - Number(row.distance)
      }))
      .filter((hit) => hit.similarity >= minScore);
  }
}

export const embeddingService = new EmbeddingService();

function assertNamespace(namespace: string) {
  if (!namespace || typeof namespace !== 'string') {
    throw new Error('embedding service: namespace required');
  }
}

function toVectorLiteral(vector: number[]): string {
  // pgvector accepts the textual literal '[1.0,2.0,...]'. We do not log the
  // value because user-provided text content can be inferred from embeddings.
  return `[${vector.map((v) => Number.isFinite(v) ? v : 0).join(',')}]`;
}
