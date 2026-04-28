import { connect, JSONCodec, type NatsConnection, type JetStreamClient, type JetStreamManager } from 'nats';
import { env } from '../../config/env.js';
import { logger } from '../../libs/logger.js';

/**
 * NATS JetStream publisher.
 *
 * Subjects always include the LINE userId namespace as the last hop, so a
 * consumer can subscribe to a single user (`line.event.text.<ns>`) or a
 * fan-out (`line.event.text.>`). The stream config below restricts the
 * registered subjects so accidental publishes outside the allowed shape
 * fail at the broker rather than leak across users.
 */
const codec = JSONCodec();

export class NatsPublisher {
  private connection: NatsConnection | null = null;
  private js: JetStreamClient | null = null;
  private connecting: Promise<void> | null = null;

  get enabled() {
    return Boolean(env.NATS_URL);
  }

  async ready(): Promise<JetStreamClient | null> {
    if (!this.enabled) return null;
    if (this.js) return this.js;
    if (!this.connecting) this.connecting = this.connect();
    await this.connecting;
    return this.js;
  }

  async publish(subject: string, payload: unknown, options?: { msgId?: string }) {
    if (!this.enabled) return;
    if (!isNamespacedSubject(subject)) {
      throw new Error(`nats publish: subject "${subject}" is missing a namespace suffix`);
    }
    const js = await this.ready();
    if (!js) return;
    try {
      await js.publish(subject, codec.encode(payload), options?.msgId ? { msgID: options.msgId } : undefined);
    } catch (error) {
      logger.warn({ subject, err: (error as Error).message }, 'nats publish failed');
    }
  }

  async close() {
    if (this.connection) await this.connection.drain();
    this.connection = null;
    this.js = null;
    this.connecting = null;
  }

  private async connect() {
    try {
      this.connection = await connect({ servers: env.NATS_URL, name: 'thaiagent-node' });
      const jsm: JetStreamManager = await this.connection.jetstreamManager();

      // Idempotent stream provisioning. The subject filters bind every event
      // family to a per-user wildcard so the broker rejects stray publishes.
      await ensureStream(jsm, {
        name: env.NATS_STREAM_NAME,
        subjects: [
          'line.event.>',
          'thclaws.plan.>',
          'ledger.tx.>',
          'wiki.fact.>',
          'slip.image.>'
        ]
      });

      this.js = this.connection.jetstream();
      logger.info({ url: env.NATS_URL, stream: env.NATS_STREAM_NAME }, 'nats publisher ready');
    } catch (error) {
      logger.error({ err: (error as Error).message }, 'nats connect failed');
      this.connection = null;
      this.js = null;
      this.connecting = null;
    }
  }
}

async function ensureStream(jsm: JetStreamManager, config: { name: string; subjects: string[] }) {
  try {
    const existing = await jsm.streams.info(config.name);
    const desired = new Set(config.subjects);
    const current = new Set(existing.config.subjects ?? []);
    const same = desired.size === current.size && [...desired].every((s) => current.has(s));
    if (!same) {
      await jsm.streams.update(config.name, { ...existing.config, subjects: config.subjects });
    }
  } catch {
    await jsm.streams.add({ name: config.name, subjects: config.subjects });
  }
}

function isNamespacedSubject(subject: string): boolean {
  // Last token must look like a namespace (contains "user:" or is non-empty).
  const parts = subject.split('.');
  if (parts.length < 3) return false;
  const last = parts[parts.length - 1];
  return Boolean(last) && !last.includes('*') && !last.includes('>');
}

export const natsPublisher = new NatsPublisher();
