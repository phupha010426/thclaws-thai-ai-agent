// Package pdpa implements the "right to be forgotten" and data export
// requirements under Thailand's PDPA. Every delete operation is idempotent —
// running it twice on an already-empty namespace returns zeros, not an error.
// The caller is responsible for verifying identity/consent; this package
// only executes the deletion once it receives a valid namespace.
package pdpa

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// DeleteReport describes what was removed across every store.
type DeleteReport struct {
	Namespace string                 `json:"namespace"`
	Postgres  map[string]int64       `json:"postgres"`
	Mongo     map[string]int64       `json:"mongo"`
	Redis     int64                  `json:"redis"`
}

// ExportData is the full namespace snapshot returned for data portability.
type ExportData struct {
	Namespace  string         `json:"namespace"`
	ExportedAt string         `json:"exportedAt"`
	Postgres   map[string]any `json:"postgres"`
	Mongo      map[string]any `json:"mongo"`
}

// Service executes PDPA operations across Postgres, MongoDB, and Redis.
type Service struct {
	pool   *pgxpool.Pool
	mongo  *mongo.Client
	dbName string
	redis  redis.UniversalClient
	logger *slog.Logger
}

// New wires the PDPA service. All three stores are required — any nil will
// cause a panic at call time rather than silently skipping a store.
func New(pool *pgxpool.Pool, mongoClient *mongo.Client, dbName string, redisClient redis.UniversalClient, logger *slog.Logger) *Service {
	return &Service{
		pool:   pool,
		mongo:  mongoClient,
		dbName: dbName,
		redis:  redisClient,
		logger: logger,
	}
}

func (s *Service) col(name string) *mongo.Collection {
	return s.mongo.Database(s.dbName).Collection(name)
}

// DeleteAll removes every record belonging to namespace across all stores.
// Postgres and Mongo deletes run in parallel to minimise wall-clock time.
func (s *Service) DeleteAll(ctx context.Context, namespace string) (DeleteReport, error) {
	if namespace == "" {
		return DeleteReport{}, fmt.Errorf("pdpa: DeleteAll: namespace required")
	}
	s.logger.Warn("pdpa delete starting", "namespace", namespace)

	report := DeleteReport{
		Namespace: namespace,
		Postgres:  make(map[string]int64),
		Mongo:     make(map[string]int64),
	}

	var pgMu, mongoMu sync.Mutex
	var wg sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex

	setErr := func(err error) {
		errMu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		errMu.Unlock()
	}

	// Postgres deletes -------------------------------------------------------
	pgTables := []struct {
		key   string
		query string
		byPayload bool
	}{
		{"wikiEdges", `DELETE FROM wiki_edges WHERE namespace = $1`, false},
		{"wikiAliases", `DELETE FROM wiki_aliases WHERE namespace = $1`, false},
		{"wikiPages", `DELETE FROM wiki_pages WHERE namespace = $1`, false},
		{"memoryEmbeddings", `DELETE FROM memory_embeddings WHERE namespace = $1`, false},
		{"ledgerEvents", `DELETE FROM ledger_events WHERE namespace = $1`, false},
		// event_outbox has no direct namespace column; filter via JSONB
		{"eventOutbox", `DELETE FROM event_outbox WHERE payload->>'namespace' = $1`, false},
	}

	for _, t := range pgTables {
		t := t
		wg.Add(1)
		go func() {
			defer wg.Done()
			tag, err := s.pool.Exec(ctx, t.query, namespace)
			if err != nil {
				setErr(fmt.Errorf("pdpa: DeleteAll: postgres %s: %w", t.key, err))
				return
			}
			pgMu.Lock()
			report.Postgres[t.key] = tag.RowsAffected()
			pgMu.Unlock()
		}()
	}

	// Mongo deletes ----------------------------------------------------------
	mongoCollections := []struct {
		key  string
		name string
	}{
		{"conversationMessages", "conversation_messages"},
		{"conversationSnapshots", "conversation_snapshots"},
		{"eventLogs", "event_logs"},
		{"memorySummaries", "memory_summaries"},
		{"memoryItems", "memory_items"},
		{"personalWikiPages", "personal_wiki_pages"},
		{"agentProfiles", "agent_profiles"},
		{"slipRecords", "slip_records"},
	}

	for _, c := range mongoCollections {
		c := c
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := s.col(c.name).DeleteMany(ctx, bson.M{"namespace": namespace})
			if err != nil {
				setErr(fmt.Errorf("pdpa: DeleteAll: mongo %s: %w", c.key, err))
				return
			}
			mongoMu.Lock()
			report.Mongo[c.key] = res.DeletedCount
			mongoMu.Unlock()
		}()
	}

	wg.Wait()
	if firstErr != nil {
		return DeleteReport{}, firstErr
	}

	// Redis SCAN + DEL — sequential because cursor-based iteration cannot be
	// parallelised against a single-shard Redis without coordination overhead.
	redisDeleted, err := s.dropRedisNamespace(ctx, namespace)
	if err != nil {
		// Redis failure is logged but not fatal — the relational/document data
		// has already been deleted; a stale cache entry is self-healing at TTL.
		s.logger.Warn("redis pdpa scan partial failure", "namespace", namespace, "err", err)
	}
	report.Redis = redisDeleted

	s.logger.Warn("pdpa delete completed", "namespace", namespace, "report", report)
	return report, nil
}

