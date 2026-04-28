import { TransactionType } from '@prisma/client';
import { prisma } from '../../db/postgres.js';
import { env } from '../../config/env.js';
import { logger } from '../../libs/logger.js';
import { coreClient } from '../core/core-client.js';
import { wikiBrainService } from '../brain/wiki-brain.service.js';
import { embeddingService } from '../brain/embedding.service.js';
import { transactionParserService } from '../ledger/transaction-parser.service.js';
import { memoryService } from '../memory/memory.service.js';
import { queueService } from '../queue/queue.service.js';
import { smlGatewayService } from './sml-gateway.service.js';

export type ThClawsAction =
  | {
      type: 'record_transaction';
      transaction: {
        type: TransactionType;
        amount: number;
        category: string;
        note: string;
        confidence: number;
      };
    }
  | { type: 'remember_memory'; content: string }
  | { type: 'update_wiki'; title: string; content: string }
  | {
      type: 'create_program';
      program: {
        name: string;
        description: string;
        language: 'typescript' | 'jsonlogic' | 'prompt';
        source: string;
      };
    }
  | { type: 'get_today_summary' }
  | { type: 'get_month_summary' }
  | { type: 'help' }
  | { type: 'ask_clarification'; question: string }
  | { type: 'no_op' };

export type ThClawsPlan = {
  finalReply?: string;
  actions: ThClawsAction[];
};

export class ThClawsAgentService {
  async planTextMessage(input: { userId: string; agentId: string; namespace: string; text: string }): Promise<ThClawsPlan> {
    const startedAt = Date.now();
    const remoteCandidate = await coreClient.parseTransactionAsParsedIntent({ namespace: input.namespace, text: input.text });
    const localCandidate = remoteCandidate ?? transactionParserService.parse(input.text);
    const candidateSource: 'core' | 'node' = remoteCandidate ? 'core' : 'node';

    try {
      const localPlan = planFromDeterministicParser(localCandidate);
      if (localPlan) {
        await this.logToolCall({
          actorId: input.userId,
          status: 'SUCCESS',
          latencyMs: Date.now() - startedAt,
          metadata: { namespace: input.namespace, planner: 'deterministic', source: candidateSource, actions: localPlan.actions.map((action) => action.type).join(',') }
        });
        logger.info({
          userId: input.userId,
          namespace: input.namespace,
          latencyMs: Date.now() - startedAt,
          source: candidateSource,
          actions: localPlan.actions.map((action) => action.type)
        }, 'thClaws deterministic planner success');
        return localPlan;
      }

      // Load conversation memory + WikiBrain graph + semantic recall in parallel.
      // All three are scoped to the same namespace; nothing crosses LINE userIds.
      const [context, brainPages, semanticHits] = await Promise.all([
        memoryService.getContext(input.namespace),
        wikiBrainService.retrieve({ namespace: input.namespace, text: input.text, limit: 6 }),
        embeddingService.search({
          namespace: input.namespace,
          query: input.text,
          limit: 4,
          sourceTypes: ['wiki_page', 'memory_item', 'summary']
        }).catch(() => []) // semantic search is optional; never block reply
      ]);

      const enrichedContext = { ...context, brainPages, semanticHits };

      // Fire-and-forget brain extraction. Skip messages that are obviously not
      // worth an LLM extraction (very short, or already classified as a
      // deterministic record_*) so we do not burn tokens on "ครับ" / "55".
      const worthExtracting = input.text.trim().length >= 8
        && !(localCandidate && (localCandidate.intent === 'record_expense' || localCandidate.intent === 'record_income'));
      if (worthExtracting) {
        void queueService.enqueueBrainExtraction({
          ownerId: input.userId,
          agentId: input.agentId,
          namespace: input.namespace,
          text: input.text
        }).catch((err) => logger.warn({ err: (err as Error).message, namespace: input.namespace }, 'enqueue brain extraction failed'));
      }

      const memoryPlan = planFromMemoryCommand(input.text);
      if (memoryPlan) return memoryPlan;

      const contextPlan = planFromContextQuestion(input.text, enrichedContext);
      if (contextPlan) return contextPlan;

      if (shouldAnswerDirectly(input.text)) {
        const finalReply = await this.answerGeneralQuestion({ namespace: input.namespace, text: input.text, context: enrichedContext });
        return { finalReply, actions: [{ type: 'no_op' }] };
      }

      const plan = await this.callThClaws({
        userId: input.userId,
        agentId: input.agentId,
        text: input.text,
        namespace: input.namespace,
        localCandidate,
        context: enrichedContext
      });
      await this.logToolCall({
        actorId: input.userId,
        status: 'SUCCESS',
        latencyMs: Date.now() - startedAt,
        metadata: { namespace: input.namespace, actions: plan.actions.map((action) => action.type).join(',') }
      });
      logger.info({
        userId: input.userId,
        namespace: input.namespace,
        latencyMs: Date.now() - startedAt,
        actions: plan.actions.map((action) => action.type)
      }, 'thClaws planner success');
      return plan;
    } catch (error) {
      await this.logToolCall({
        actorId: input.userId,
        status: 'FAILED',
        latencyMs: Date.now() - startedAt,
        metadata: { namespace: input.namespace }
      });
      logger.error({ err: error, userId: input.userId, namespace: input.namespace }, 'thClaws planner failed');
      throw error;
    }
  }

