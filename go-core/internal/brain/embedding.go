package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// EmbedSourceType mirrors the TypeScript EmbedSourceType union.
type EmbedSourceType = string

const (
	SourceTypeMemoryItem EmbedSourceType = "memory_item"
	SourceTypeWikiPage   EmbedSourceType = "wiki_page"
	SourceTypeSummary    EmbedSourceType = "summary"
	SourceTypeMessage    EmbedSourceType = "message"
)

// Hit is one result row from EmbeddingService.Search.
type Hit struct {
	ID         string
	SourceType string
	SourceID   string
	Content    string
	Similarity float64 // 1 - cosine_distance
}

// EmbeddingService persists and queries pgvector embeddings.
// Every public method requires a non-empty namespace; the WHERE clause always
// filters by namespace BEFORE the ANN operator so the HNSW index never
// traverses another tenant's rows.
type EmbeddingService struct {
	pool           *pgxpool.Pool
	gateway        GatewayClient
	model          string
	fallbackModels []string
	log            *slog.Logger
}

// NewEmbeddingService wires up the service. model and fallbackModels come from
// config.EmbeddingModel / config.EmbeddingFallbackModels.
func NewEmbeddingService(pool *pgxpool.Pool, gw GatewayClient, model string, fallbacks []string, log *slog.Logger) *EmbeddingService {
	return &EmbeddingService{
		pool:           pool,
		gateway:        gw,
		model:          model,
		fallbackModels: fallbacks,
		log:            log,
	}
}

// Upsert embeds content (after PII redaction) and writes it to memory_embeddings.
// ON CONFLICT (namespace, sourceType, sourceId) updates the row in place.
func (s *EmbeddingService) Upsert(ctx context.Context, namespace, sourceType, sourceID, content string, metadata map[string]any) error {
	if namespace == "" {
		return fmt.Errorf("embedding: namespace required")
	}

	cleaned := RedactPii(content)

	vec, err := s.gateway.Embed(ctx, EmbedRequest{
		Model:          s.model,
		FallbackModels: s.fallbackModels,
		Text:           cleaned,
	})
	if err != nil {
		return fmt.Errorf("embedding: embed: %w", err)
	}

	literal := toVectorLiteral(vec)

	var metaJSON []byte
	if metadata != nil {
		metaJSON, err = json.Marshal(metadata)
		if err != nil {
			return fmt.Errorf("embedding: marshal metadata: %w", err)
		}
	}

	// Namespace is bound as $1 — never interpolated — so a malicious namespace
	// value cannot escape the parameter context.
	const q = `
INSERT INTO memory_embeddings (id, namespace, "sourceType", "sourceId", content, embedding, metadata, "createdAt", "updatedAt")
VALUES (gen_random_uuid()::text, $1, $2, $3, $4, $5::vector, $6::jsonb, NOW(), NOW())
ON CONFLICT (namespace, "sourceType", "sourceId")
DO UPDATE SET content = EXCLUDED.content,
              embedding = EXCLUDED.embedding,
              metadata = EXCLUDED.metadata,
              "updatedAt" = NOW()`

	_, err = s.pool.Exec(ctx, q, namespace, sourceType, sourceID, cleaned, literal, nullableJSON(metaJSON))
	if err != nil {
		return fmt.Errorf("embedding: upsert: %w", err)
	}
	s.log.Info("memory embedding upserted",
		"namespace", namespace, "sourceType", sourceType, "sourceId", sourceID)
	return nil
}

// Search returns at most limit embeddings closest to query, filtered to the
// given namespace and (optionally) source types. Only hits with
// similarity >= minScore are returned.
func (s *EmbeddingService) Search(ctx context.Context, namespace, query string, limit int, sourceTypes []string, minScore float64) ([]Hit, error) {
	if namespace == "" {
		return nil, fmt.Errorf("embedding: namespace required")
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 50 {
		limit = 50
	}

	vec, err := s.gateway.Embed(ctx, EmbedRequest{
		Model:          s.model,
		FallbackModels: s.fallbackModels,
		Text:           query,
	})
	if err != nil {
		return nil, fmt.Errorf("embedding: embed query: %w", err)
	}
	literal := toVectorLiteral(vec)

	// sourceType filter is constructed from a bind parameter list so no
	// interpolation of caller-supplied strings ever touches the SQL text.
	var rows []struct {
		id         string
		sourceType string
		sourceID   string
		content    string
		distance   float64
	}

	if len(sourceTypes) == 0 {
		const q = `
SELECT id, "sourceType", "sourceId", content, (embedding <=> $2::vector) AS distance
FROM memory_embeddings
WHERE namespace = $1
ORDER BY embedding <=> $2::vector
LIMIT $3`
		pgRows, err := s.pool.Query(ctx, q, namespace, literal, limit)
		if err != nil {
			return nil, fmt.Errorf("embedding: search: %w", err)
		}
		defer pgRows.Close()
		for pgRows.Next() {
			var r struct {
				id         string
				sourceType string
				sourceID   string
				content    string
				distance   float64
			}
			if err := pgRows.Scan(&r.id, &r.sourceType, &r.sourceID, &r.content, &r.distance); err != nil {
				return nil, fmt.Errorf("embedding: scan: %w", err)
			}
			rows = append(rows, r)
		}
	} else {
		const q = `
SELECT id, "sourceType", "sourceId", content, (embedding <=> $2::vector) AS distance
FROM memory_embeddings
WHERE namespace = $1 AND "sourceType" = ANY($3::text[])
ORDER BY embedding <=> $2::vector
LIMIT $4`
		pgRows, err := s.pool.Query(ctx, q, namespace, literal, sourceTypes, limit)
		if err != nil {
			return nil, fmt.Errorf("embedding: search: %w", err)
		}
		defer pgRows.Close()
		for pgRows.Next() {
			var r struct {
				id         string
				sourceType string
				sourceID   string
				content    string
				distance   float64
			}
			if err := pgRows.Scan(&r.id, &r.sourceType, &r.sourceID, &r.content, &r.distance); err != nil {
				return nil, fmt.Errorf("embedding: scan: %w", err)
			}
			rows = append(rows, r)
		}
	}

	hits := make([]Hit, 0, len(rows))
	for _, r := range rows {
		sim := 1.0 - r.distance
		if sim < minScore {
			continue
		}
		hits = append(hits, Hit{
			ID:         r.id,
			SourceType: r.sourceType,
			SourceID:   r.sourceID,
			Content:    r.content,
			Similarity: sim,
		})
	}
	return hits, nil
}

// toVectorLiteral serialises a float64 slice to pgvector's text literal format
// "[v1,v2,...]". Non-finite values are clamped to 0 so a bad embedding cannot
// corrupt the index.
func toVectorLiteral(vec []float64) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, v := range vec {
		if i > 0 {
			b.WriteByte(',')
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			v = 0
		}
		b.WriteString(strconv.FormatFloat(v, 'f', -1, 64))
	}
	b.WriteByte(']')
	return b.String()
}

// nullableJSON returns nil when b is empty so pgx sends a SQL NULL instead of
// an empty byte slice, which pgx would reject for a jsonb column.
func nullableJSON(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}
