-- Enable extensions used by the new tables.
CREATE EXTENSION IF NOT EXISTS "vector";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- Append-only ledger event log.
CREATE TABLE IF NOT EXISTS "ledger_events" (
    "id"              BIGSERIAL  PRIMARY KEY,
    "namespace"       TEXT       NOT NULL,
    "userId"          TEXT       NOT NULL,
    "type"            TEXT       NOT NULL,
    "payload"         JSONB      NOT NULL,
    "causationId"     TEXT,
    "correlationId"   TEXT,
    "occurredAt"      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS "ledger_events_ns_id_idx"
    ON "ledger_events" ("namespace", "id");
CREATE INDEX IF NOT EXISTS "ledger_events_user_type_time_idx"
    ON "ledger_events" ("userId", "type", "occurredAt");

-- Transactional outbox.
CREATE TABLE IF NOT EXISTS "event_outbox" (
    "id"          BIGSERIAL PRIMARY KEY,
    "topic"       TEXT      NOT NULL,
    "payload"     JSONB     NOT NULL,
    "status"      TEXT      NOT NULL DEFAULT 'pending',
    "attempts"    INT       NOT NULL DEFAULT 0,
    "lastError"   TEXT,
    "publishedAt" TIMESTAMPTZ,
    "createdAt"   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updatedAt"   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS "event_outbox_status_idx"
    ON "event_outbox" ("status", "createdAt");
CREATE INDEX IF NOT EXISTS "event_outbox_topic_status_idx"
    ON "event_outbox" ("topic", "status");

-- Vector store for memory retrieval. The default 1024 dimension matches BGE-M3
-- and Cohere v3 multilingual embeddings; adjust if the embedding model changes.
CREATE TABLE IF NOT EXISTS "memory_embeddings" (
    "id"          TEXT          PRIMARY KEY,
    "namespace"   TEXT          NOT NULL,
    "sourceType"  TEXT          NOT NULL,
    "sourceId"    TEXT          NOT NULL,
    "content"     TEXT          NOT NULL,
    "embedding"   vector(1024)  NOT NULL,
    "metadata"    JSONB,
    "createdAt"   TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    "updatedAt"   TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS "memory_embeddings_ns_src_uniq"
    ON "memory_embeddings" ("namespace", "sourceType", "sourceId");
CREATE INDEX IF NOT EXISTS "memory_embeddings_ns_src_idx"
    ON "memory_embeddings" ("namespace", "sourceType");

-- ANN index. HNSW is faster than IVFFlat for our retrieval pattern
-- (low write rate, sub-100ms queries, < 1M rows expected per namespace).
CREATE INDEX IF NOT EXISTS "memory_embeddings_hnsw_cosine"
    ON "memory_embeddings" USING hnsw ("embedding" vector_cosine_ops);
