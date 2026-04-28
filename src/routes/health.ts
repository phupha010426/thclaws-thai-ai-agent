import { Router } from 'express';
import { metrics } from '../modules/metrics/metrics.service.js';

export const healthRouter = Router();

healthRouter.get('/health', (_req, res) => {
  res.json({ ok: true, service: 'ThaiAiAgent', time: new Date().toISOString() });
});

healthRouter.get('/metrics', (_req, res) => {
  res.type('text/plain; version=0.0.4');
  res.send(metrics.render());
});
