-- Migration 005: Household and Business support tables
-- These tables were in Prisma schema but not yet created via Go migrations.

CREATE TABLE IF NOT EXISTS households (
    "id"        TEXT PRIMARY KEY,
    "name"      TEXT NOT NULL,
    "ownerId"   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    "createdAt"  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS households_owner_idx ON households ("ownerId");

CREATE TABLE IF NOT EXISTS household_members (
    "id"          TEXT PRIMARY KEY,
    "householdId" TEXT NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    "userId"      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    "role"        TEXT NOT NULL DEFAULT 'member',
    "createdAt"   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE ("householdId", "userId")
);
CREATE INDEX IF NOT EXISTS hm_user_idx ON household_members ("userId");
CREATE INDEX IF NOT EXISTS hm_household_idx ON household_members ("householdId");

CREATE TABLE IF NOT EXISTS businesses (
    "id"        TEXT PRIMARY KEY,
    "ownerId"   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    "name"      TEXT NOT NULL,
    "type"      TEXT NOT NULL DEFAULT 'ร้านค้าเล็ก',
    "createdAt"  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS businesses_owner_idx ON businesses ("ownerId");
