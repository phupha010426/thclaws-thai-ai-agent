#!/bin/sh
set -eu

cd /opt/thaiaiagent
tmp="$(mktemp)"
grep -v -E '^(DEFAULT_AGENT_MODEL|FAST_AGENT_MODEL|VISION_AGENT_MODEL)=' .env > "$tmp" || true
cat >> "$tmp" <<'EOF'
DEFAULT_AGENT_MODEL=groq/meta-llama/llama-4-scout-17b-16e-instruct
FAST_AGENT_MODEL=groq/llama-3.1-8b-instant
VISION_AGENT_MODEL=groq/meta-llama/llama-4-scout-17b-16e-instruct
EOF
cat "$tmp" > .env
rm -f "$tmp"
docker compose -f docker-compose.prod.yml up -d api worker
