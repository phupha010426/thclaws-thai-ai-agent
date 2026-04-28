import { env } from '../../config/env.js';
import { logger } from '../../libs/logger.js';
import { smlGatewayCache } from './sml-gateway-cache.js';

export class SmlGatewayService {
  async structured<T>(input: {
    model: string;
    fallbackModels?: string[];
    messages: unknown[];
    schema: Record<string, unknown>;
    maxRetries?: number;
    timeoutMs?: number;
    strategy?: 'fastest' | 'strongest';
    maxLatencyMs?: number | null;
    preferProviders?: string;
    excludeProviders?: string;
  }): Promise<T> {
    const response = await this.request('/structured', {
      method: 'POST',
      body: {
        model: input.model,
        messages: input.messages,
        schema: input.schema,
        max_retries: input.maxRetries ?? 2
      },
      timeoutMs: input.timeoutMs,
      strategy: input.strategy,
      maxLatencyMs: input.maxLatencyMs,
      preferProviders: input.preferProviders,
      excludeProviders: input.excludeProviders
    });
    const bodyText = await response.text();
    const body = safeJson(bodyText) as { ok?: boolean; data?: T; error?: unknown } | null;
    if (response.ok && body?.ok !== false && body?.data) return body.data;

    const logPayload = { status: response.status, body: bodyText.slice(0, 500) };
    if (bodyText.includes('HTTP 401 from gateway')) {
      logger.info(logPayload, 'SMLGateway structured unavailable, falling back to chat JSON');
    } else {
      logger.warn(logPayload, 'SMLGateway structured failed, falling back to chat JSON');
    }

    return this.structuredViaChat<T>(input);
  }

