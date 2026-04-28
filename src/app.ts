import cors from 'cors';
import express from 'express';
import helmet from 'helmet';
import { isAppError } from './libs/errors.js';
import { logger } from './libs/logger.js';
import { lineRouter } from './modules/line/line.controller.js';
import { pdpaRouter } from './modules/pdpa/pdpa.controller.js';
import { healthRouter } from './routes/health.js';

export function createApp() {
  const app = express();

  app.use(helmet());
  app.use(cors());
  app.use((req, res, next) => {
    const startedAt = Date.now();
    res.on('finish', () => {
      logger.info({ method: req.method, path: req.path, statusCode: res.statusCode, durationMs: Date.now() - startedAt }, 'http request');
    });
    next();
  });

  app.use(healthRouter);
  app.use(lineRouter);
  app.use(express.json({ limit: '256kb' }));
  app.use(pdpaRouter);

  app.use((err: unknown, _req: express.Request, res: express.Response, _next: express.NextFunction) => {
    logger.error({ err }, 'request failed');
    if (isAppError(err)) {
      return res.status(err.statusCode).json({ ok: false, code: err.code, error: err.message });
    }
    return res.status(500).json({ ok: false, error: 'Internal server error' });
  });

  return app;
}
