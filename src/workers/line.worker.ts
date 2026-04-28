import { Worker } from 'bullmq';
import { env } from '../config/env.js';
import { redis } from '../db/redis.js';
import { connectMongo } from '../db/mongo.js';
import { logger } from '../libs/logger.js';
import { lineEventService } from '../modules/line/line-event.service.js';
import { memoryService } from '../modules/memory/memory.service.js';
import { extractionService } from '../modules/brain/extraction.service.js';
import { reflectionService } from '../modules/brain/reflection.service.js';
import { queueNames, queueService, type LineWebhookJob, type MemoryJob } from '../modules/queue/queue.service.js';

await connectMongo();
await queueService.ensureDailyReflectionCron().catch((err) => logger.warn({ err }, 'failed to install reflection cron'));

const lineWorker = new Worker<LineWebhookJob>(
  queueNames.lineWebhookEvents,
  async (job) => {
    logger.info({ jobId: job.id, team: 'line' }, 'LINE job started');
    await lineEventService.process(job.data.event);
  },
  { connection: redis, concurrency: 10, prefix: env.REDIS_QUEUE_PREFIX }
);

const memoryWorker = new Worker<MemoryJob>(
  queueNames.memoryJobs,
  async (job) => {
    logger.info({ jobId: job.id, team: 'memory', type: job.data.type, namespace: job.data.namespace }, 'memory job started');
    if (job.data.type === 'compact_conversation') {
      await memoryService.compactConversationIfNeeded(job.data);
    } else if (job.data.type === 'extract_brain') {
      try {
        await extractionService.extractAndStore(job.data);
      } catch (err) {
        logger.error({ err, namespace: job.data.namespace }, 'brain extraction failed');
      }
    } else if (job.data.type === 'daily_reflection') {
      try {
        if (job.data.namespace) {
          await reflectionService.reflectNamespace(job.data.namespace);
        } else {
          await reflectionService.reflectAllActive();
        }
      } catch (err) {
        logger.error({ err }, 'reflection job failed');
      }
    }
  },
  { connection: redis, concurrency: 5, prefix: env.REDIS_QUEUE_PREFIX }
);

lineWorker.on('completed', (job) => logger.info({ jobId: job.id, team: 'line' }, 'LINE job completed'));
lineWorker.on('failed', (job, err) => logger.error({ jobId: job?.id, team: 'line', err }, 'LINE job failed'));
memoryWorker.on('completed', (job) => logger.info({ jobId: job.id, team: 'memory' }, 'memory job completed'));
memoryWorker.on('failed', (job, err) => logger.error({ jobId: job?.id, team: 'memory', err }, 'memory job failed'));

// Graceful shutdown: stop accepting new jobs, finish in-flight ones, then exit.
async function shutdown(signal: string) {
  logger.info({ signal }, 'worker shutting down');
  await Promise.allSettled([lineWorker.close(), memoryWorker.close()]);
  process.exit(0);
}
process.on('SIGTERM', () => void shutdown('SIGTERM'));
process.on('SIGINT', () => void shutdown('SIGINT'));
