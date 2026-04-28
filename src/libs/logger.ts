import pino from 'pino';
import { env } from '../config/env.js';

export const logger = pino({
  name: env.APP_NAME,
  level: env.LOG_LEVEL,
  redact: {
    paths: [
      'req.headers.authorization',
      'req.headers.cookie',
      '*.LINE_CHANNEL_ACCESS_TOKEN',
      '*.LINE_CHANNEL_SECRET',
      '*.THCLAWS_API_KEY'
    ],
    remove: true
  }
});
