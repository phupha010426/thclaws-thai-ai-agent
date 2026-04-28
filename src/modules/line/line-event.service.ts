import { logger } from '../../libs/logger.js';
import { isLineImageMessageEvent, isLineTextMessageEvent } from '../../types/line.js';
import { agentService } from '../agents/agent.service.js';
import { agentProgramService } from '../agents/agent-program.service.js';
import { imageUnderstandingService } from '../agents/image-understanding.service.js';
import { thClawsAgentService, type ThClawsAction, type ThClawsPlan } from '../agents/thclaws-agent.service.js';
import { auditService } from '../audit/audit.service.js';
import { ledgerService } from '../ledger/ledger.service.js';
import { slipService } from '../ledger/slip.service.js';
import { lineContentService } from './line-content.service.js';
import { imageService } from '../memory/image.service.js';
import { memoryService } from '../memory/memory.service.js';
import { summaryService } from '../summaries/summary.service.js';
import { userService } from '../users/user.service.js';
import { lineReplyService } from './line-reply.service.js';
import { queueService } from '../queue/queue.service.js';

export class LineEventService {
  async process(event: unknown) {
    if (isLineImageMessageEvent(event)) {
      const lineUserId = event.source?.userId;
      const messageId = event.message?.id;
      if (!lineUserId || !messageId) return;
      logger.info({ lineUserIdHash: hashId(lineUserId), messageType: 'image', hasReplyToken: Boolean(event.replyToken) }, 'line image event received');
      const user = await userService.findOrCreateLineUser({ lineUserId });
      const agent = await agentService.ensurePersonalAgent(user.id);
      await memoryService.ensureAgentProfile(agent.id, agent.namespace);
      await memoryService.saveEventLog({
        ownerId: user.id,
        agentId: agent.id,
        namespace: agent.namespace,
        eventType: 'line.image.received',
        content: '[image]',
        metadata: { lineUserIdHash: hashId(lineUserId), messageId }
      });
      const replyText = await this.handleImageMessageSafely({
        userId: user.id,
        agentId: agent.id,
        namespace: agent.namespace,
        lineMessageId: messageId
      });
      if (event.replyToken) {
        await lineReplyService.replyText({ replyToken: event.replyToken, text: replyText, actorId: user.id });
        await memoryService.saveEventLog({
          ownerId: user.id,
          agentId: agent.id,
          namespace: agent.namespace,
          eventType: 'line.reply.sent',
          content: replyText,
          metadata: { messageType: 'image' }
        });
      }
      return;
    }

    if (!isLineTextMessageEvent(event)) return;
    const lineUserId = event.source?.userId;
    if (!lineUserId) return;
    logger.info({ lineUserIdHash: hashId(lineUserId), messageType: 'text', textLength: event.message.text.length, hasReplyToken: Boolean(event.replyToken) }, 'line text event received');

    const user = await userService.findOrCreateLineUser({ lineUserId });
    const agent = await agentService.ensurePersonalAgent(user.id);
    await memoryService.ensureAgentProfile(agent.id, agent.namespace);

    const text = event.message.text.trim();
    await memoryService.saveEventLog({
      ownerId: user.id,
      agentId: agent.id,
      namespace: agent.namespace,
      eventType: 'line.text.received',
      content: text,
      metadata: { lineUserIdHash: hashId(lineUserId), messageLength: text.length }
    });
    await memoryService.saveConversationMessage({
      ownerId: user.id,
      agentId: agent.id,
      namespace: agent.namespace,
      role: 'user',
      content: text,
      metadata: { source: 'line' }
    });

    const plan = await this.getThClawsPlan({
      userId: user.id,
      agentId: agent.id,
      namespace: agent.namespace,
      text
    });
    if (!plan) {
      if (event.replyToken) {
        const unavailableText = 'ThaiAiAgent ยังไม่พร้อมใช้งานครับ ระบบ thClaws ยังไม่ทำงาน';
        await lineReplyService.replyText({
          replyToken: event.replyToken,
          text: unavailableText,
          actorId: user.id
        });
        await memoryService.saveConversationMessage({
          ownerId: user.id,
          agentId: agent.id,
          namespace: agent.namespace,
          role: 'assistant',
          content: unavailableText,
          metadata: { source: 'line_reply', thclaws: 'unavailable' }
        });
      }
      return;
    }
    logger.info({ userId: user.id, namespace: agent.namespace, actions: plan.actions.map((action) => action.type) }, 'thClaws plan received');

    const replyText = await this.executePlan({ userId: user.id, agentId: agent.id, namespace: agent.namespace, plan });

    await memoryService.saveConversationSnapshot({
      namespace: agent.namespace,
      lineUserId,
      lastIntent: plan.actions.map((action) => action.type).join(',')
    });

    if (event.replyToken) {
      await lineReplyService.replyText({ replyToken: event.replyToken, text: replyText, actorId: user.id });
      await memoryService.saveEventLog({
        ownerId: user.id,
        agentId: agent.id,
        namespace: agent.namespace,
        eventType: 'line.reply.sent',
        content: replyText,
        metadata: { actions: plan.actions.map((action) => action.type) }
      });
      await memoryService.saveConversationMessage({
        ownerId: user.id,
        agentId: agent.id,
        namespace: agent.namespace,
        role: 'assistant',
        content: replyText,
        metadata: { source: 'line_reply', actions: plan.actions.map((action) => action.type) }
      });
      await queueService.enqueueMemoryCompaction({
        ownerId: user.id,
        agentId: agent.id,
        namespace: agent.namespace
      });
    } else {
      logger.warn({ userId: user.id }, 'missing LINE replyToken');
    }
  }