  private async answerGeneralQuestion(input: { namespace: string; text: string; context: unknown }) {
    return smlGatewayService.chat({
      namespace: input.namespace,
      cacheable: true,
      model: env.GENERAL_AGENT_MODEL,
      fallbackModels: [env.DEFAULT_AGENT_MODEL, ...modelList(env.TEXT_FALLBACK_MODELS)],
      messages: [
        {
          role: 'system',
          content: [
            'You are ThaiAiAgent, a Thai personal secretary.',
            'Answer the current question directly in Thai.',
            'Use only the provided userBrain for personal facts and history.',
            'Do not fabricate private history, transactions, balances, or preferences.',
            'If a fact is not in userBrain, say you do not know yet.',
            'Keep the answer concise and useful.'
          ].join('\n')
        },
        {
          role: 'user',
          content: JSON.stringify({ text: input.text, userBrain: input.context })
        }
      ],
      timeoutMs: env.THCLAWS_TIMEOUT_MS,
      strategy: 'fastest',
      maxLatencyMs: 5000,
      preferProviders: env.SMLGATEWAY_PREFER_PROVIDERS,
      excludeProviders: env.SMLGATEWAY_TEXT_EXCLUDE_PROVIDERS
    });
  }

  private async callThClaws(input: {
    userId: string;
    agentId: string;
    text: string;
    namespace: string;
    localCandidate: unknown;
    context: unknown;
  }) {
    const plan = await smlGatewayService.structured<ThClawsPlan>({
      model: env.DEFAULT_AGENT_MODEL,
      fallbackModels: modelList(env.TEXT_FALLBACK_MODELS),
      schema: thClawsPlanSchema(),
      strategy: 'fastest',
      maxLatencyMs: 5000,
      preferProviders: env.SMLGATEWAY_PREFER_PROVIDERS,
      excludeProviders: env.SMLGATEWAY_TEXT_EXCLUDE_PROVIDERS,
      messages: [
        {
          role: 'system',
          content: [
            'You are the central planner and personal secretary of ThaiAiAgent.',
            'You can answer general questions in Thai, help as a personal assistant, and manage accounting actions.',
            'Never fabricate facts, balances, transactions, user preferences, or history.',
            'Use only the provided userBrain, deterministicParserCandidate, current message, and executed tool results.',
            'If information is missing, uncertain, or not in the provided data, say you do not know yet and ask for the needed detail.',
            'Do not claim an action was saved, paid, transferred, deleted, or confirmed unless you return the matching action for Node to execute.',
            'You control the work plan, team actions, database actions, and final Thai reply.',
            'Node services are only executors. They must execute only the structured actions you return.',
            'Each LINE user is isolated. Never use memory, ledger, profile, summary, or account data outside the provided userId/agentId/namespace.',
            'All database actions must stay inside the provided namespace and owner userId.',
            'Use compactSummary, recentHistory, and wikiPages as this user personal brain only.',
            'Plan whether to update the personal wiki when the user reveals stable preferences, account ownership, identity, or reusable facts.',
            'You may create small reusable private agent programs when the user asks for repeated automation or custom calculation.',
            'For create_program, return a draft program spec only. Do not ask Node to execute arbitrary code.',
            'Generated programs must never include API keys, tokens, credentials, cross-user reads, network calls, shell commands, or filesystem access.',
            'The deterministicParserCandidate is trusted for simple accounting intents.',
            'If deterministicParserCandidate.intent is ask_today_summary, return action get_today_summary and do not ask clarification.',
            'If deterministicParserCandidate.intent is ask_month_summary, return action get_month_summary and do not ask clarification.',
            'Thai phrases like "วันนี้จ่ายค่าอะไรไปบ้าง" mean today summary, not missing amount.',
            'Thai phrases like "เดือนนี้จ่ายค่าอะไรไปบ้าง" mean month summary, not missing amount.',
            'For general questions such as recipes, travel, reminders, or explanations, use no_op and put the answer in finalReply.',
            'If amount is missing or confidence is low for accounting, use ask_clarification and do not record.',
            'Do not push messages. Only return finalReply for the current LINE reply.',
            'Keep finalReply short, useful, and Thai.'
          ].join('\n')
        },
        {
          role: 'user',
          content: JSON.stringify({
            userId: input.userId,
            agentId: input.agentId,
            namespace: input.namespace,
            text: input.text,
            deterministicParserCandidate: input.localCandidate,
            userBrain: input.context
          })
        }
      ]
    });

    return validatePlan(normalizePlan(plan, JSON.stringify(plan)));
  }

