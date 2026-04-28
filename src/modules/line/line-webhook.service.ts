import { auditService } from '../audit/audit.service.js';
import { queueService } from '../queue/queue.service.js';
import { lineSignatureService } from './line-signature.service.js';
import { logger } from '../../libs/logger.js';

export class LineWebhookService {
  async verifyAndEnqueue(input: { rawBody: Buffer; signature?: string }) {
    if (!lineSignatureService.verify(input.rawBody, input.signature)) {
      await auditService.log({
        action: 'line.webhook_signature_failed',
        resource: 'line_webhook'
      });
      return { ok: false as const, statusCode: 401, error: 'Invalid LINE signature' };
    }

    let body: { events?: unknown[] };
    try {
      body = JSON.parse(input.rawBody.toString('utf8')) as { events?: unknown[] };
    } catch {
      return { ok: false as const, statusCode: 400, error: 'Invalid JSON payload' };
    }

    const events = Array.isArray(body.events) ? body.events : [];
    await Promise.all(events.map((event) => queueService.enqueueLineEvent(event)));
    logger.info({ eventCount: events.length }, 'line webhook events enqueued');
    await auditService.log({
      action: 'line.webhook_enqueued',
      resource: 'line_webhook',
      metadata: { eventCount: events.length }
    });

    return { ok: true as const, eventCount: events.length };
  }
}

export const lineWebhookService = new LineWebhookService();