  private async structuredViaChat<T>(input: {
    model: string;
    fallbackModels?: string[];
    messages: unknown[];
    schema: Record<string, unknown>;
    timeoutMs?: number;
    strategy?: 'fastest' | 'strongest';
    maxLatencyMs?: number | null;
    preferProviders?: string;
    excludeProviders?: string;
  }): Promise<T> {
    const errors: string[] = [];
    const models = unique([input.model, ...(input.fallbackModels ?? [])]);
    for (const model of models) {
      try {
        const content = await this.chat({
          model,
          messages: [
            {
              role: 'system',
              content: [
                'Return only valid JSON matching this JSON schema.',
                'Do not wrap in markdown.',
                'The finalReply must be Thai unless the user explicitly asks for another language.',
                JSON.stringify(input.schema)
              ].join('\n')
            },
            ...input.messages
          ],
          timeoutMs: input.timeoutMs,
          maxTokens: 2000,
          strategy: input.strategy,
          maxLatencyMs: input.maxLatencyMs,
          preferProviders: input.preferProviders,
          excludeProviders: input.excludeProviders
        });
        const parsed = parseLooseJson(extractJson(content));
        if (!parsed) throw new Error(`invalid JSON: ${content.slice(0, 300)}`);
        if (hasNonThaiFinalReply(parsed)) throw new Error('non-Thai finalReply');
        if (model !== input.model) logger.warn({ primaryModel: input.model, model }, 'SMLGateway structured fallback model used');
        return parsed as T;
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error);
        errors.push(`${model}: ${message.slice(0, 300)}`);
        logger.warn({ model, err: error }, 'SMLGateway structured fallback model failed');
      }
    }
    throw new Error(`SMLGateway structured fallback exhausted: ${errors.join(' | ')}`);
  }

  async chat(input: {
    model: string;
    fallbackModels?: string[];
    messages: unknown[];
    timeoutMs?: number;
    maxTokens?: number;
    responseFormat?: Record<string, unknown>;
    strategy?: 'fastest' | 'strongest';
    maxLatencyMs?: number | null;
    preferProviders?: string;
    excludeProviders?: string;
    /**
     * LINE userId scope. Required to enable response caching — if omitted, the
     * call always hits the gateway. We never share cached answers between
     * users, even for identical prompts.
     */
    namespace?: string;
    /** Set true for deterministic prompts (summary, help, idempotent tools). */
    cacheable?: boolean;
  }) {
    if (input.cacheable && input.namespace) {
      const cached = await smlGatewayCache.get<string>({
        namespace: input.namespace,
        model: input.model,
        messages: input.messages,
        strategy: input.strategy,
        preferProviders: input.preferProviders,
        excludeProviders: input.excludeProviders
      });
      if (cached) {
        logger.info({ model: input.model, namespace: input.namespace }, 'SMLGateway cache hit');
        return cached;
      }
    }

    const errors: string[] = [];
    const models = unique([input.model, ...(input.fallbackModels ?? [])]);
    for (const model of models) {
      try {
        const content = await this.chatOnce({ ...input, model });
        if (looksWrongLanguage(content)) throw new Error('wrong language response');
        if (model !== input.model) logger.warn({ primaryModel: input.model, model }, 'SMLGateway chat fallback model used');
        if (input.cacheable && input.namespace) {
          void smlGatewayCache.set({
            namespace: input.namespace,
            model: input.model,
            messages: input.messages,
            strategy: input.strategy,
            preferProviders: input.preferProviders,
            excludeProviders: input.excludeProviders
          }, content);
        }
        return content;
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error);
        errors.push(`${model}: ${message.slice(0, 300)}`);
        logger.warn({ model, err: error }, 'SMLGateway chat model failed');
      }
    }
    throw new Error(`SMLGateway chat exhausted: ${errors.join(' | ')}`);
  }

  /**
   * Compute an embedding. Tries `model` first, then falls through
   * `fallbackModels`. Returns the first non-empty vector. Throws only when
   * every model in the chain fails so callers see a single rich error.
   */
  async embed(input: { model: string; fallbackModels?: string[]; text: string; timeoutMs?: number }): Promise<number[]> {
    const errors: string[] = [];
    const models = unique([input.model, ...(input.fallbackModels ?? [])]);
    for (const model of models) {
      try {
        const response = await this.request('/embeddings', {
          method: 'POST',
          body: { model, input: input.text },
          timeoutMs: input.timeoutMs ?? env.THCLAWS_TIMEOUT_MS,
          strategy: 'fastest',
          maxLatencyMs: env.SMLGATEWAY_MAX_LATENCY_MS
        });
        const bodyText = await response.text();
        if (!response.ok) {
          errors.push(`${model}: HTTP ${response.status} ${bodyText.slice(0, 100)}`);
          continue;
        }
        const body = JSON.parse(bodyText) as { data?: Array<{ embedding?: number[] }> };
        const vector = body.data?.[0]?.embedding;
        if (!Array.isArray(vector) || vector.length === 0) {
          errors.push(`${model}: empty response`);
          continue;
        }
        if (model !== input.model) logger.warn({ primary: input.model, used: model }, 'SMLGateway embed fallback used');
        return vector;
      } catch (error) {
        errors.push(`${model}: ${(error as Error).message.slice(0, 200)}`);
      }
    }
    throw new Error(`SMLGateway embed exhausted: ${errors.join(' | ')}`);
  }

  private async chatOnce(input: {
    model: string;
    messages: unknown[];
    timeoutMs?: number;
    maxTokens?: number;
    responseFormat?: Record<string, unknown>;
    strategy?: 'fastest' | 'strongest';
    maxLatencyMs?: number | null;
    preferProviders?: string;
    excludeProviders?: string;
  }) {
    const response = await this.request('/chat/completions', {
      method: 'POST',
      body: {
        model: input.model,
        messages: input.messages,
        temperature: 0,
        max_tokens: input.maxTokens ?? 1200,
        ...(input.responseFormat ? { response_format: input.responseFormat } : {})
      },
      timeoutMs: input.timeoutMs,
      strategy: input.strategy,
      maxLatencyMs: input.maxLatencyMs,
      preferProviders: input.preferProviders,
      excludeProviders: input.excludeProviders
    });
    const bodyText = await response.text();
    if (!response.ok) throw new Error(`SMLGateway chat failed ${response.status}: ${bodyText.slice(0, 500)}`);
    const body = JSON.parse(bodyText) as { choices?: Array<{ message?: { content?: string } }> };
    const content = body.choices?.[0]?.message?.content?.trim();
    if (!content) throw new Error('SMLGateway chat empty response');
    return content;
  }

  private async request(path: string, input: {
    method: 'POST';
    body: unknown;
    timeoutMs?: number;
    strategy?: 'fastest' | 'strongest';
    maxLatencyMs?: number | null;
    preferProviders?: string;
    excludeProviders?: string;
  }) {
    const url = `${env.THCLAWS_GATEWAY_URL.replace(/\/$/, '')}${path}`;
    const headers: Record<string, string> = {
      Authorization: `Bearer ${env.THCLAWS_API_KEY}`,
      'Content-Type': 'application/json',
      'X-SMLGateway-Strategy': input.strategy ?? 'fastest'
    };
    if (input.maxLatencyMs !== null) {
      headers['X-SMLGateway-Max-Latency'] = String(input.maxLatencyMs ?? env.SMLGATEWAY_MAX_LATENCY_MS);
    }
    if (input.preferProviders) headers['X-SMLGateway-Prefer'] = input.preferProviders;
    if (input.excludeProviders) headers['X-SMLGateway-Exclude'] = input.excludeProviders;
    const response = await fetch(url, {
      method: input.method,
      headers,
      body: JSON.stringify(input.body),
      signal: AbortSignal.timeout(input.timeoutMs ?? env.THCLAWS_TIMEOUT_MS)
    });
    logger.info({
      status: response.status,
      requestId: response.headers.get('x-smlgateway-request-id') ?? undefined,
      model: response.headers.get('x-smlgateway-model') ?? undefined,
      provider: response.headers.get('x-smlgateway-provider') ?? undefined,
      cache: response.headers.get('x-smlgateway-cache') ?? undefined
    }, 'SMLGateway response');
    return response;
  }
}