  private async logToolCall(input: {
    actorId: string;
    status: 'SUCCESS' | 'FAILED';
    latencyMs: number;
    metadata: Record<string, string | number | boolean | undefined>;
  }) {
    await prisma.toolCall.create({
      data: {
        actorId: input.actorId,
        toolName: 'thclaws.plan_text_message',
        status: input.status,
        latencyMs: input.latencyMs,
        metadata: input.metadata
      }
    });
  }
}

function normalizePlan(rawPlan: unknown, rawContent: string): ThClawsPlan {
  if (!rawPlan || typeof rawPlan !== 'object') throw new Error('thClaws plan is not object');
  const plan = rawPlan as Record<string, unknown>;
  const finalReply = pickString(plan.finalReply, plan.final_reply, plan.reply, plan.response, plan.message);
  const rawActions = Array.isArray(plan.actions)
    ? plan.actions
    : plan.action
      ? [plan.action]
      : [];
  const actions = rawActions
    .map((action) => normalizeAction(action))
    .filter((action): action is ThClawsAction => Boolean(action));

  if (actions.length === 0 && finalReply) {
    logger.warn({ rawContent: rawContent.slice(0, 500) }, 'thClaws plan repaired to no_op');
    return { finalReply, actions: [{ type: 'no_op' }] };
  }

  return {
    finalReply,
    actions
  };
}

function normalizeAction(rawAction: unknown): ThClawsAction | null {
  if (typeof rawAction === 'string') {
    const type = normalizeActionType(rawAction);
    return type ? ({ type } as ThClawsAction) : null;
  }
  if (!rawAction || typeof rawAction !== 'object') return null;

  const action = rawAction as Record<string, unknown>;
  const type = normalizeActionType(action.type ?? action.action ?? action.name ?? action.tool ?? action.intent);
  if (!type) return null;
  if (type === 'remember_memory' && !pickString(action.content)) return null;
  if (type === 'update_wiki' && (!pickString(action.title) || !pickString(action.content))) return null;
  if (type === 'ask_clarification' && !pickString(action.question)) return null;
  if (type === 'create_program') {
    const program = action.program as Record<string, unknown> | undefined;
    if (!program || !pickString(program.name) || !pickString(program.description) || !pickString(program.source)) return null;
  }
  return { ...action, type } as ThClawsAction;
}

function normalizeActionType(value: unknown) {
  const normalized = String(value ?? '').trim().toLowerCase();
  const aliases: Record<string, ThClawsAction['type']> = {
    today_summary: 'get_today_summary',
    ask_today_summary: 'get_today_summary',
    month_summary: 'get_month_summary',
    ask_month_summary: 'get_month_summary',
    clarification: 'ask_clarification',
    answer: 'no_op',
    chat: 'no_op',
    unknown: 'no_op'
  };
  const type = aliases[normalized] ?? normalized;
  return isKnownAction(type) ? type as ThClawsAction['type'] : null;
}

function pickString(...values: unknown[]) {
  for (const value of values) {
    if (typeof value === 'string' && value.trim()) return value.trim();
  }
  return undefined;
}

function validatePlan(plan: ThClawsPlan): ThClawsPlan {
  if (!Array.isArray(plan.actions)) throw new Error('thClaws plan missing actions');
  if (plan.actions.length === 0) return { ...plan, actions: [{ type: 'no_op' }] };

  for (const action of plan.actions) {
    if (!isKnownAction(action.type)) throw new Error(`unknown thClaws action: ${String(action.type)}`);
    if (action.type === 'record_transaction') {
      const tx = action.transaction;
      if (!tx || !['INCOME', 'EXPENSE', 'TRANSFER'].includes(tx.type)) throw new Error('invalid transaction type');
      if (!Number.isFinite(tx.amount) || tx.amount <= 0) throw new Error('invalid transaction amount');
      if (!tx.category || !tx.note) throw new Error('invalid transaction detail');
    }
    if (action.type === 'update_wiki') {
      if (!action.title?.trim() || !action.content?.trim()) throw new Error('invalid wiki update');
    }
    if (action.type === 'remember_memory') {
      if (!action.content?.trim()) throw new Error('invalid memory content');
    }
    if (action.type === 'create_program') {
      const program = action.program;
      if (!program?.name?.trim() || !program.description?.trim() || !program.source?.trim()) {
        throw new Error('invalid agent program');
      }
      if (!['typescript', 'jsonlogic', 'prompt'].includes(program.language)) throw new Error('invalid agent program language');
    }
  }

  return plan;
}

