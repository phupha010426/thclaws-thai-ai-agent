package brain

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// WikiKind mirrors the TypeScript WikiKind union.
type WikiKind = string

const (
	KindPerson     WikiKind = "person"
	KindPlace      WikiKind = "place"
	KindItem       WikiKind = "item"
	KindEvent      WikiKind = "event"
	KindConcept    WikiKind = "concept"
	KindPreference WikiKind = "preference"
	KindRule       WikiKind = "rule"
)

// WikiPage is a node in the per-namespace knowledge graph.
type WikiPage struct {
	ID         string
	Namespace  string
	Slug       string
	Title      string
	Kind       string
	Summary    *string
	BodyMd     string
	Importance float64
	Source     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// WikiAlias is a lookup token that resolves to a WikiPage.
type WikiAlias struct {
	ID        int64
	PageID    string
	Namespace string
	Alias     string
	CreatedAt time.Time
}

// WikiEdge is a directed, labelled relation between two WikiPages.
type WikiEdge struct {
	ID         int64
	Namespace  string
	FromPageID string
	ToPageID   string
	Relation   string
	Weight     float64
	CreatedAt  time.Time
}

// UpsertPageInput carries the fields for WikiBrainService.UpsertPage.
type UpsertPageInput struct {
	Namespace  string
	Slug       string
	Title      string
	Kind       WikiKind
	Summary    string
	BodyMd     string
	Importance float64  // 0–1; 0 means "not set" (will use 0.5 default)
	Source     string
	Aliases    []string
}

// LinkPagesInput carries the fields for WikiBrainService.LinkPages.
type LinkPagesInput struct {
	Namespace  string
	FromPageID string
	ToPageID   string
	Relation   string
	Weight     float64 // 0 means "not set" (will use 1.0 default)
}

// RetrieveInput carries the fields for WikiBrainService.Retrieve.
type RetrieveInput struct {
	Namespace string
	Text      string
	Limit     int
}

// RetrievedPage is one ranked result from WikiBrainService.Retrieve.
type RetrievedPage struct {
	ID           string  `json:"id"`
	Slug         string  `json:"slug"`
	Title        string  `json:"title"`
	Kind         string  `json:"kind"`
	Summary      *string `json:"summary"`
	Importance   float64 `json:"importance"`
	Reason       string  `json:"reason"`       // "alias_match" | "edge_neighbor"
	Hops         int     `json:"hops"`
	MatchedAlias string  `json:"matchedAlias,omitempty"`
}

// WikiBrainService implements the per-namespace knowledge graph operations.
type WikiBrainService struct {
	pool  *pgxpool.Pool
	cache *BrainCache
	log   *slog.Logger
}

// NewWikiBrainService wires up the service with a pgx pool and Redis cache.
func NewWikiBrainService(pool *pgxpool.Pool, cache *BrainCache, log *slog.Logger) *WikiBrainService {
	return &WikiBrainService{pool: pool, cache: cache, log: log}
}

// UpsertPage creates or updates a wiki page and its aliases inside a single
// transaction. Returns the persisted page. Cache invalidation is fire-and-forget
// so a Redis outage cannot block a write.
func (s *WikiBrainService) UpsertPage(ctx context.Context, input UpsertPageInput) (WikiPage, error) {
	if input.Namespace == "" {
		return WikiPage{}, fmt.Errorf("wiki: namespace required")
	}
	slug := normalizeSlug(input.Slug)
	aliases := uniqueLower(append([]string{input.Title}, input.Aliases...))

	kind := input.Kind
	if kind == "" {
		kind = KindConcept
	}
	importance := clamp01(input.Importance)
	if importance == 0 {
		importance = 0.5
	}
	source := input.Source
	if source == "" {
		source = "auto"
	}

	var page WikiPage
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		const q = `
INSERT INTO wiki_pages (id, namespace, slug, title, kind, summary, body_md, importance, source, "createdAt", "updatedAt")
VALUES (gen_random_uuid()::text, $1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
ON CONFLICT (namespace, slug)
DO UPDATE SET title      = EXCLUDED.title,
              kind       = EXCLUDED.kind,
              summary    = EXCLUDED.summary,
              body_md    = EXCLUDED.body_md,
              importance = EXCLUDED.importance,
              source     = EXCLUDED.source,
              "updatedAt" = NOW()
RETURNING id, namespace, slug, title, kind, summary, body_md, importance, source, "createdAt", "updatedAt"`

		summaryVal := (*string)(nil)
		if input.Summary != "" {
			summaryVal = &input.Summary
		}
		bodyMd := input.BodyMd

		row := tx.QueryRow(ctx, q,
			input.Namespace, slug, strings.TrimSpace(input.Title),
			kind, summaryVal, bodyMd, importance, source)

		if err := row.Scan(
			&page.ID, &page.Namespace, &page.Slug, &page.Title,
			&page.Kind, &page.Summary, &page.BodyMd, &page.Importance,
			&page.Source, &page.CreatedAt, &page.UpdatedAt,
		); err != nil {
			return fmt.Errorf("wiki: upsert page scan: %w", err)
		}

		if len(aliases) > 0 {
			// Bulk insert is much cheaper than N individual upserts on the
			// hot extraction path. Conflicts are skipped — moving an alias to
			// a different page requires an explicit maintenance call.
			batch := &pgx.Batch{}
			const aliasQ = `
INSERT INTO wiki_aliases (namespace, "pageId", alias, "createdAt")
VALUES ($1, $2, $3, NOW())
ON CONFLICT (namespace, alias) DO NOTHING`
			for _, a := range aliases {
				batch.Queue(aliasQ, input.Namespace, page.ID, a)
			}
			br := tx.SendBatch(ctx, batch)
			if err := br.Close(); err != nil {
				return fmt.Errorf("wiki: alias batch: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return WikiPage{}, err
	}

	go s.cache.Invalidate(context.Background(), input.Namespace)
	return page, nil
}

// ListByImportance returns pages for `namespace` ordered by (importance desc,
// updatedAt desc). Used by memory.GetContext to surface top facts to the LLM.
func (s *WikiBrainService) ListByImportance(ctx context.Context, namespace string, limit int) ([]WikiPage, error) {
	if namespace == "" {
		return nil, fmt.Errorf("wiki: namespace required")
	}
	if limit <= 0 {
		limit = 8
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, namespace, slug, title, kind, summary, body_md, importance, source, "createdAt", "updatedAt"
		FROM wiki_pages
		WHERE namespace = $1
		ORDER BY importance DESC, "updatedAt" DESC
		LIMIT $2`, namespace, limit)
	if err != nil {
		return nil, fmt.Errorf("wiki: list by importance: %w", err)
	}
	defer rows.Close()
	var out []WikiPage
	for rows.Next() {
		var p WikiPage
		if err := rows.Scan(&p.ID, &p.Namespace, &p.Slug, &p.Title, &p.Kind, &p.Summary, &p.BodyMd, &p.Importance, &p.Source, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("wiki: scan: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// LinkPages upserts a directed edge between two pages in the same namespace.
// The Postgres trigger enforce_wiki_edge_same_namespace provides an additional
// cross-namespace guard at the storage layer.
func (s *WikiBrainService) LinkPages(ctx context.Context, input LinkPagesInput) (WikiEdge, error) {
	if input.Namespace == "" {
		return WikiEdge{}, fmt.Errorf("wiki: namespace required")
	}
	weight := input.Weight
	if weight == 0 {
		weight = 1.0
	}

	const q = `
INSERT INTO wiki_edges (namespace, "fromPageId", "toPageId", relation, weight, "createdAt")
VALUES ($1, $2, $3, $4, $5, NOW())
ON CONFLICT (namespace, "fromPageId", "toPageId", relation)
DO UPDATE SET weight = EXCLUDED.weight
RETURNING id, namespace, "fromPageId", "toPageId", relation, weight, "createdAt"`

	var edge WikiEdge
	row := s.pool.QueryRow(ctx, q,
		input.Namespace, input.FromPageID, input.ToPageID, input.Relation, weight)
	if err := row.Scan(
		&edge.ID, &edge.Namespace, &edge.FromPageID, &edge.ToPageID,
		&edge.Relation, &edge.Weight, &edge.CreatedAt,
	); err != nil {
		return WikiEdge{}, fmt.Errorf("wiki: link pages: %w", err)
	}

	go s.cache.Invalidate(context.Background(), input.Namespace)
	return edge, nil
}

// Retrieve returns the ranked slice of pages relevant to text via alias match
// + 1-hop edge expansion. This is the hot path on every LINE message.
//
// Stage 1 — cache lookup and tokenization are started concurrently so the
// Redis RTT overlaps with the CPU-bound tokenization work.
// Stage 2 — alias DB query.
// Stage 3 — 1-hop neighbor expansion.
func (s *WikiBrainService) Retrieve(ctx context.Context, input RetrieveInput) ([]RetrievedPage, error) {
	if input.Namespace == "" {
		return nil, fmt.Errorf("wiki: namespace required")
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 8
	}

	// Stage 1 — overlap Redis RTT with tokenization.
	type cacheRes = CacheResult[[]RetrievedPage]
	cacheCh := make(chan cacheRes, 1)
	tokenCh := make(chan []string, 1)

	go func() {
		cacheCh <- Get[[]RetrievedPage](ctx, s.cache, input.Namespace, input.Text)
	}()
	go func() {
		tokenCh <- tokenize(input.Text)
	}()

	cached := <-cacheCh
	tokens := <-tokenCh

	if cached.Hit {
		return cached.Value, nil
	}
	if len(tokens) == 0 {
		go Set(context.Background(), s.cache, input.Namespace, input.Text, []RetrievedPage{})
		return []RetrievedPage{}, nil
	}

	// Stage 2 — alias lookup.
	type aliasRow struct {
		aliasID        int64
		aliasNamespace string
		aliasStr       string
		pageID         string
		pageNamespace  string
		pageSlug       string
		pageTitle      string
		pageKind       string
		pageSummary    *string
		pageImportance float64
	}

	const aliasQ = `
SELECT a.id, a.namespace, a.alias,
       p.id, p.namespace, p.slug, p.title, p.kind, p.summary, p.importance
FROM wiki_aliases a
JOIN wiki_pages p ON p.id = a."pageId"
WHERE a.namespace = $1 AND a.alias = ANY($2::text[])`

	aliasRows, err := s.pool.Query(ctx, aliasQ, input.Namespace, tokens)
	if err != nil {
		return nil, fmt.Errorf("wiki: alias query: %w", err)
	}
	defer aliasRows.Close()

	seedPages := make(map[string]RetrievedPage)
	for aliasRows.Next() {
		var r aliasRow
		if err := aliasRows.Scan(
			&r.aliasID, &r.aliasNamespace, &r.aliasStr,
			&r.pageID, &r.pageNamespace, &r.pageSlug, &r.pageTitle,
			&r.pageKind, &r.pageSummary, &r.pageImportance,
		); err != nil {
			return nil, fmt.Errorf("wiki: alias scan: %w", err)
		}
		// Defense-in-depth: the trigger forbids cross-namespace aliases, but
		// we log and skip if one slips through (e.g. a future migration bug).
		if r.pageNamespace != input.Namespace {
			s.log.Error("wiki retrieval namespace mismatch",
				"aliasNamespace", r.aliasNamespace,
				"pageNamespace", r.pageNamespace,
				"requested", input.Namespace)
			continue
		}
		if _, exists := seedPages[r.pageID]; exists {
			continue
		}
		seedPages[r.pageID] = RetrievedPage{
			ID:           r.pageID,
			Slug:         r.pageSlug,
			Title:        r.pageTitle,
			Kind:         r.pageKind,
			Summary:      r.pageSummary,
			Importance:   r.pageImportance,
			Reason:       "alias_match",
			Hops:         0,
			MatchedAlias: r.aliasStr,
		}
	}
	aliasRows.Close()

	if len(seedPages) == 0 {
		go Set(context.Background(), s.cache, input.Namespace, input.Text, []RetrievedPage{})
		return []RetrievedPage{}, nil
	}

	seedIDs := make([]string, 0, len(seedPages))
	for id := range seedPages {
		seedIDs = append(seedIDs, id)
	}

	// Stage 3 — 1-hop neighbor expansion. Cap at 200 to guard against
	// a pathologically dense graph wedging the worker.
	type edgeRow struct {
		fromPageID        string
		fromPageNamespace string
		fromPageSlug      string
		fromPageTitle     string
		fromPageKind      string
		fromPageSummary   *string
		fromPageImportance float64
		toPageID          string
		toPageNamespace   string
		toPageSlug        string
		toPageTitle       string
		toPageKind        string
		toPageSummary     *string
		toPageImportance  float64
	}

	const edgeQ = `
SELECT f.id, f.namespace, f.slug, f.title, f.kind, f.summary, f.importance,
       t.id, t.namespace, t.slug, t.title, t.kind, t.summary, t.importance
FROM wiki_edges e
JOIN wiki_pages f ON f.id = e."fromPageId"
JOIN wiki_pages t ON t.id = e."toPageId"
WHERE e.namespace = $1
  AND (e."fromPageId" = ANY($2::text[]) OR e."toPageId" = ANY($2::text[]))
LIMIT 200`

	edgeRows, err := s.pool.Query(ctx, edgeQ, input.Namespace, seedIDs)
	if err != nil {
		return nil, fmt.Errorf("wiki: edge query: %w", err)
	}
	defer edgeRows.Close()

	all := make(map[string]RetrievedPage, len(seedPages))
	for k, v := range seedPages {
		all[k] = v
	}
	seedIDSet := make(map[string]bool, len(seedIDs))
	for _, id := range seedIDs {
		seedIDSet[id] = true
	}

	for edgeRows.Next() {
		var r edgeRow
		if err := edgeRows.Scan(
			&r.fromPageID, &r.fromPageNamespace, &r.fromPageSlug, &r.fromPageTitle,
			&r.fromPageKind, &r.fromPageSummary, &r.fromPageImportance,
			&r.toPageID, &r.toPageNamespace, &r.toPageSlug, &r.toPageTitle,
			&r.toPageKind, &r.toPageSummary, &r.toPageImportance,
		); err != nil {
			return nil, fmt.Errorf("wiki: edge scan: %w", err)
		}

		var candID, candNS, candSlug, candTitle, candKind string
		var candSummary *string
		var candImportance float64

		if seedIDSet[r.fromPageID] {
			candID, candNS = r.toPageID, r.toPageNamespace
			candSlug, candTitle, candKind = r.toPageSlug, r.toPageTitle, r.toPageKind
			candSummary, candImportance = r.toPageSummary, r.toPageImportance
		} else {
			candID, candNS = r.fromPageID, r.fromPageNamespace
			candSlug, candTitle, candKind = r.fromPageSlug, r.fromPageTitle, r.fromPageKind
			candSummary, candImportance = r.fromPageSummary, r.fromPageImportance
		}

		if candNS != input.Namespace {
			s.log.Error("wiki edge neighbor namespace mismatch",
				"pageNamespace", candNS, "requested", input.Namespace)
			continue
		}
		if _, exists := all[candID]; exists {
			continue
		}
		all[candID] = RetrievedPage{
			ID:         candID,
			Slug:       candSlug,
			Title:      candTitle,
			Kind:       candKind,
			Summary:    candSummary,
			Importance: candImportance,
			Reason:     "edge_neighbor",
			Hops:       1,
		}
	}
	edgeRows.Close()

	ranked := rankPages(all, limit)

	go Set(context.Background(), s.cache, input.Namespace, input.Text, ranked)
	return ranked, nil
}

// rankPages sorts pages by score descending and slices to limit.
func rankPages(pages map[string]RetrievedPage, limit int) []RetrievedPage {
	out := make([]RetrievedPage, 0, len(pages))
	for _, p := range pages {
		out = append(out, p)
	}
	// Insertion sort is fine for ≤ 200 items.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && pageScore(out[j]) > pageScore(out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	if limit < len(out) {
		out = out[:limit]
	}
	return out
}

func pageScore(p RetrievedPage) float64 {
	hopPenalty := 1.0 / (1.0 + float64(p.Hops))
	reasonBoost := 0.6
	if p.Reason == "alias_match" {
		reasonBoost = 1.0
	}
	return p.Importance * hopPenalty * reasonBoost
}

// normalizeSlug lower-cases and hyphenates spaces, mirroring the TS version.
var multiSpace = regexp.MustCompile(`\s+`)

func normalizeSlug(slug string) string {
	return multiSpace.ReplaceAllString(strings.TrimSpace(strings.ToLower(slug)), "-")
}

// tokenize splits text into Unicode letter/digit tokens of length >= 2,
// deduplicated and lowercased. Thai characters are preserved intact because
// unicode.IsLetter matches them.
func tokenize(text string) []string {
	lower := strings.ToLower(text)
	var current strings.Builder
	var tokens []string
	flush := func() {
		s := current.String()
		// Count runes, not bytes, so single Thai characters (3 bytes each) are
		// correctly rejected by the min-length filter.
		if len([]rune(s)) >= 2 {
			tokens = append(tokens, s)
		}
		current.Reset()
	}
	for _, r := range lower {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return uniqueLower(tokens)
}

// uniqueLower deduplicates and lowercases a string slice, preserving order.
func uniqueLower(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		n := strings.TrimSpace(strings.ToLower(v))
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

// clamp01 clamps v to [0, 1]. A zero input is returned as 0, not as 0.5 —
// the caller decides whether 0 means "unset".
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
