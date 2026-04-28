CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "vector";

CREATE TABLE IF NOT EXISTS "users" (
    "id"          TEXT PRIMARY KEY,
    "lineUserId"  TEXT NOT NULL UNIQUE,
    "displayName" TEXT,
    "language"    TEXT NOT NULL DEFAULT 'th',
    "createdAt"   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updatedAt"   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS "agents" (
    "id"        TEXT PRIMARY KEY,
    "ownerId"   TEXT NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
    "type"      TEXT NOT NULL,
    "name"      TEXT NOT NULL,
    "namespace" TEXT NOT NULL UNIQUE,
    "isActive"  BOOLEAN NOT NULL DEFAULT TRUE,
    "createdAt" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updatedAt" TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS "agents_owner_type_active_idx"
    ON "agents" ("ownerId", "type", "isActive");

CREATE TABLE IF NOT EXISTS "transactions" (
    "id"         TEXT PRIMARY KEY,
    "userId"     TEXT NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
    "agentId"    TEXT,
    "scope"      TEXT NOT NULL DEFAULT 'PERSONAL',
    "scopeId"    TEXT,
    "type"       TEXT NOT NULL,
    "amount"     NUMERIC(12,2) NOT NULL,
    "currency"   TEXT NOT NULL DEFAULT 'THB',
    "category"   TEXT NOT NULL,
    "note"       TEXT,
    "source"     TEXT NOT NULL DEFAULT 'line',
    "confidence" DOUBLE PRECISION NOT NULL DEFAULT 1,
    "happenedAt" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "createdAt"  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS "transactions_user_happened_idx"
    ON "transactions" ("userId", "happenedAt");
CREATE INDEX IF NOT EXISTS "transactions_user_type_happened_idx"
    ON "transactions" ("userId", "type", "happenedAt");
CREATE INDEX IF NOT EXISTS "transactions_scope_happened_idx"
    ON "transactions" ("scope", "scopeId", "happenedAt");

CREATE TABLE IF NOT EXISTS "categories" (
    "id"        TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    "scope"     TEXT NOT NULL DEFAULT 'PERSONAL',
    "type"      TEXT NOT NULL,
    "name"      TEXT NOT NULL,
    "isDefault" BOOLEAN NOT NULL DEFAULT FALSE,
    "createdAt" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE ("scope", "type", "name")
);

CREATE TABLE IF NOT EXISTS "budgets" (
    "id"        TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    "userId"    TEXT NOT NULL,
    "scope"     TEXT NOT NULL DEFAULT 'PERSONAL',
    "scopeId"   TEXT,
    "category"  TEXT NOT NULL,
    "amount"    NUMERIC(12,2) NOT NULL,
    "period"    TEXT NOT NULL DEFAULT 'monthly',
    "createdAt" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updatedAt" TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS "budgets_user_scope_idx" ON "budgets" ("userId", "scope", "scopeId");

CREATE TABLE IF NOT EXISTS "goals" (
    "id"        TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    "userId"    TEXT NOT NULL,
    "name"      TEXT NOT NULL,
    "target"    NUMERIC(12,2) NOT NULL,
    "current"   NUMERIC(12,2) NOT NULL DEFAULT 0,
    "dueDate"   TIMESTAMPTZ,
    "createdAt" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updatedAt" TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS "goals_user_idx" ON "goals" ("userId");

CREATE TABLE IF NOT EXISTS "permissions" (
    "id"           TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    "actorAgentId" TEXT NOT NULL,
    "resource"     TEXT NOT NULL,
    "action"       TEXT NOT NULL,
    "scope"        TEXT NOT NULL,
    "expiresAt"    TIMESTAMPTZ,
    "createdAt"    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS "permissions_lookup_idx" ON "permissions" ("actorAgentId", "resource", "action");

CREATE TABLE IF NOT EXISTS "consents" (
    "id"        TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    "userId"    TEXT NOT NULL,
    "purpose"   TEXT NOT NULL,
    "status"    TEXT NOT NULL DEFAULT 'GRANTED',
    "grantedAt" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "revokedAt" TIMESTAMPTZ,
    UNIQUE ("userId", "purpose")
);
CREATE INDEX IF NOT EXISTS "consents_user_purpose_status_idx" ON "consents" ("userId", "purpose", "status");

CREATE TABLE IF NOT EXISTS "audit_logs" (
    "id"        TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    "actorId"   TEXT,
    "action"    TEXT NOT NULL,
    "resource"  TEXT NOT NULL,
    "metadata"  JSONB,
    "createdAt" TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS "audit_logs_actor_time_idx" ON "audit_logs" ("actorId", "createdAt");
CREATE INDEX IF NOT EXISTS "audit_logs_action_time_idx" ON "audit_logs" ("action", "createdAt");

CREATE TABLE IF NOT EXISTS "tool_calls" (
    "id"        TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    "actorId"   TEXT,
    "toolName"  TEXT NOT NULL,
    "status"    TEXT NOT NULL,
    "latencyMs" INT,
    "metadata"  JSONB,
    "createdAt" TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS "cost_logs" (
    "id"           TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    "actorId"      TEXT,
    "provider"     TEXT NOT NULL,
    "model"        TEXT NOT NULL,
    "inputTokens"  INT NOT NULL DEFAULT 0,
    "outputTokens" INT NOT NULL DEFAULT 0,
    "costUsd"      NUMERIC(12,6) NOT NULL DEFAULT 0,
    "createdAt"    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS "ledger_events" (
    "id"            BIGSERIAL PRIMARY KEY,
    "namespace"     TEXT NOT NULL,
    "userId"        TEXT NOT NULL,
    "type"          TEXT NOT NULL,
    "payload"       JSONB NOT NULL,
    "causationId"   TEXT,
    "correlationId" TEXT,
    "occurredAt"    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS "ledger_events_ns_id_idx" ON "ledger_events" ("namespace", "id");
CREATE INDEX IF NOT EXISTS "ledger_events_user_type_time_idx" ON "ledger_events" ("userId", "type", "occurredAt");
CREATE UNIQUE INDEX IF NOT EXISTS "ledger_events_ns_cause_tx_uniq"
    ON "ledger_events" ("namespace", "causationId")
    WHERE "causationId" IS NOT NULL AND "type" = 'tx.recorded';

CREATE TABLE IF NOT EXISTS "event_outbox" (
    "id"          BIGSERIAL PRIMARY KEY,
    "topic"       TEXT NOT NULL,
    "payload"     JSONB NOT NULL,
    "status"      TEXT NOT NULL DEFAULT 'pending',
    "attempts"    INT NOT NULL DEFAULT 0,
    "lastError"   TEXT,
    "publishedAt" TIMESTAMPTZ,
    "createdAt"   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updatedAt"   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS "event_outbox_status_idx" ON "event_outbox" ("status", "createdAt");
CREATE INDEX IF NOT EXISTS "event_outbox_topic_status_idx" ON "event_outbox" ("topic", "status");

CREATE TABLE IF NOT EXISTS "wiki_pages" (
    "id"         TEXT PRIMARY KEY,
    "namespace"  TEXT NOT NULL,
    "slug"       TEXT NOT NULL,
    "title"      TEXT NOT NULL,
    "kind"       TEXT NOT NULL DEFAULT 'concept',
    "summary"    TEXT,
    "body_md"    TEXT NOT NULL DEFAULT '',
    "importance" DOUBLE PRECISION NOT NULL DEFAULT 0.5,
    "source"     TEXT NOT NULL DEFAULT 'auto',
    "metadata"   JSONB,
    "createdAt"  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updatedAt"  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE ("namespace", "slug")
);
CREATE INDEX IF NOT EXISTS "wiki_pages_ns_kind_idx" ON "wiki_pages" ("namespace", "kind");
CREATE INDEX IF NOT EXISTS "wiki_pages_ns_updated_idx" ON "wiki_pages" ("namespace", "updatedAt" DESC);

CREATE TABLE IF NOT EXISTS "wiki_aliases" (
    "id"         BIGSERIAL PRIMARY KEY,
    "pageId"     TEXT NOT NULL REFERENCES "wiki_pages"("id") ON DELETE CASCADE,
    "namespace"  TEXT NOT NULL,
    "alias"      TEXT NOT NULL,
    "createdAt"  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE ("namespace", "alias")
);
CREATE INDEX IF NOT EXISTS "wiki_aliases_page_idx" ON "wiki_aliases" ("pageId");

CREATE TABLE IF NOT EXISTS "wiki_edges" (
    "id"         BIGSERIAL PRIMARY KEY,
    "namespace"  TEXT NOT NULL,
    "fromPageId" TEXT NOT NULL REFERENCES "wiki_pages"("id") ON DELETE CASCADE,
    "toPageId"   TEXT NOT NULL REFERENCES "wiki_pages"("id") ON DELETE CASCADE,
    "relation"   TEXT NOT NULL,
    "weight"     DOUBLE PRECISION NOT NULL DEFAULT 1,
    "metadata"   JSONB,
    "createdAt"  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE ("namespace", "fromPageId", "toPageId", "relation")
);
CREATE INDEX IF NOT EXISTS "wiki_edges_from_idx" ON "wiki_edges" ("fromPageId");
CREATE INDEX IF NOT EXISTS "wiki_edges_to_idx" ON "wiki_edges" ("toPageId");

CREATE TABLE IF NOT EXISTS "memory_embeddings" (
    "id"         TEXT PRIMARY KEY,
    "namespace"  TEXT NOT NULL,
    "sourceType" TEXT NOT NULL,
    "sourceId"   TEXT NOT NULL,
    "content"    TEXT NOT NULL,
    "embedding"  vector(1024) NOT NULL,
    "metadata"   JSONB,
    "createdAt"  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updatedAt"  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE ("namespace", "sourceType", "sourceId")
);
CREATE INDEX IF NOT EXISTS "memory_embeddings_ns_src_idx" ON "memory_embeddings" ("namespace", "sourceType");
