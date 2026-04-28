#!/bin/sh
set -eu

rm -f /tmp/thaiaiagent-image-flow-smoke.sh /tmp/thaiaiagent-minio-smoke.sh /tmp/thaiaiagent-smoke.sh
cd /opt/thaiaiagent
docker compose -f docker-compose.prod.yml ps
docker compose -f docker-compose.prod.yml logs --tail=120 api worker