function safeJson(content: string) {
  try {
    return JSON.parse(content);
  } catch {
    return null;
  }
}

function parseLooseJson(content: string) {
  const parsed = safeJson(content);
  if (parsed) return parsed;

  const repaired = content
    .replace(/}\s*,\s*"actions"\s*:/, ',"actions":')
    .replace(/}\s*,\s*"finalReply"\s*:/, ',"finalReply":');
  const repairedParsed = safeJson(repaired);
  if (repairedParsed) return repairedParsed;

  const finalReply = content.match(/"finalReply"\s*:\s*"((?:\\"|[^"])*)"/)?.[1]
    ?.replace(/\\"/g, '"')
    ?.trim();
  if (finalReply) return { finalReply, actions: [{ type: 'no_op' }] };

  return null;
}

function hasNonThaiFinalReply(value: unknown) {
  if (!value || typeof value !== 'object') return false;
  const finalReply = (value as Record<string, unknown>).finalReply;
  return typeof finalReply === 'string' && looksWrongLanguage(finalReply);
}

function looksWrongLanguage(content: string) {
  const thaiChars = (content.match(/[\u0E00-\u0E7F]/g) ?? []).length;
  const arabicChars = (content.match(/[\u0600-\u06FF]/g) ?? []).length;
  if (arabicChars >= 8 && arabicChars > thaiChars) return true;
  return false;
}

function unique(values: string[]) {
  return [...new Set(values.map((value) => value.trim()).filter(Boolean))];
}

function extractJson(content: string) {
  const start = content.indexOf('{');
  const end = content.lastIndexOf('}');
  if (start < 0 || end < start) return content;
  return content.slice(start, end + 1);
}

export const smlGatewayService = new SmlGatewayService();
