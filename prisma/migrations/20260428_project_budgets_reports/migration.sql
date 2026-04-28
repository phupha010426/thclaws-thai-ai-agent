ALTER TABLE "projects"
    ADD COLUMN IF NOT EXISTS "budgetAmount" NUMERIC(12,2),
    ADD COLUMN IF NOT EXISTS "targetDate" TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS "projects_user_ns_target_idx"
    ON "projects" ("userId", "namespace", "targetDate")
    WHERE "deletedAt" IS NULL;
