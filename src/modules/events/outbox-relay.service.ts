import { prisma } from '../../db/postgres.js';
import { logger } from '../../libs/logger.js';

export type OutboxPublisher = (input: { topic: string; payload: unknown }) => Promise<void>;

export class OutboxRelayService {
  constructor(private readonly publisher: OutboxPublisher) {}

  /**
   * Drains a batch of pending outbox rows. Failures bump `attempts` and store
   * the last error so a separate alerting job can pick them up; successful
   * rows are marked published with a wall-clock timestamp.
   *
   * Returns the number of rows processed so the caller can decide whether to
   * keep draining (e.g. when the batch was full).
   */
  async drainOnce(batchSize = 100): Promise<number> {
    const rows = await prisma.eventOutbox.findMany({
      where: { status: 'pending' },
      orderBy: { id: 'asc' },
      take: batchSize
    });
    if (rows.length === 0) return 0;

    for (const row of rows) {
      try {
        await this.publisher({ topic: row.topic, payload: row.payload });
        await prisma.eventOutbox.update({
          where: { id: row.id },
          data: { status: 'published', publishedAt: new Date(), lastError: null }
        });
      } catch (error) {
        const message = (error as Error).message;
        await prisma.eventOutbox.update({
          where: { id: row.id },
          data: { attempts: { increment: 1 }, lastError: message }
        });
        logger.error({ outboxId: row.id.toString(), topic: row.topic, err: message }, 'outbox publish failed');
      }
    }
    return rows.length;
  }
}

/** Default publisher used in dev when no broker is wired — logs and drops. */
export const consoleOutboxPublisher: OutboxPublisher = async ({ topic, payload }) => {
  logger.info({ topic, payload }, 'outbox event (console publisher)');
};
