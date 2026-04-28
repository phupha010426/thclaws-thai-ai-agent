import { raw, Router } from 'express';
import { env } from '../../config/env.js';
import { lineWebhookService } from './line-webhook.service.js';

export const lineRouter = Router();

lineRouter.post('/webhook/line', raw({ type: '*/*', limit: env.WEBHOOK_BODY_LIMIT }), async (req, res, next) => {
  try {
    const rawBody = Buffer.isBuffer(req.body) ? req.body : Buffer.from('');
    const result = await lineWebhookService.verifyAndEnqueue({
      rawBody,
      signature: req.header('x-line-signature') ?? undefined
    });

    if (!result.ok) return res.status(result.statusCode).json({ ok: false, error: result.error });
    return res.json({ ok: true, eventCount: result.eventCount });
  } catch (error) {
    return next(error);
  }
});
