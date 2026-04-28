import { Queue } from 'bullmq';
import { env } from '../../config/env.js';
import { redis } from '../../db/redis.js';

export type LineWebhookJob = {
  event: unknown;
  receivedAt: string;
};

export type MemoryJob =
  | {
      type: 'compact_conversation';
      ownerId: string;
      agentId: string;
      namespace: string;
    }
  | {
      type: 'extract_brain';
      ownerId: string;
      agentId: string;
      namespace: string;
      text: string;
      causationId?: string;
    }
  | {
      type: 'daily_reflection';
      namespace?: string; // when omitted, reflect every active namespace
    };

export type AgentJob = {
  type: 'background_plan';
  ownerId: string;
  agentId: string;
  namespace: string;
  input: unknown;
};

export type SummaryJob = {
  type: 'refresh_summary';
  ownerId: string;
  agentId: string;
  namespace: string;
  period: 'today' | 'month';
};

export const queueNames = {
  lineWebhookEvents: 'line_webhook_events',
  agentJobs: 'agent_jobs',
  memoryJobs: 'memory_jobs',
  summaryJobs: 'summary_jobs',
  notificationJobs: 'notification_jobs'
} as const;

export class QueueService {
  readonly lineWebhookEvents = new Queue<LineWebhookJob>(queueNames.lineWebhookEvents, {
    connection: redis,
    prefix: env.REDIS_QUEUE_PREFIX
  });

  readonly agentJobs = new Queue<AgentJob>(queueNames.agentJobs, {
    connection: redis,
    prefix: env.REDIS_QUEUE_PREFIX
  });

  readonly memoryJobs = new Queue<MemoryJob>(queueNames.memoryJobs, {
    connection: redis,
    prefix: env.REDIS_QUEUE_PREFIX
  });

  readonly summaryJobs = new Queue<SummaryJob>(queueNames.summaryJobs, {
    connection: redis,
    prefix: env.REDIS_QUEUE_PREFIX
  });

  async enqueueLineEvent(event: unknown) {
    return this.lineWebhookEvents.add(
      'line-event',
      { event, receivedAt: new Date().toISOString() },
      {
        attempts: 3,
        backoff: { type: 'exponential', delay: 1000 },
        removeOnComplete: { count: 1000 },
        removeOnFail: { count: 5000 }
      }
    );
  }

  async enqueueMemoryCompaction(input: { ownerId: string; agentId: string; namespace: string }) {
    return this.memoryJobs.add(
      'compact-conversation',
      { type: 'compact_conversation', ...input },
      {
        attempts: 3,
        backoff: { type: 'exponential', delay: 1000 },
        removeOnComplete: { count: 1000 },
        removeOnFail: { count: 5000 }
      }
    );
  }

  /** Schedule the nightly reflection cron. Idempotent — safe to call on every boot. */
  async ensureDailyReflectionCron() {
    // Scheduler keys are de-duped on the queue; running this twice does not
    // create two crons. Use the timezone of the operations team (UTC+7).
    return this.memoryJobs.upsertJobScheduler(
      'daily-reflection-cron',
      { pattern: '0 19 * * *', tz: 'UTC' }, // 02:00 ICT (UTC+7)
      {
        name: 'daily-reflection',
        data: { type: 'daily_reflection' },
        opts: {
          attempts: 1,
          removeOnComplete: { count: 30 },
          removeOnFail: { count: 100 }
        }
      }
    );
  }

  async enqueueBrainExtraction(input: {
    ownerId: string;
    agentId: string;
    namespace: string;
    text: string;
    causationId?: string;
  }) {
    if (!input.namespace) throw new Error('queueService.enqueueBrainExtraction: namespace required');
    return this.memoryJobs.add(
      'extract-brain',
      { type: 'extract_brain', ...input },
      {
        attempts: 2,
        backoff: { type: 'exponential', delay: 5000 },
        removeOnComplete: { count: 500 },
        removeOnFail: { count: 1000 }
      }
    );
  }
}

export const queueService = new QueueService();
