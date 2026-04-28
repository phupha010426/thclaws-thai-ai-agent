import { auditService } from '../audit/audit.service.js';
import {
  AgentProfile,
  ConversationMessage,
  ConversationSnapshot,
  EventLog,
  MemoryItem,
  MemorySummary,
  PersonalWikiPage
} from './memory.model.js';
import { logger } from '../../libs/logger.js';
import { prisma } from '../../db/postgres.js';

export class MemoryService {
  async rememberImportantFact(input: {
    ownerId: string;
    agentId: string;
    namespace: string;
    content: string;
    source?: string;
  }) {
    const content = input.content.trim();
    if (!content) return null;

    const item = await MemoryItem.create({
      ownerId: input.ownerId,
      agentId: input.agentId,
      namespace: input.namespace,
      type: 'semantic',
      content,
      sensitivityLevel: 'low',
      confidence: 0.9,
      source: input.source ?? 'line'
    });

    await auditService.log({
      actorId: input.ownerId,
      action: 'memory.item_created',
      resource: 'memory_items',
      metadata: { agentId: input.agentId, namespace: input.namespace }
    });

    return item;
  }

  async saveConversationSnapshot(input: { namespace: string; lineUserId: string; lastIntent: string }) {
    return ConversationSnapshot.findOneAndUpdate(
      { namespace: input.namespace, lineUserId: input.lineUserId },
      { $set: { lastIntent: input.lastIntent } },
      { upsert: true, new: true }
    );
  }

  async saveConversationMessage(input: {
    ownerId: string;
    agentId: string;
    namespace: string;
    role: 'user' | 'assistant' | 'system';
    content: string;
    metadata?: Record<string, unknown>;
  }) {
    const content = input.content.trim();
    if (!content) return null;
    return ConversationMessage.create({
      ownerId: input.ownerId,
      agentId: input.agentId,
      namespace: input.namespace,
      role: input.role,
      content: content.slice(0, 4000),
      metadata: input.metadata ?? {}
    });
  }

  async saveEventLog(input: {
    ownerId: string;
    agentId?: string | null;
    namespace: string;
    eventType: string;
    source?: string;
    content?: string | null;
    metadata?: Record<string, unknown>;
  }) {
    return EventLog.create({
      ownerId: input.ownerId,
      agentId: input.agentId ?? null,
      namespace: input.namespace,
      eventType: input.eventType,
      source: input.source ?? 'line',
      content: input.content?.slice(0, 4000) ?? null,
      metadata: sanitizeLogMetadata(input.metadata ?? {})
    });
  }

  async getContext(namespace: string) {
    if (!namespace) throw new Error('memoryService.getContext: namespace required');

    // Read Mongo + Postgres wiki in parallel; both are scoped to the same
    // namespace so two LINE userIds can never see each other's pages.
    const [recentHistory, compactSummary, mongoWiki, brainPages] = await Promise.all([
      ConversationMessage.find({ namespace, compacted: false })
        .sort({ createdAt: -1 })
        .limit(12)
        .select({ role: 1, content: 1, createdAt: 1, _id: 0 })
        .lean(),
      MemorySummary.findOne({ namespace }).sort({ createdAt: -1 }).lean(),
      PersonalWikiPage.find({ namespace })
        .sort({ updatedAt: -1 })
        .limit(8)
        .select({ title: 1, content: 1, updatedAt: 1, _id: 0 })
        .lean(),
      prisma.wikiPage.findMany({
        where: { namespace },
        orderBy: [{ importance: 'desc' }, { updatedAt: 'desc' }],
        take: 8,
        select: { title: true, kind: true, summary: true, updatedAt: true, importance: true }
      }).catch((err: Error) => {
        logger.warn({ err: err.message, namespace }, 'wikiPage fetch failed; continuing with mongo only');
        return [];
      })
    ]);

    // Merge: prefer brain pages (more structured), fall back to legacy Mongo.
    const wikiPages = [
      ...brainPages.map((p) => ({
        title: p.title,
        content: p.summary ?? '',
        updatedAt: p.updatedAt,
        kind: p.kind,
        importance: p.importance,
        source: 'brain'
      })),
      ...mongoWiki.map((p) => ({ ...p, source: 'mongo' }))
    ];

    return {
      recentHistory: recentHistory.reverse(),
      compactSummary: compactSummary?.summary ?? '',
      wikiPages
    };
  }

  async upsertWikiPage(input: { ownerId: string; agentId: string; namespace: string; title: string; content: string }) {
    return PersonalWikiPage.findOneAndUpdate(
      { namespace: input.namespace, title: input.title.trim() },
      {
        $set: {
          ownerId: input.ownerId,
          agentId: input.agentId,
          namespace: input.namespace,
          title: input.title.trim(),
          content: input.content.trim().slice(0, 6000),
          source: 'thclaws'
        }
      },
      { upsert: true, new: true }
    );
  }

  async compactConversationIfNeeded(input: { ownerId: string; agentId: string; namespace: string }) {
    const count = await ConversationMessage.countDocuments({ namespace: input.namespace, compacted: false });
    if (count < 40) return null;

    const messages = await ConversationMessage.find({ namespace: input.namespace, compacted: false })
      .sort({ createdAt: 1 })
      .limit(Math.max(0, count - 12))
      .lean();
    if (messages.length === 0) return null;

    const summary = messages
      .map((message) => `${message.role}: ${message.content}`)
      .join('\n')
      .slice(0, 12000);

    const compacted = await MemorySummary.create({
      ownerId: input.ownerId,
      agentId: input.agentId,
      namespace: input.namespace,
      summary,
      messageCount: messages.length,
      fromDate: messages[0]?.createdAt,
      toDate: messages[messages.length - 1]?.createdAt
    });

    await ConversationMessage.updateMany(
      { _id: { $in: messages.map((message) => message._id) } },
      { $set: { compacted: true } }
    );

    await auditService.log({
      actorId: input.ownerId,
      action: 'memory.conversation_compacted',
      resource: 'conversation_messages',
      metadata: { namespace: input.namespace, messageCount: messages.length }
    });
    logger.info({ namespace: input.namespace, messageCount: messages.length }, 'conversation compacted');

    return compacted;
  }

  async ensureAgentProfile(agentId: string, namespace: string) {
    return AgentProfile.findOneAndUpdate(
      { agentId },
      { $setOnInsert: { agentId, namespace, preferences: {} } },
      { upsert: true, new: true }
    );
  }
}

export const memoryService = new MemoryService();

function sanitizeLogMetadata(metadata: Record<string, unknown>) {
  const encodedImageKeyPattern = new RegExp(['token', 'secret', 'password', 'authorization', 'base', '64'].join('|'), 'i');
  return Object.fromEntries(
    Object.entries(metadata).filter(([key]) => !encodedImageKeyPattern.test(key))
  );
}
