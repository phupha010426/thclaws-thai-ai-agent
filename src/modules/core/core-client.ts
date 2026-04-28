import { env } from '../../config/env.js';
import { logger } from '../../libs/logger.js';
import { parseImageUnderstanding, type ImageUnderstandingResult, type ParseOutcome as VisionOutcome } from '../agents/image-understanding.parser.js';
import type { ParsedIntent } from '../ledger/transaction-parser.service.js';

export type CoreTransactionResult = {
  intent: 'record_expense' | 'record_income' | 'ask_today_summary' | 'ask_month_summary' | 'help' | 'unknown';
  type?: 'INCOME' | 'EXPENSE';
  amount?: number;
  category?: string;
  note?: string;
  confidence?: number;
  reason?: string;
};

export type CoreVisionResponse = {
  result: ImageUnderstandingResult & { hasAmount?: boolean };
  usedFallback: boolean;
};

export class CoreClient {
  constructor(private readonly baseUrl: string, private readonly timeoutMs: number) {}

  get enabled() {
    return Boolean(this.baseUrl);
  }

  async parseTransaction(input: { namespace: string; text: string }): Promise<CoreTransactionResult | null> {
    if (!input.namespace) throw new Error('coreClient.parseTransaction: namespace required');
    return this.post<CoreTransactionResult>('/v1/parse/transaction', input);
  }

  /** Returns a Node-shaped ParsedIntent if the Go core is reachable, else null. */
  async parseTransactionAsParsedIntent(input: { namespace: string; text: string }): Promise<ParsedIntent | null> {
    if (!input.namespace) throw new Error('coreClient.parseTransactionAsParsedIntent: namespace required');
    const remote = await this.parseTransaction(input);
    if (!remote) return null;
    if (remote.intent === 'record_expense' || remote.intent === 'record_income') {
      if (remote.type !== 'INCOME' && remote.type !== 'EXPENSE') return null;
      if (typeof remote.amount !== 'number' || !Number.isFinite(remote.amount)) return null;
      return {
        intent: remote.intent,
        type: remote.type,
        amount: remote.amount,
        category: remote.category ?? 'อื่น ๆ',
        note: remote.note ?? input.text,
        confidence: remote.confidence ?? 0.7
      };
    }
    return { intent: remote.intent, reason: remote.reason };
  }

  async parseVision(input: { namespace: string; content: string }): Promise<CoreVisionResponse | null> {
    if (!input.namespace) throw new Error('coreClient.parseVision: namespace required');
    return this.post<CoreVisionResponse>('/v1/parse/vision', input);
  }

  /** Wraps parseVision with a Node-side fallback so callers always get a result. */
  async parseVisionOrFallback(input: { namespace: string; content: string }): Promise<VisionOutcome> {
    if (!input.namespace) throw new Error('coreClient.parseVisionOrFallback: namespace required');
    const remote = await this.parseVision(input);
    if (remote) {
      const { hasAmount, ...rest } = remote.result;
      return {
        result: { ...rest, amount: hasAmount ? rest.amount ?? null : null },
        usedFallback: remote.usedFallback
      };
    }
    return parseImageUnderstanding(input.content);
  }

  private async post<T>(path: string, body: unknown): Promise<T | null> {
    if (!this.enabled) return null;
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.timeoutMs);
    try {
      const response = await fetch(`${this.baseUrl}${path}`, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify(body),
        signal: controller.signal
      });
      if (!response.ok) {
        logger.warn({ path, status: response.status }, 'core client non-2xx');
        return null;
      }
      return (await response.json()) as T;
    } catch (err) {
      logger.warn({ path, err: (err as Error).message }, 'core client request failed');
      return null;
    } finally {
      clearTimeout(timer);
    }
  }
}

export const coreClient = new CoreClient(env.CORE_BASE_URL, env.CORE_TIMEOUT_MS);
