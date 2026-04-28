import crypto from 'node:crypto';
import { env } from '../../config/env.js';

export class LineSignatureService {
  verify(rawBody: Buffer, signature?: string): boolean {
    if (!signature) return false;
    const hmac = crypto.createHmac('sha256', env.LINE_CHANNEL_SECRET);
    hmac.update(rawBody);
    const digest = hmac.digest('base64');
    const signatureBuffer = Buffer.from(signature);
    const digestBuffer = Buffer.from(digest);
    if (signatureBuffer.length !== digestBuffer.length) return false;
    return crypto.timingSafeEqual(signatureBuffer, digestBuffer);
  }
}

export const lineSignatureService = new LineSignatureService();
