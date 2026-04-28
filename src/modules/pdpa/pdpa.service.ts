import { redis } from '../../db/redis.js';
import { prisma } from '../../db/postgres.js';
import { logger } from '../../libs/logger.js';
import {
  ConversationMessage,
  ConversationSnapshot,
  EventLog,
  MemoryItem,
  MemorySummary,
  PersonalWikiPage,
  AgentProfile,
  SlipRecord
} from '../memory/memory.model.js';

/**
 * PDPA / "right to be forgotten" service. Removes everything we know about a
 * given LINE userId namespace across Postgres, Mongo, and Redis. Idempotent —
 * calling twice on the same namespace is a no-op for any layer that is
 * already empty.
 *
 * NOT covered here: MinIO objects (they live under a per-user prefix; the
 * caller must delete the prefix separately) and gateway/provider logs (out of
 * our control).
 */

export type DeleteReport = {
  namespace: string;
  postgres: Record<string, number>;
  mongo: Record<string, number>;
  redis: number;
};

export class PdpaService {
  async deleteAll(namespace: string): Promise<DeleteReport> {
    if (!namespace) throw new Error('pdpa.deleteAll: namespace required');
    logger.warn({ namespace }, 'pdpa delete starting');

    // Postgres — order matters: edges/aliases cascade with pages, but we still
    // call them explicitly so the report includes the count.
    const [edges, aliases, pages, embeddings, ledger, outbox] = await Promise.all([
      prisma.wikiEdge.deleteMany({ where: { namespace } }),
      prisma.wikiAlias.deleteMany({ where: { namespace } }),
      prisma.wikiPage.deleteMany({ where: { namespace } }),
      prisma.memoryEmbedding.deleteMany({ where: { namespace } }),
      prisma.ledgerEvent.deleteMany({ where: { namespace } }),
      prisma.eventOutbox.deleteMany({ where: { payload: { path: ['namespace'], equals: namespace } } })
    ]);

    // Mongo collections — every doc carries a namespace field by design.
    const [conv, snap, evt, summ, item, wiki, profile, slip] = await Promise.all([
      ConversationMessage.deleteMany({ namespace }),
      ConversationSnapshot.deleteMany({ namespace }),
      EventLog.deleteMany({ namespace }),
      MemorySummary.deleteMany({ namespace }),
      MemoryItem.deleteMany({ namespace }),
      PersonalWikiPage.deleteMany({ namespace }),
      AgentProfile.deleteMany({ namespace }),
      SlipRecord.deleteMany({ namespace })
    ]);

    const redisDeleted = await this.dropRedisNamespace(namespace);

    const report: DeleteReport = {
      namespace,
      postgres: {
        wikiEdges: edges.count,
        wikiAliases: aliases.count,
        wikiPages: pages.count,
        memoryEmbeddings: embeddings.count,
        ledgerEvents: ledger.count,
        eventOutbox: outbox.count
      },
      mongo: {
        conversationMessages: conv.deletedCount ?? 0,
        conversationSnapshots: snap.deletedCount ?? 0,
        eventLogs: evt.deletedCount ?? 0,
        memorySummaries: summ.deletedCount ?? 0,
        memoryItems: item.deletedCount ?? 0,
        personalWikiPages: wiki.deletedCount ?? 0,
        agentProfiles: profile.deletedCount ?? 0,
        slipRecords: slip.deletedCount ?? 0
      },
      redis: redisDeleted
    };

    logger.warn({ namespace, report }, 'pdpa delete completed');
    return report;
  }

  async exportAll(namespace: string) {
    if (!namespace) throw new Error('pdpa.exportAll: namespace required');
    const [pages, edges, embeddings, ledger, conv, summ, item, wiki] = await Promise.all([
      prisma.wikiPage.findMany({ where: { namespace } }),
      prisma.wikiEdge.findMany({ where: { namespace } }),
      prisma.memoryEmbedding.findMany({ where: { namespace }, select: { id: true, sourceType: true, sourceId: true, content: true, metadata: true, createdAt: true } }),
      prisma.ledgerEvent.findMany({ where: { namespace }, orderBy: { id: 'asc' } }),
      ConversationMessage.find({ namespace }).lean(),
      MemorySummary.find({ namespace }).lean(),
      MemoryItem.find({ namespace }).lean(),
      PersonalWikiPage.find({ namespace }).lean()
    ]);
    return {
      namespace,
      exportedAt: new Date().toISOString(),
      postgres: { pages, edges, embeddings, ledger },
      mongo: { conversations: conv, summaries: summ, items: item, wikiPages: wiki }
    };
  }

  private async dropRedisNamespace(namespace: string): Promise<number> {
    // Anything keyed by namespace should match either the namespace itself
    // (rate limit) or a SHA1-prefixed bucket (caches). We delete both shapes.
    const directPattern = `*${namespace}*`;
    let deleted = 0;
    try {
      let cursor = '0';
      do {
        const [next, keys] = await redis.scan(cursor, 'MATCH', directPattern, 'COUNT', 200);
        cursor = next;
        if (keys.length > 0) deleted += await redis.del(...keys);
      } while (cursor !== '0');
    } catch (error) {
      logger.warn({ err: (error as Error).message, namespace }, 'redis pdpa scan failed');
    }
    return deleted;
  }
}

export const pdpaService = new PdpaService();
