import { env } from '../../config/env.js';
import { logger } from '../../libs/logger.js';
import { coreClient } from '../core/core-client.js';
import type { ImageUnderstandingResult } from './image-understanding.parser.js';
import { smlGatewayService } from './sml-gateway.service.js';

export type { ImageUnderstandingResult } from './image-understanding.parser.js';

export class ImageUnderstandingService {
  async analyze(input: { buffer: Buffer; contentType: string; namespace: string }): Promise<ImageUnderstandingResult> {
    // Do not persist base64. It exists only in this request payload for vision analysis.
    const imageUrl = `data:${input.contentType};base64,${input.buffer.toString('base64')}`;
    const content = await smlGatewayService.chat({
      model: env.VISION_AGENT_MODEL,
      fallbackModels: modelList(env.VISION_FALLBACK_MODELS),
      timeoutMs: env.THCLAWS_TIMEOUT_MS,
      strategy: 'strongest',
      maxLatencyMs: null,
      messages: [
        {
          role: 'system',
          content: [
            'You are ThaiAiAgent vision secretary.',
            'Read any image and answer in Thai.',
            'If it is a Thai bank transfer slip, extract amount, sender, receiver, masked accounts, bank, transfer time, and explain who transferred to whom.',
            'Classify directionHint only if clear: INCOME when money comes into the user, EXPENSE when money goes out from the user, TRANSFER when same owner moves money, UNKNOWN when not enough information.',
            'Never invent account ownership. If unsure, use UNKNOWN.',
            'Return only one JSON object with keys finalReply,isSlip,amount,currency,transferAt,fromAccountMasked,toAccountMasked,fromName,toName,bankName,directionHint,confidence.',
            'Use double-quoted JSON strings only. Do not use Python-style single quotes, do not wrap the response in a list, and do not include code fences.',
            'Escape newlines in JSON string values.',
            'Keep finalReply short Thai and useful.'
          ].join('\n')
        },
        {
          role: 'user',
          content: [
            { type: 'text', text: `namespace=${input.namespace}. อ่านรูปนี้และสรุปให้ผู้ใช้` },
            { type: 'image_url', image_url: { url: imageUrl } }
          ]
        }
      ]
    });

    const { result, usedFallback } = await coreClient.parseVisionOrFallback({ namespace: input.namespace, content });
    if (usedFallback) {
      logger.warn({
        contentPreview: content.replace(/\s+/g, ' ').slice(0, 300)
      }, 'image understanding returned non-json content');
    }
    logger.info({
      namespace: input.namespace,
      isSlip: result.isSlip,
      directionHint: result.directionHint,
      confidence: result.confidence,
      parser: coreClient.enabled ? 'core' : 'node'
    }, 'image understood by AI');
    return result;
  }
}

export const imageUnderstandingService = new ImageUnderstandingService();

function modelList(value: string) {
  return value.split(',').map((model) => model.trim()).filter(Boolean);
}