// ExportAll collects all records for namespace in parallel and returns a
// structured snapshot suitable for PDPA data portability responses.
func (s *Service) ExportAll(ctx context.Context, namespace string) (ExportData, error) {
	if namespace == "" {
		return ExportData{}, fmt.Errorf("pdpa: ExportAll: namespace required")
	}

	type result struct {
		key  string
		rows any
		err  error
	}

	// Postgres queries
	pgQueries := []struct {
		key string
		sql string
	}{
		{"wikiPages", `SELECT * FROM wiki_pages WHERE namespace = $1 ORDER BY "updatedAt" DESC`},
		{"wikiEdges", `SELECT * FROM wiki_edges WHERE namespace = $1 ORDER BY id`},
		{"memoryEmbeddings", `SELECT id, namespace, "sourceType", "sourceId", content, metadata, "createdAt" FROM memory_embeddings WHERE namespace = $1`},
		{"ledgerEvents", `SELECT * FROM ledger_events WHERE namespace = $1 ORDER BY id ASC`},
	}

	pgResults := make(map[string]any, len(pgQueries))
	var pgMu sync.Mutex
	var wg sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex
	setErr := func(err error) {
		errMu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		errMu.Unlock()
	}

	for _, q := range pgQueries {
		q := q
		wg.Add(1)
		go func() {
			defer wg.Done()
			rows, err := s.pool.Query(ctx, q.sql, namespace)
			if err != nil {
				setErr(fmt.Errorf("pdpa: ExportAll: postgres %s: %w", q.key, err))
				return
			}
			defer rows.Close()
			// Collect as generic maps to avoid tying the export to a strict model
			var collected []map[string]any
			fds := rows.FieldDescriptions()
			for rows.Next() {
				vals, err := rows.Values()
				if err != nil {
					setErr(fmt.Errorf("pdpa: ExportAll: postgres %s scan: %w", q.key, err))
					return
				}
				row := make(map[string]any, len(fds))
				for i, fd := range fds {
					row[string(fd.Name)] = vals[i]
				}
				collected = append(collected, row)
			}
			if err := rows.Err(); err != nil {
				setErr(fmt.Errorf("pdpa: ExportAll: postgres %s rows: %w", q.key, err))
				return
			}
			pgMu.Lock()
			pgResults[q.key] = collected
			pgMu.Unlock()
		}()
	}

	// Mongo queries
	mongoQueries := []struct {
		key  string
		name string
	}{
		{"conversations", "conversation_messages"},
		{"summaries", "memory_summaries"},
		{"memoryItems", "memory_items"},
		{"wikiPages", "personal_wiki_pages"},
	}

	mongoResults := make(map[string]any, len(mongoQueries))
	var mongoMu sync.Mutex

	for _, mq := range mongoQueries {
		mq := mq
		wg.Add(1)
		go func() {
			defer wg.Done()
			cur, err := s.col(mq.name).Find(ctx, bson.M{"namespace": namespace})
			if err != nil {
				setErr(fmt.Errorf("pdpa: ExportAll: mongo %s: %w", mq.key, err))
				return
			}
			defer cur.Close(ctx)
			var docs []bson.M
			if err := cur.All(ctx, &docs); err != nil {
				setErr(fmt.Errorf("pdpa: ExportAll: mongo %s decode: %w", mq.key, err))
				return
			}
			mongoMu.Lock()
			mongoResults[mq.key] = docs
			mongoMu.Unlock()
		}()
	}

	wg.Wait()
	if firstErr != nil {
		return ExportData{}, firstErr
	}

	return ExportData{
		Namespace: namespace,
		Postgres:  pgResults,
		Mongo:     mongoResults,
	}, nil
}

// dropRedisNamespace scans for all keys matching *<namespace>* and deletes
// them in chunks of 200 to avoid blocking the Redis event loop with a single
// large DEL command.
func (s *Service) dropRedisNamespace(ctx context.Context, namespace string) (int64, error) {
	pattern := "*" + namespace + "*"
	var deleted int64
	var cursor uint64
	for {
		var keys []string
		var err error
		keys, cursor, err = s.redis.Scan(ctx, cursor, pattern, 200).Result()
		if err != nil {
			return deleted, fmt.Errorf("pdpa: redis scan: %w", err)
		}
		if len(keys) > 0 {
			n, err := s.redis.Del(ctx, keys...).Result()
			if err != nil {
				return deleted, fmt.Errorf("pdpa: redis del: %w", err)
			}
			deleted += n
		}
		if cursor == 0 {
			break
		}
	}
	return deleted, nil
}
