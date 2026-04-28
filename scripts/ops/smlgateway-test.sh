#!/bin/sh
set -eu

cd /opt/thaiaiagent
docker compose -f docker-compose.prod.yml exec -T api node - <<'NODE'
const base = String(process.env.THCLAWS_GATEWAY_URL || '').replace(/\/$/, '');
const auth = `Bearer ${process.env.THCLAWS_API_KEY}`;
const commonHeaders = { authorization: auth, 'content-type': 'application/json' };

async function request(name, path, options = {}) {
  const startedAt = Date.now();
  try {
    const response = await fetch(`${base}${path}`, {
      method: options.method ?? 'GET',
      headers: options.headers ?? { authorization: auth },
      body: options.body ? JSON.stringify(options.body) : undefined,
      signal: AbortSignal.timeout(options.timeoutMs ?? 45000)
    });
    const text = await response.text();
    const data = {
      name,
      status: response.status,
      ms: Date.now() - startedAt,
      requestId: response.headers.get('x-smlgateway-request-id'),
      model: response.headers.get('x-smlgateway-model'),
      provider: response.headers.get('x-smlgateway-provider'),
      body: text.slice(0, 700)
    };
    console.log(JSON.stringify(data));
  } catch (error) {
    console.log(JSON.stringify({ name, error: error.name, message: error.message, ms: Date.now() - startedAt }));
  }
}

await request('models', '/models');
await request('search_thai', '/models/search?category=thai&top=5');
await request('search_vision', '/models/search?supports_vision=1&top=5');

const chatMessages = [
  { role: 'system', content: 'ตอบภาษาไทยสั้น กระชับ' },
  { role: 'user', content: 'ขอสูตรส้มตำแบบง่าย' }
];

for (const model of [
  'groq/meta-llama/llama-4-scout-17b-16e-instruct',
  'groq/llama-3.1-8b-instant',
  'sml/auto',
  'sml/thai',
  'sml/fast'
]) {
  await request(`chat_${model}`, '/chat/completions', {
    method: 'POST',
    headers: commonHeaders,
    body: { model, messages: chatMessages, max_tokens: 220, temperature: 0 },
    timeoutMs: 30000
  });
}

await request('structured_auto', '/structured', {
  method: 'POST',
  headers: commonHeaders,
  body: {
    model: 'groq/meta-llama/llama-4-scout-17b-16e-instruct',
    messages: [{ role: 'user', content: 'ตอบ JSON สูตรส้มตำแบบสั้น' }],
    schema: {
      type: 'object',
      required: ['finalReply', 'actions'],
      properties: {
        finalReply: { type: 'string' },
        actions: {
          type: 'array',
          items: {
            type: 'object',
            required: ['type'],
            properties: { type: { type: 'string', enum: ['no_op'] } }
          }
        }
      }
    },
    max_retries: 2
  },
  timeoutMs: 30000
});

const onePixelPng = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII=';
await request('vision_direct', '/chat/completions', {
  method: 'POST',
  headers: commonHeaders,
  body: {
    model: 'groq/meta-llama/llama-4-scout-17b-16e-instruct',
    messages: [{
      role: 'user',
      content: [
        { type: 'text', text: 'อธิบายรูปนี้สั้นๆ' },
        { type: 'image_url', image_url: { url: `data:image/png;base64,${onePixelPng}` } }
      ]
    }],
    max_tokens: 120,
    temperature: 0
  },
  timeoutMs: 30000
});
NODE