  private async handleImageMessage(input: { userId: string; agentId: string; namespace: string; lineMessageId: string }) {
    const content = await lineContentService.getMessageContent(input.lineMessageId);
    await memoryService.saveConversationMessage({
      ownerId: input.userId,
      agentId: input.agentId,
      namespace: input.namespace,
      role: 'user',
      content: '[image]',
      metadata: { source: 'line', contentType: content.contentType, size: content.buffer.length }
    });
    const image = await imageService.storeLineImage({
      ownerId: input.userId,
      agentId: input.agentId,
      namespace: input.namespace,
      lineMessageId: input.lineMessageId,
      buffer: content.buffer,
      contentType: content.contentType
    });
    const vision = await imageUnderstandingService.analyze({
      buffer: content.buffer,
      contentType: content.contentType,
      namespace: input.namespace
    });
    const slip = await slipService.createPendingSlip({
      ownerId: input.userId,
      agentId: input.agentId,
      namespace: input.namespace,
      storedImageId: image._id
    });
    if (vision.isSlip) {
      const direction = await slipService.applyVisionResult({
        ownerId: input.userId,
        slipId: slip._id,
        vision
      });
      const reply = buildSlipReply(vision.finalReply, direction);
      await memoryService.saveConversationMessage({
        ownerId: input.userId,
        agentId: input.agentId,
        namespace: input.namespace,
        role: 'assistant',
        content: reply,
        metadata: { source: 'line_reply', image: 'slip', direction }
      });
      return reply;
    }

    await memoryService.saveConversationMessage({
      ownerId: input.userId,
      agentId: input.agentId,
      namespace: input.namespace,
      role: 'assistant',
      content: vision.finalReply,
      metadata: { source: 'line_reply', image: 'general' }
    });
    return vision.finalReply;
  }

  private async handleImageMessageSafely(input: { userId: string; agentId: string; namespace: string; lineMessageId: string }) {
    try {
      return await this.handleImageMessage(input);
    } catch (error) {
      logger.error({ err: error, userId: input.userId, namespace: input.namespace }, 'line image handling failed');
      await auditService.log({
        actorId: input.userId,
        action: 'slip.image_failed',
        resource: 'line_image',
        metadata: { namespace: input.namespace }
      });
      return 'รับรูปและเก็บไว้แล้วครับ แต่ตอนนี้ AI อ่านรูปยังไม่พร้อมใช้งาน รอสักครู่แล้วส่งใหม่อีกครั้งได้ครับ';
    }
  }

  private async getThClawsPlan(input: { userId: string; agentId: string; namespace: string; text: string }) {
    try {
      return await thClawsAgentService.planTextMessage(input);
    } catch {
      await auditService.log({
        actorId: input.userId,
        action: 'agent.thclaws_unavailable',
        resource: 'thclaws',
        metadata: { namespace: input.namespace }
      });
      return null;
    }
  }

  private async executePlan(input: {
    userId: string;
    agentId: string;
    namespace: string;
    plan: ThClawsPlan;
  }) {
    let lastReply = input.plan.finalReply;
    for (const action of input.plan.actions) {
      lastReply = await this.executeAction({ ...input, action, currentReply: lastReply });
    }
    return lastReply ?? 'เรียบร้อยครับ';
  }

