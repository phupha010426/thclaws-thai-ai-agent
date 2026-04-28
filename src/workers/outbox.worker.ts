/**
 * Standalone process that drains the Postgres event_outbox into NATS
 * JetStream. Lives in its own container so a spike in LINE traffic cannot
 * starve the relay loop, and so we can scale or restart it independently.
 */
import { logger } from '../libs/logger.js';
import { outboxRelay } from '../modules/events/outbox-bridge.js';
import { natsPublisher } from '../modules/events/nats-publisher.js';

const RELAY_INTERVAL_MS = 1000;
const BATCH_SIZE = 200;

let stopRequested = false;

async function main() {
  logger.info({ batchSize: BATCH_SIZE, interval: RELAY_INTERVAL_MS }, 'outbox relay starting');

  while (!stopRequested) {
    try {
      const drained = await outboxRelay.drainOnce(BATCH_SIZE);
      if (drained === 0) {
        await sleep(RELAY_INTERVAL_MS);
      }
    } catch (err) {
      logger.error({ err }, 'outbox relay loop error');
      await sleep(RELAY_INTERVAL_MS * 5);
    }
  }

  logger.info('outbox relay shutting down');
  await natsPublisher.close().catch(() => undefined);
  process.exit(0);
}

function sleep(ms: number) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

process.on('SIGTERM', () => {
  logger.info('SIGTERM received');
  stopRequested = true;
});
process.on('SIGINT', () => {
  logger.info('SIGINT received');
  stopRequested = true;
});

main().catch((err) => {
  logger.error({ err }, 'outbox worker crashed');
  process.exit(1);
});
