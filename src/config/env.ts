import 'dotenv/config';
import { z } from 'zod';

const booleanFromEnv = z.preprocess((value) => {
  if (typeof value !== 'string') return value;
  const normalized = value.trim().toLowerCase();
  if (['true', '1', 'yes', 'y'].includes(normalized)) return true;
  if (['false', '0', 'no', 'n', ''].includes(normalized)) return false;
  return value;
}, z.boolean());

const envSchema = z.object({
  APP_NAME: z.string().default('ThaiAiAgent'),
  NODE_ENV: z.string().default('development'),
  LOG_LEVEL: z.string().default('info'),
  PORT: z.coerce.number().default(3000),
  LINE_CHANNEL_ACCESS_TOKEN: z.string().min(1),
  LINE_CHANNEL_SECRET: z.string().min(1),
  POSTGRES_URL: z.string().min(1),
  MONGODB_URL: z.string().min(1),
  REDIS_URL: z.string().min(1),
  REDIS_QUEUE_PREFIX: z.string().default('thaiaiagent'),
  MINIO_ENDPOINT: z.string().default('localhost'),
  MINIO_PORT: z.coerce.number().default(9000),
  MINIO_USE_SSL: booleanFromEnv.default(false),
  MINIO_ACCESS_KEY: z.string().min(1),
  MINIO_SECRET_KEY: z.string().min(1),
  MINIO_BUCKET: z.string().default('thaiaiagent'),
  WEBHOOK_BODY_LIMIT: z.string().default('1mb'),
  WEBHOOK_REPLAY_WINDOW_SECONDS: z.coerce.number().default(300),
  DEFAULT_AGENT_MODEL: z.string().default('sml/auto'),
  FAST_AGENT_MODEL: z.string().default('sml/fast'),
  GENERAL_AGENT_MODEL: z.string().default('sml/thai'),
  VISION_AGENT_MODEL: z.string().default('sml/auto'),
  TEXT_FALLBACK_MODELS: z.string().default('groq/meta-llama/llama-4-scout-17b-16e-instruct,groq/llama-3.1-8b-instant'),
  VISION_FALLBACK_MODELS: z.string().default('mistral/mistral-medium-3-5,groq/meta-llama/llama-4-scout-17b-16e-instruct'),
  THCLAWS_GATEWAY_URL: z.string().url().default('http://localhost:8787'),
  THCLAWS_API_KEY: z.string().default('dev-key'),
  THCLAWS_TIMEOUT_MS: z.coerce.number().int().positive().default(30000),
  SMLGATEWAY_MAX_LATENCY_MS: z.coerce.number().int().positive().default(3000),
  SMLGATEWAY_PREFER_PROVIDERS: z.string().default('typhoon,thaillm,groq,cerebras'),
  SMLGATEWAY_TEXT_EXCLUDE_PROVIDERS: z.string().default('mistral'),
  CORE_BASE_URL: z.string().default(''),
  CORE_TIMEOUT_MS: z.coerce.number().int().positive().default(2000),
  NATS_URL: z.string().default(''),
  NATS_STREAM_NAME: z.string().default('thaiagent'),
  EMBEDDING_MODEL: z.string().default('bge-m3'),
  EMBEDDING_FALLBACK_MODELS: z.string().default('cohere/embed-multilingual-v3.0,openai/text-embedding-3-large')
});

export const env = envSchema.parse(process.env);
