ALTER TABLE "transactions"
    ADD COLUMN IF NOT EXISTS "counterpartyName" TEXT,
    ADD COLUMN IF NOT EXISTS "counterpartyRole" TEXT,
    ADD COLUMN IF NOT EXISTS "updatedAt" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN IF NOT EXISTS "deletedAt" TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS "transactions_user_namespace_active_happened_idx"
    ON "transactions" ("userId", "namespace", "happenedAt" DESC)
    WHERE "deletedAt" IS NULL;

CREATE TABLE IF NOT EXISTS "projects" (
    "id"          TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    "userId"      TEXT NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
    "namespace"   TEXT NOT NULL,
    "name"        TEXT NOT NULL,
    "description" TEXT,
    "status"      TEXT NOT NULL DEFAULT 'ACTIVE',
    "color"       TEXT NOT NULL DEFAULT '#0f766e',
    "sortOrder"   INT NOT NULL DEFAULT 0,
    "createdAt"   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updatedAt"   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "deletedAt"   TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS "projects_ns_name_active_uniq"
    ON "projects" ("namespace", lower("name"))
    WHERE "deletedAt" IS NULL;
CREATE INDEX IF NOT EXISTS "projects_user_ns_status_idx"
    ON "projects" ("userId", "namespace", "status", "updatedAt" DESC)
    WHERE "deletedAt" IS NULL;

CREATE TABLE IF NOT EXISTS "transaction_project_allocations" (
    "id"            TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    "namespace"     TEXT NOT NULL,
    "userId"        TEXT NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
    "transactionId" TEXT NOT NULL REFERENCES "transactions"("id") ON DELETE CASCADE,
    "projectId"     TEXT NOT NULL REFERENCES "projects"("id") ON DELETE RESTRICT,
    "amount"        NUMERIC(12,2) NOT NULL,
    "percent"       NUMERIC(7,4) NOT NULL DEFAULT 0,
    "note"          TEXT,
    "createdAt"     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updatedAt"     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK ("amount" >= 0),
    CHECK ("percent" >= 0)
);
CREATE UNIQUE INDEX IF NOT EXISTS "tx_project_alloc_tx_project_uniq"
    ON "transaction_project_allocations" ("transactionId", "projectId");
CREATE INDEX IF NOT EXISTS "tx_project_alloc_project_time_idx"
    ON "transaction_project_allocations" ("namespace", "projectId", "createdAt" DESC);
CREATE INDEX IF NOT EXISTS "tx_project_alloc_user_tx_idx"
    ON "transaction_project_allocations" ("userId", "transactionId");

CREATE INDEX IF NOT EXISTS "journal_entries_ns_status_time_idx"
    ON "journal_entries" ("namespace", "status", "occurredAt" DESC);
