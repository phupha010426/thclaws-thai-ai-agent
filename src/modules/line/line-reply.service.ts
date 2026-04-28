import { messagingApi } from '@line/bot-sdk';
import { env } from '../../config/env.js';
import { auditService } from '../audit/audit.service.js';
import { logger } from '../../libs/logger.js';

export class LineReplyService {
  private readonly client = new messagingApi.MessagingApiClient({
    channelAccessToken: env.LINE_CHANNEL_ACCESS_TOKEN
  });

  async replyText(input: { replyToken: string; text: string; actorId?: string }) {
    // LINE OA policy: reply only. Proactive sends are forbidden.
    await this.client.replyMessage({
      replyToken: input.replyToken,
      messages: [{ type: 'text', text: input.text.slice(0, 4900) }]
    });
    logger.info({ actorId: input.actorId, textLength: input.text.length }, 'line reply sent');

    await auditService.log({
      actorId: input.actorId,
      action: 'line.reply_sent',
      resource: 'line_messages',
      metadata: { textLength: input.text.length }
    });
  }
}

export const lineReplyService = new LineReplyService();
