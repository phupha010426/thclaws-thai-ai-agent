import { logger } from '../../libs/logger.js';
import { natsPublisher } from './nats-publisher.js';
import { OutboxRelayService, type OutboxPublisher } from './outbox-relay.service.js';

/**
 * Drains the Postgres event_outbox into NATS JetStream. Subjects must include
 * the namespace tail so subscribers can fan out per LINE userId without ever
 * seeing another user's payload.
 */
export const natsOutboxPublisher: OutboxPublisher = async ({ topic, payload }) => {
  const data = payload as { namespace?: string } | null;
  const namespace = data?.namespace;
  if (!namespace) {
    logger.error({ topic }, 'outbox payload missing namespace; refusing to publish');
    throw new Error('outbox payload missing namespace');
  }
  const subject = topic.includes('.') ? `${topic}.${namespace}` : `${topic}.${namespace}`;
  await natsPublisher.publish(subject, payload);
};

export const outboxRelay = new OutboxRelayService(natsOutboxPublisher);
