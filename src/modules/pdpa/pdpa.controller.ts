import { Router } from 'express';
import { env } from '../../config/env.js';
import { logger } from '../../libs/logger.js';
import { pdpaService } from './pdpa.service.js';

/**
 * PDPA endpoints. Authentication is intentionally simple (shared admin token)
 * because consent / identity verification happens out of band — these
 * endpoints are operated by ops or by an authenticated LIFF dashboard, not
 * by anonymous LINE users.
 */
export const pdpaRouter = Router();

pdpaRouter.use('/pdpa', (req, res, next) => {
  const provided = req.header('x-admin-token');
  if (!provided || provided !== env.THCLAWS_API_KEY) {
    return res.status(401).json({ ok: false, error: 'unauthorized' });
  }
  next();
});

pdpaRouter.post('/pdpa/delete', async (req, res) => {
  const namespace = parseNamespace(req.body);
  if (!namespace) return res.status(400).json({ ok: false, error: 'namespace required' });
  try {
    const report = await pdpaService.deleteAll(namespace);
    res.json({ ok: true, report });
  } catch (err) {
    logger.error({ err, namespace }, 'pdpa delete failed');
    res.status(500).json({ ok: false, error: 'delete failed' });
  }
});

pdpaRouter.post('/pdpa/export', async (req, res) => {
  const namespace = parseNamespace(req.body);
  if (!namespace) return res.status(400).json({ ok: false, error: 'namespace required' });
  try {
    const data = await pdpaService.exportAll(namespace);
    res.json({ ok: true, data });
  } catch (err) {
    logger.error({ err, namespace }, 'pdpa export failed');
    res.status(500).json({ ok: false, error: 'export failed' });
  }
});

function parseNamespace(body: unknown): string | null {
  if (!body || typeof body !== 'object') return null;
  const ns = (body as { namespace?: unknown }).namespace;
  if (typeof ns !== 'string' || !ns.trim()) return null;
  return ns.trim();
}
