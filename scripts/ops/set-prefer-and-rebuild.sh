#!/bin/sh
set -eu

cd /opt/thaiaiagent
tmp="$(mktemp)"
grep -v -E '^(GENERAL_AGENT_MODEL|SMLGATEWAY_PREFER_PROVIDERS|SMLGATEWAY_TEXT_EXCLUDE_PROVIDERS)=' .env > "$tmp" || true
printf '\nGENERAL_AGENT_MODEL=sml/thai\n' >> "$tmp"
printf '\nSMLGATEWAY_PREFER_PROVIDERS=typhoon,thaillm,groq,cerebras\n' >> "$tmp"
printf 'SMLGATEWAY_TEXT_EXCLUDE_PROVIDERS=mistral\n' >> "$tmp"
cat "$tmp" > .env
rm -f "$tmp"

docker compose -f docker-compose.prod.yml up -d --build api worker
