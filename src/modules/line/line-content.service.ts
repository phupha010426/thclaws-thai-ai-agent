import { env } from '../../config/env.js';

export class LineContentService {
  async getMessageContent(messageId: string) {
    const response = await fetch(`https://api-data.line.me/v2/bot/message/${encodeURIComponent(messageId)}/content`, {
      headers: {
        Authorization: `Bearer ${env.LINE_CHANNEL_ACCESS_TOKEN}`
      }
    });
    if (!response.ok) throw new Error(`LINE content fetch failed: ${response.status}`);
    const contentType = response.headers.get('content-type') ?? 'application/octet-stream';
    const buffer = Buffer.from(await response.arrayBuffer());
    return { buffer, contentType };
  }
}

export const lineContentService = new LineContentService();
