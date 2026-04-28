import { Prisma } from '@prisma/client';
import { prisma } from '../../db/postgres.js';
import { logger } from '../../libs/logger.js';
import { smlGatewayCache } from '../agents/sml-gateway-cache.js';
import { brainCache } from '../brain/brain-cache.js';

export type LedgerEventType =
  | 'tx.recorded'
  | 'tx.reversed'
  | 'budget.set'
  | 'goal.advanced'
  | 'slip.attached';

export type LedgerEventInput = {
  namespace: string;
  userId: string;
  type: LedgerEventType;
  payload: Prisma.InputJsonValue;
  correlationId?: string;
  causationId?: string;
  /**
   * NATS subject to publish via the outbox. If omitted the event is logged
   * to ledger_events but no outbox row is written. Use this when you do not
   * yet need fanout but want the audit trail.
   */
  outboxTopic?: string;
};

export class LedgerEventService {
  /**
   * Append a domain event and (optionally) enqueue an outbox row inside the
   * same DB transaction so consumers cannot observe the event without seeing
   * the matching state mutation, and vice versa.
   */
  async append(input: LedgerEventInput) {
    return prisma.$transaction(async (tx) => {
      const event = await tx.ledgerEvent.create({
        data: {
          namespace: input.namespace,
          userId: input.userId,
          type: input.type,
          payload: input.payload,
          correlationId: input.correlationId,
          causationId: input.causationId
        }
      });

      if (input.outboxTopic) {
        await tx.eventOutbox.create({
          data: {
            topic: input.outboxTopic,
            payload: {
              eventId: event.id.toString(),
              namespace: input.namespace,
              userId: input.userId,
              type: input.type,
              correlationId: input.correlationId,
              causationId: input.causationId,
              occurredAt: event.occurredAt.toISOString(),
              data: input.payload
            }
          }
        });
      }

      logger.info({
        eventId: event.id.toString(),
        type: input.type,
        namespace: input.namespace,
        outboxTopic: input.outboxTopic
      }, 'ledger event appended');

      return event;
    }).then((event) => {
      // Drop cached LLM summaries + brain retrieval results now that the
      // user's ledger has changed; otherwise "สรุปวันนี้" can return a stale
      // total for up to TTL seconds. Fire-and-forget — cache failures must
      // not block the write.
      void smlGatewayCache.invalidate(input.namespace);
      void brainCache.invalidate(input.namespace);
      return event;
    });
  }

  /** Replay events for a namespace ordered by id. Use to rebuild projections. */
  async replay(namespace: string, fromId = 0n, batchSize = 500) {
    return prisma.ledgerEvent.findMany({
      where: { namespace, id: { gt: fromId } },
      orderBy: { id: 'asc' },
      take: batchSize
    });
  }
}

export const ledgerEventService = new LedgerEventService();
