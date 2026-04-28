package brain

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	minTextLength  = 8
	dedupTTL       = 300 * time.Second
)

// extractionSystemPrompt is the Thai-language extractor prompt ported verbatim
// from extraction.service.ts. It is constant so the LLM always sees the same
// instruction regardless of runtime state.
const extractionSystemPrompt = `You build a personal Thai-language knowledge graph for a single user.
Read the user message and emit JSON only. Do not invent facts.
Schema: {"pages":[{"slug","title","kind","summary","aliases","importance"}],"edges":[{"fromSlug","toSlug","relation","weight"}]}.
kind enum: person | place | item | event | concept | preference | rule.
slug must be a kebab-case identifier in Thai or English (no spaces).
Use importance 0.9 when the user explicitly says to remember it; 0.3 otherwise.
Skip the message entirely (return empty arrays) if there is no durable fact.`

// extractedPage is the JSON schema for one page returned by the LLM.
type extractedPage struct {
	Slug       string   `json:"slug"`
	Title      string   `json:"title"`
	Kind       string   `json:"kind"`
	Summary    string   `json:"summary,omitempty"`
	Aliases    []string `json:"aliases,omitempty"`
	Importance float64  `json:"importance,omitempty"`
}

// extractedEdge is the JSON schema for one edge returned by the LLM.
type extractedEdge struct {
	FromSlug string  `json:"fromSlug"`
	ToSlug   string  `json:"toSlug"`
	Relation string  `json:"relation"`
	Weight   float64 `json:"weight,omitempty"`
}

// extractionResult is the top-level JSON object emitted by the LLM.
type extractionResult struct {
	Pages []extractedPage `json:"pages"`
	Edges []extractedEdge `json:"edges"`
}

// ExtractInput carries the fields for ExtractionService.ExtractAndStore.
type ExtractInput struct {
	Namespace   string
	OwnerID     string
	AgentID     string
	Text        string
	CausationID string
}

// ExtractionService calls the LLM to extract entities from conversation text
// and persists them as wiki pages, aliases, edges, and embeddings.
// This is designed to run from a background queue worker, not on the hot
// LINE reply path, because the LLM round trip is the dominant latency.
type ExtractionService struct {
	gateway        GatewayClient
	rdb            *redis.Client
	wiki           *WikiBrainService
	embedding      *EmbeddingService
	defaultModel   string
	fallbackModels []string
	log            *slog.Logger
}

// NewExtractionService wires the extraction pipeline.
func NewExtractionService(
	gw GatewayClient,
	rdb *redis.Client,
	wiki *WikiBrainService,
	emb *EmbeddingService,
	defaultModel string,
	fallbacks []string,
	log *slog.Logger,
) *ExtractionService {
	return &ExtractionService{
		gateway:        gw,
		rdb:            rdb,
		wiki:           wiki,
		embedding:      emb,
		defaultModel:   defaultModel,
		fallbackModels: fallbacks,
		log:            log,
	}
}

// ExtractAndStore extracts entities from input.Text and persists them to the
// knowledge graph. Returns counts of pages and edges written.
//
// It is idempotent within a 300-second dedup window: calling it twice with the
// same (namespace, text) returns (0, 0) on the second call.
func (s *ExtractionService) ExtractAndStore(ctx context.Context, input ExtractInput) (pages, edges int, err error) {
	if input.Namespace == "" {
		return 0, 0, fmt.Errorf("extraction: namespace required")
	}
	trimmed := strings.TrimSpace(input.Text)
	if len(trimmed) < minTextLength {
		return 0, 0, nil
	}

	// Dedup: SETNX a SHA1-keyed flag so the same text is never extracted twice
	// within DEDUP_TTL. Redis failure is non-fatal — we log and continue.
	dedupKey := buildDedupKey(input.Namespace, trimmed)
	set, dedupErr := s.rdb.SetNX(ctx, dedupKey, "1", dedupTTL).Result()
	if dedupErr != nil {
		s.log.Warn("extraction dedup failed (continuing)", "err", dedupErr)
	} else if !set {
		s.log.Debug("extraction skipped (dedup)", "namespace", input.Namespace)
		return 0, 0, nil
	}

	sanitized := RedactPii(trimmed)

	raw, err := s.gateway.Chat(ctx, ChatRequest{
		Namespace:      input.Namespace,
		Model:          s.defaultModel,
		FallbackModels: s.fallbackModels,
		MaxTokens:      1500,
		Messages: []ChatMessage{
			{Role: "system", Content: extractionSystemPrompt},
			{Role: "user", Content: sanitized},
		},
	})
	if err != nil {
		return 0, 0, fmt.Errorf("extraction: chat: %w", err)
	}

	parsed, ok := parseJSONLoosely(raw)
	if !ok {
		s.log.Warn("extraction: model returned non-json",
			"namespace", input.Namespace, "preview", truncate(raw, 200))
		return 0, 0, nil
	}

	result, valErr := validateExtractionResult(parsed)
	if valErr != nil {
		s.log.Warn("extraction: schema mismatch",
			"namespace", input.Namespace, "err", valErr)
		return 0, 0, nil
	}

	slugToID := make(map[string]string, len(result.Pages))
	for _, p := range result.Pages {
		persisted, upsertErr := s.wiki.UpsertPage(ctx, UpsertPageInput{
			Namespace:  input.Namespace,
			Slug:       p.Slug,
			Title:      p.Title,
			Kind:       p.Kind,
			Summary:    p.Summary,
			Aliases:    p.Aliases,
			Importance: clamp01(p.Importance),
		})
		if upsertErr != nil {
			s.log.Warn("extraction: upsert page failed",
				"namespace", input.Namespace, "slug", p.Slug, "err", upsertErr)
			continue
		}
		slugToID[p.Slug] = persisted.ID

		// Embed in the background — a slow or failing embedding API must not
		// stall the extraction pipeline or the queue worker.
		go func(pageID, title, summary string, aliases []string) {
			content := buildEmbedContent(title, summary, aliases)
			if embedErr := s.embedding.Upsert(
				context.Background(),
				input.Namespace, SourceTypeWikiPage, pageID, content, nil,
			); embedErr != nil {
				s.log.Warn("embedding upsert failed (non-fatal)", "err", embedErr)
			}
		}(persisted.ID, p.Title, p.Summary, p.Aliases)

		pages++
	}

	for _, e := range result.Edges {
		fromID, fromOK := slugToID[e.FromSlug]
		toID, toOK := slugToID[e.ToSlug]
		if !fromOK || !toOK {
			continue
		}
		w := e.Weight
		if w == 0 {
			w = 1.0
		}
		if _, linkErr := s.wiki.LinkPages(ctx, LinkPagesInput{
			Namespace:  input.Namespace,
			FromPageID: fromID,
			ToPageID:   toID,
			Relation:   e.Relation,
			Weight:     clamp01(w),
		}); linkErr != nil {
			s.log.Warn("extraction: edge link failed",
				"namespace", input.Namespace, "err", linkErr)
			continue
		}
		edges++
	}

	s.log.Info("extraction stored",
		"namespace", input.Namespace,
		"pages", pages,
		"edges", edges,
		"causationId", input.CausationID)
	return pages, edges, nil
}

