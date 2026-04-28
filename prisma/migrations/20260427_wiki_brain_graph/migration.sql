-- Personal wiki / knowledge graph. Every row carries a namespace column so the
-- LINE userId scope is enforced both by foreign keys and by service-layer
-- filters. Cross-namespace edges are explicitly blocked by a CHECK constraint
-- to make leakage impossible at the storage layer (defense in depth).

CREATE TABLE IF NOT EXISTS "wiki_pages" (
    "id"         TEXT       PRIMARY KEY,
    "namespace"  TEXT       NOT NULL,
    "slug"       TEXT       NOT NULL,
    "title"      TEXT       NOT NULL,
    "kind"       TEXT       NOT NULL DEFAULT 'concept',
    "summary"    TEXT,
    "body_md"    TEXT       NOT NULL DEFAULT '',
    "importance" DOUBLE PRECISION NOT NULL DEFAULT 0.5,
    "source"     TEXT       NOT NULL DEFAULT 'auto',
    "metadata"   JSONB,
    "createdAt"  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updatedAt"  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS "wiki_pages_ns_slug_uniq"
    ON "wiki_pages" ("namespace", "slug");
CREATE INDEX IF NOT EXISTS "wiki_pages_ns_kind_idx"
    ON "wiki_pages" ("namespace", "kind");
CREATE INDEX IF NOT EXISTS "wiki_pages_ns_updated_idx"
    ON "wiki_pages" ("namespace", "updatedAt" DESC);

CREATE TABLE IF NOT EXISTS "wiki_aliases" (
    "id"         BIGSERIAL  PRIMARY KEY,
    "pageId"     TEXT       NOT NULL REFERENCES "wiki_pages"("id") ON DELETE CASCADE,
    "namespace"  TEXT       NOT NULL,
    "alias"      TEXT       NOT NULL,
    "createdAt"  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS "wiki_aliases_ns_alias_uniq"
    ON "wiki_aliases" ("namespace", "alias");
CREATE INDEX IF NOT EXISTS "wiki_aliases_page_idx"
    ON "wiki_aliases" ("pageId");

CREATE TABLE IF NOT EXISTS "wiki_edges" (
    "id"         BIGSERIAL  PRIMARY KEY,
    "namespace"  TEXT       NOT NULL,
    "fromPageId" TEXT       NOT NULL REFERENCES "wiki_pages"("id") ON DELETE CASCADE,
    "toPageId"   TEXT       NOT NULL REFERENCES "wiki_pages"("id") ON DELETE CASCADE,
    "relation"   TEXT       NOT NULL,
    "weight"     DOUBLE PRECISION NOT NULL DEFAULT 1,
    "metadata"   JSONB,
    "createdAt"  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS "wiki_edges_uniq"
    ON "wiki_edges" ("namespace", "fromPageId", "toPageId", "relation");
CREATE INDEX IF NOT EXISTS "wiki_edges_from_idx" ON "wiki_edges" ("fromPageId");
CREATE INDEX IF NOT EXISTS "wiki_edges_to_idx"   ON "wiki_edges" ("toPageId");

-- Defense in depth: an edge can never connect pages that belong to different
-- LINE userId namespaces. Trigger runs on insert + update so any code path
-- (including raw SQL or future migrations) is forced to obey the rule.
CREATE OR REPLACE FUNCTION enforce_wiki_edge_same_namespace() RETURNS TRIGGER AS $$
DECLARE
    from_ns TEXT;
    to_ns   TEXT;
BEGIN
    SELECT namespace INTO from_ns FROM wiki_pages WHERE id = NEW."fromPageId";
    SELECT namespace INTO to_ns   FROM wiki_pages WHERE id = NEW."toPageId";
    IF from_ns IS DISTINCT FROM NEW.namespace OR to_ns IS DISTINCT FROM NEW.namespace THEN
        RAISE EXCEPTION 'wiki_edges namespace mismatch: edge=%, from=%, to=%',
            NEW.namespace, from_ns, to_ns;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS wiki_edges_namespace_check ON "wiki_edges";
CREATE TRIGGER wiki_edges_namespace_check
    BEFORE INSERT OR UPDATE ON "wiki_edges"
    FOR EACH ROW EXECUTE FUNCTION enforce_wiki_edge_same_namespace();

-- Same guard for aliases — alias.namespace must equal its page's namespace.
CREATE OR REPLACE FUNCTION enforce_wiki_alias_same_namespace() RETURNS TRIGGER AS $$
DECLARE
    page_ns TEXT;
BEGIN
    SELECT namespace INTO page_ns FROM wiki_pages WHERE id = NEW."pageId";
    IF page_ns IS DISTINCT FROM NEW.namespace THEN
        RAISE EXCEPTION 'wiki_aliases namespace mismatch: alias=%, page=%',
            NEW.namespace, page_ns;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS wiki_aliases_namespace_check ON "wiki_aliases";
CREATE TRIGGER wiki_aliases_namespace_check
    BEFORE INSERT OR UPDATE ON "wiki_aliases"
    FOR EACH ROW EXECUTE FUNCTION enforce_wiki_alias_same_namespace();
