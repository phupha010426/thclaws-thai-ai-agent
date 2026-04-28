#!/bin/sh
set -eu

cd /opt/thaiaiagent
echo "== compose ps =="
docker compose -f docker-compose.prod.yml ps
echo "== recent logs =="
docker compose -f docker-compose.prod.yml logs --since=30m api worker
echo "== recent mongo counts =="
docker compose -f docker-compose.prod.yml exec -T mongodb sh -c \
  'mongosh --quiet --username "$MONGO_INITDB_ROOT_USERNAME" --password "$MONGO_INITDB_ROOT_PASSWORD" --authenticationDatabase admin thaiaiagent --eval '\''db.getCollectionNames().sort().forEach((name) => print(name + ":" + db[name].countDocuments()))'\'''
echo "== recent event logs =="
docker compose -f docker-compose.prod.yml exec -T mongodb sh -c \
  'mongosh --quiet --username "$MONGO_INITDB_ROOT_USERNAME" --password "$MONGO_INITDB_ROOT_PASSWORD" --authenticationDatabase admin thaiaiagent --eval '\''db.event_logs.find({}, {eventType:1, namespace:1, content:1, createdAt:1, metadata:1, _id:0}).sort({createdAt:-1}).limit(10).forEach((doc) => printjson(doc))'\'''