  private async executeAction(input: {
    userId: string;
    agentId: string;
    namespace: string;
    action: ThClawsAction;
    currentReply?: string;
  }) {
    if (input.action.type === 'help') return helpText();
    if (input.action.type === 'get_today_summary') return summaryService.today(input.userId);
    if (input.action.type === 'get_month_summary') return summaryService.thisMonth(input.userId);
    if (input.action.type === 'ask_clarification') return input.action.question;
    if (input.action.type === 'no_op') return input.currentReply ?? 'รับทราบครับ';

    if (input.action.type === 'remember_memory') {
      logger.info({ userId: input.userId, namespace: input.namespace }, 'executing remember_memory action');
      await memoryService.rememberImportantFact({
        ownerId: input.userId,
        agentId: input.agentId,
        namespace: input.namespace,
        content: input.action.content
      });
      return input.currentReply ?? `จำไว้ให้แล้วครับ: ${input.action.content}`;
    }

    if (input.action.type === 'update_wiki') {
      logger.info({ userId: input.userId, namespace: input.namespace, title: input.action.title }, 'executing update_wiki action');
      await memoryService.upsertWikiPage({
        ownerId: input.userId,
        agentId: input.agentId,
        namespace: input.namespace,
        title: input.action.title,
        content: input.action.content
      });
      return input.currentReply ?? 'อัปเดตสมองส่วนตัวให้แล้วครับ';
    }

    if (input.action.type === 'create_program') {
      logger.info({
        userId: input.userId,
        namespace: input.namespace,
        programName: input.action.program.name
      }, 'executing create_program action');
      await agentProgramService.upsertProgram({
        ownerId: input.userId,
        agentId: input.agentId,
        namespace: input.namespace,
        ...input.action.program
      });
      await auditService.log({
        actorId: input.userId,
        action: 'agent.program_created',
        resource: 'agent_program',
        metadata: { namespace: input.namespace, name: input.action.program.name }
      });
      return input.currentReply ?? `สร้างโปรแกรม "${input.action.program.name}" แบบร่างไว้แล้วครับ`;
    }

    const transaction = input.action.transaction;
    logger.info({
      userId: input.userId,
      namespace: input.namespace,
      type: transaction.type,
      amount: transaction.amount,
      category: transaction.category
    }, 'executing record_transaction action');
    await ledgerService.createPersonalTransaction({
      userId: input.userId,
      agentId: input.agentId,
      type: transaction.type,
      amount: transaction.amount,
      category: transaction.category,
      note: transaction.note,
      confidence: transaction.confidence
    });

    const label = transaction.type === 'INCOME' ? 'รายรับ' : transaction.type === 'EXPENSE' ? 'รายจ่าย' : 'โอน';
    return input.currentReply ?? `บันทึกแล้วครับ ${label} "${transaction.note}" ${transaction.amount.toLocaleString('th-TH')} บาท หมวด${transaction.category}`;
  }
}

function helpText() {
  return ['พิมพ์รายการได้เลย เช่น', 'กาแฟ 60', 'เงินเดือน 25000', 'ค่าไฟ 980', 'สรุปวันนี้', 'สรุปเดือนนี้'].join('\n');
}

function buildSlipReply(summary: string, direction: string) {
  const directionText = direction === 'INCOME'
    ? 'น่าจะเป็นรายรับ'
    : direction === 'EXPENSE'
      ? 'น่าจะเป็นรายจ่าย'
      : direction === 'TRANSFER'
        ? 'น่าจะเป็นการโอนระหว่างบัญชี'
        : 'ยังไม่แน่ใจว่าเป็นรายรับหรือรายจ่าย';
  return `${summary}\n${directionText} รบกวนยืนยันก่อนบันทึกบัญชีครับ`;
}

function unknownText(reason?: string) {
  if (reason === 'missing_amount') return 'ยังไม่เห็นจำนวนเงินครับ ลองพิมพ์เช่น กาแฟ 60';
  return 'ยังไม่เข้าใจครับ พิมพ์ "ช่วยเหลือ" เพื่อดูตัวอย่างได้เลย';
}

export const lineEventService = new LineEventService();

function hashId(value: string) {
  let hash = 0;
  for (let index = 0; index < value.length; index += 1) {
    hash = (hash * 31 + value.charCodeAt(index)) >>> 0;
  }
  return hash.toString(16);
}
