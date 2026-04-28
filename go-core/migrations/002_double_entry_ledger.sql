ALTER TABLE "transactions"
    ADD COLUMN IF NOT EXISTS "namespace" TEXT;

UPDATE "transactions" t
SET "namespace" = a."namespace"
FROM "agents" a
WHERE t."agentId" = a."id"
  AND t."namespace" IS NULL;

CREATE INDEX IF NOT EXISTS "transactions_namespace_happened_idx"
    ON "transactions" ("namespace", "happenedAt" DESC);
CREATE INDEX IF NOT EXISTS "transactions_user_namespace_happened_idx"
    ON "transactions" ("userId", "namespace", "happenedAt" DESC);

CREATE TABLE IF NOT EXISTS "chart_accounts" (
    "id"            TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    "namespace"     TEXT NOT NULL,
    "code"          TEXT NOT NULL,
    "name"          TEXT NOT NULL,
    "accountType"   TEXT NOT NULL,
    "normalBalance" TEXT NOT NULL,
    "isSystem"      BOOLEAN NOT NULL DEFAULT TRUE,
    "createdAt"     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updatedAt"     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE ("namespace", "code")
);
CREATE INDEX IF NOT EXISTS "chart_accounts_ns_type_idx"
    ON "chart_accounts" ("namespace", "accountType", "code");

CREATE TABLE IF NOT EXISTS "journal_entries" (
    "id"                  TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    "namespace"           TEXT NOT NULL,
    "userId"              TEXT NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
    "agentId"             TEXT,
    "sourceTransactionId" TEXT,
    "memo"                TEXT,
    "status"              TEXT NOT NULL DEFAULT 'POSTED',
    "causationId"         TEXT,
    "occurredAt"          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "createdAt"           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS "journal_entries_ns_time_idx"
    ON "journal_entries" ("namespace", "occurredAt" DESC);
CREATE INDEX IF NOT EXISTS "journal_entries_user_time_idx"
    ON "journal_entries" ("userId", "occurredAt" DESC);
CREATE UNIQUE INDEX IF NOT EXISTS "journal_entries_ns_cause_uniq"
    ON "journal_entries" ("namespace", "causationId")
    WHERE "causationId" IS NOT NULL;

CREATE TABLE IF NOT EXISTS "journal_lines" (
    "id"              TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    "journalEntryId"  TEXT NOT NULL REFERENCES "journal_entries"("id") ON DELETE CASCADE,
    "namespace"       TEXT NOT NULL,
    "accountId"       TEXT NOT NULL REFERENCES "chart_accounts"("id"),
    "debit"           NUMERIC(12,2) NOT NULL DEFAULT 0,
    "credit"          NUMERIC(12,2) NOT NULL DEFAULT 0,
    "currency"        TEXT NOT NULL DEFAULT 'THB',
    "memo"            TEXT,
    "createdAt"       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK ("debit" >= 0 AND "credit" >= 0),
    CHECK (("debit" > 0 AND "credit" = 0) OR ("credit" > 0 AND "debit" = 0))
);
CREATE INDEX IF NOT EXISTS "journal_lines_ns_account_idx"
    ON "journal_lines" ("namespace", "accountId");
CREATE INDEX IF NOT EXISTS "journal_lines_entry_idx"
    ON "journal_lines" ("journalEntryId");