function isKnownAction(action: unknown) {
  return [
    'record_transaction',
    'remember_memory',
    'update_wiki',
    'create_program',
    'get_today_summary',
    'get_month_summary',
    'help',
    'ask_clarification',
    'no_op'
  ].includes(String(action));
}

function thClawsPlanSchema() {
  return {
    type: 'object',
    required: ['actions', 'finalReply'],
    properties: {
      finalReply: { type: 'string' },
      actions: {
        type: 'array',
        items: {
          type: 'object',
          required: ['type'],
          properties: {
            type: {
              type: 'string',
              enum: [
                'record_transaction',
                'remember_memory',
                'update_wiki',
                'create_program',
                'get_today_summary',
                'get_month_summary',
                'help',
                'ask_clarification',
                'no_op'
              ]
            },
            question: { type: 'string' },
            content: { type: 'string' },
            title: { type: 'string' },
            transaction: {
              type: 'object',
              properties: {
                type: { type: 'string', enum: ['INCOME', 'EXPENSE', 'TRANSFER'] },
                amount: { type: 'number' },
                category: { type: 'string' },
                note: { type: 'string' },
                confidence: { type: 'number' }
              }
            },
            program: {
              type: 'object',
              properties: {
                name: { type: 'string' },
                description: { type: 'string' },
                language: { type: 'string', enum: ['typescript', 'jsonlogic', 'prompt'] },
                source: { type: 'string' }
              }
            }
          }
        }
      }
    }
  };
}

function modelList(value: string) {
  return value.split(',').map((model) => model.trim()).filter(Boolean);
}

export const thClawsAgentService = new ThClawsAgentService();

function planFromDeterministicParser(candidate: ReturnType<typeof transactionParserService.parse>): ThClawsPlan | null {
  if (candidate.intent === 'help') return { actions: [{ type: 'help' }] };
  if (candidate.intent === 'ask_today_summary') return { actions: [{ type: 'get_today_summary' }] };
  if (candidate.intent === 'ask_month_summary') return { actions: [{ type: 'get_month_summary' }] };
  if (candidate.intent === 'record_expense' || candidate.intent === 'record_income') {
    if (candidate.confidence < 0.75) {
      return {
        finalReply: 'รายการนี้ยังไม่ชัดครับ รบกวนพิมพ์หมวดหรือรายละเอียดเพิ่มอีกนิด',
        actions: [{ type: 'ask_clarification', question: 'รายการนี้เป็นรายรับหรือรายจ่าย หมวดอะไรครับ?' }]
      };
    }
    return {
      actions: [
        {
          type: 'record_transaction',
          transaction: {
            type: candidate.type,
            amount: candidate.amount,
            category: candidate.category,
            note: candidate.note,
            confidence: candidate.confidence
          }
        }
      ]
    };
  }
  return null;
}

function planFromMemoryCommand(text: string): ThClawsPlan | null {
  const match = text.trim().match(/^(?:จำไว้ว่า|จำว่า|ช่วยจำว่า)\s*(.+)$/i);
  const content = match?.[1]?.trim();
  if (!content) return null;
  return {
    finalReply: 'จำไว้ให้แล้วครับ',
    actions: [{ type: 'remember_memory', content }]
  };
}

function planFromContextQuestion(text: string, context: unknown): ThClawsPlan | null {
  if (!/เมื่อกี้|ก่อนหน้า|จำได้ไหม/.test(text)) return null;
  if (!/ไม่กินอะไร|ไม่กิน/.test(text)) return null;
  const contextText = JSON.stringify(context);
  const match = contextText.match(/ไม่กิน\s*([^"\\,.\n\r]+)/);
  if (!match?.[1]) return null;
  return {
    finalReply: `เมื่อกี้คุณบอกว่าไม่กิน${match[1].trim()}ครับ`,
    actions: [{ type: 'no_op' }]
  };
}

function shouldAnswerDirectly(text: string) {
  if (/สร้างโปรแกรม|ทำโปรแกรม|automation|อัตโนมัติ/i.test(text)) return false;
  return true;
}
