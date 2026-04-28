import { logger } from '../../libs/logger.js';
import { ConversationMessage } from '../memory/memory.model.js';
import { redactPii } from './redaction.js';
import { extractionService } from './extraction.service.js';

/**
 * Nightly reflection. For every active namespace, summarise yesterday's
 * conversation into a few wiki facts. Replaces the noisy per-message
 * extraction path with a single high-quality LLM call per user.
 *
 * SCOPE: namespaces are processed one at a time and kept entirely separate;
 * no aggregation across LINE userIds.
 */
export class ReflectionService {
  async reflectNamespace(namespace: string, sinceHoursAgo = 24): Promise<{ ok: boolean; pages: number; edges: number }> {
    if (!namespace) throw new Error('reflection: namespace required');
    const since = new Date(Date.now() - sinceHoursAgo * 60 * 60 * 1000);

    const messages = await ConversationMessage.find({ namespace, createdAt: { $gte: since } })
      .sort({ createdAt: 1 })
      .limit(200)
      .lean();
    if (messages.length === 0) return { ok: true, pages: 0, edges: 0 };

    // Concatenate up to 6000 chars and redact identifiers before handing to
    // the extraction LLM. We deliberately do not include role labels because
    // the extractor does not use them.
    const blob = redactPii(messages.map((m) => m.content).join('\n').slice(0, 6000));
    try {
      const result = await extractionService.extractAndStore({
        namespace,
        ownerId: messages[0]?.ownerId ?? 'unknown',
        agentId: messages[0]?.agentId,
        text: blob
      });
      logger.info({ namespace, pages: result.pages, edges: result.edges }, 'reflection cycle complete');
      return { ok: true, ...result };
    } catch (err) {
      logger.error({ err: (err as Error).message, namespace }, 'reflection failed');
      return { ok: false, pages: 0, edges: 0 };
    }
  }

  /**
   * Discover every active namespace from yesterday's conversation messages
   * and run reflectNamespace on each. Intended to be triggered by a cron
   * BullMQ job once per day (UTC+7 night, when traffic is lowest).
   */
  async reflectAllActive(sinceHoursAgo = 24): Promise<{ namespaces: number; success: number }> {
    const since = new Date(Date.now() - sinceHoursAgo * 60 * 60 * 1000);
    const namespaces = await ConversationMessage.distinct('namespace', { createdAt: { $gte: since } });
    let success = 0;
    for (const ns of namespaces) {
      if (typeof ns !== 'string' || !ns) continue;
      const result = await this.reflectNamespace(ns, sinceHoursAgo);
      if (result.ok) success++;
    }
    logger.info({ namespaces: namespaces.length, success }, 'reflection batch complete');
    return { namespaces: namespaces.length, success };
  }
}

export const reflectionService = new ReflectionService();