// parseJSONLoosely tries a straight parse first, then strips markdown fences
// and extracts the first {...} block — models often wrap JSON in triple backticks.
func parseJSONLoosely(content string) (map[string]json.RawMessage, bool) {
	trimmed := strings.TrimSpace(content)
	// Strip ```json ... ``` or ``` ... ``` fences.
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	trimmed = strings.TrimSpace(trimmed)

	var result map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &result); err == nil {
		return result, true
	}

	// Fall back to first-brace extraction.
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start < 0 || end <= start {
		return nil, false
	}
	if err := json.Unmarshal([]byte(trimmed[start:end+1]), &result); err != nil {
		return nil, false
	}
	return result, true
}

// validateExtractionResult unmarshals and validates the raw JSON map into an
// extractionResult. Manual validation mirrors the TypeScript Zod schema without
// the Zod dependency.
func validateExtractionResult(raw map[string]json.RawMessage) (extractionResult, error) {
	var result extractionResult

	if pagesRaw, ok := raw["pages"]; ok {
		if err := json.Unmarshal(pagesRaw, &result.Pages); err != nil {
			return extractionResult{}, fmt.Errorf("pages: %w", err)
		}
	}
	if edgesRaw, ok := raw["edges"]; ok {
		if err := json.Unmarshal(edgesRaw, &result.Edges); err != nil {
			return extractionResult{}, fmt.Errorf("edges: %w", err)
		}
	}

	validKinds := map[string]bool{
		"person": true, "place": true, "item": true,
		"event": true, "concept": true, "preference": true, "rule": true,
	}
	for i, p := range result.Pages {
		if p.Slug == "" {
			return extractionResult{}, fmt.Errorf("pages[%d].slug empty", i)
		}
		if p.Title == "" {
			return extractionResult{}, fmt.Errorf("pages[%d].title empty", i)
		}
		if p.Kind == "" {
			result.Pages[i].Kind = "concept"
		} else if !validKinds[p.Kind] {
			result.Pages[i].Kind = "concept"
		}
		if p.Importance < 0 || p.Importance > 1 {
			result.Pages[i].Importance = clamp01(p.Importance)
		}
	}
	for i, e := range result.Edges {
		if e.FromSlug == "" {
			return extractionResult{}, fmt.Errorf("edges[%d].fromSlug empty", i)
		}
		if e.ToSlug == "" {
			return extractionResult{}, fmt.Errorf("edges[%d].toSlug empty", i)
		}
		if e.Relation == "" {
			return extractionResult{}, fmt.Errorf("edges[%d].relation empty", i)
		}
	}
	return result, nil
}

// buildDedupKey produces a short Redis key from (namespace, text) using SHA1
// truncated to reduce key size without meaningful collision risk for this TTL.
func buildDedupKey(namespace, text string) string {
	nsHash := sha1.Sum([]byte(namespace))
	nsHex := hex.EncodeToString(nsHash[:])[:8]
	txtHash := sha1.Sum([]byte(strings.ToLower(text)))
	txtHex := hex.EncodeToString(txtHash[:])[:16]
	return fmt.Sprintf("brain:extract:%s:%s", nsHex, txtHex)
}

// buildEmbedContent joins title, summary, and aliases with newlines, filtering
// empty strings, to form the text that gets embedded for a wiki page.
func buildEmbedContent(title, summary string, aliases []string) string {
	parts := []string{title}
	if summary != "" {
		parts = append(parts, summary)
	}
	if len(aliases) > 0 {
		parts = append(parts, strings.Join(aliases, ", "))
	}
	return strings.Join(parts, "\n")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
