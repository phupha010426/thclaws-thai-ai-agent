package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// OutboxMetrics is an optional hook for recording relay stats. Pass nil to
// disable instrumentation without changing relay behaviour.
type OutboxMetrics interface {
	RecordPublished(subject string)
	RecordFailure(subject string)
	RecordSkipped(reason string)
}

type noopMetrics struct{}

func (noopMetrics) RecordPublished(string) {}
func (noopMetrics) RecordFailure(string)   {}
func (noopMetrics) RecordSkipped(string)   {}

// RelayPublisher is the subset of Publisher the relay needs. Defined as an
// interface so tests can inject a mock without a live NATS connection.
type RelayPublisher interface {
	Publish(ctx context.Context, subject string, payload any) error
}

// outboxRow mirrors the columns fetched from event_outbox.
type outboxRow struct {
	ID      int64
	Topic   string
	Payload []byte
}

// RelayStore abstracts the Postgres reads and writes the relay performs.
// Splitting it out keeps the relay logic pure and testable without a real DB.
type RelayStore interface {
	FetchPending(ctx context.Context, batchSize int) ([]outboxRow, error)
	MarkPublished(ctx context.Context, id int64)
	BumpAttempts(ctx context.Context, id int64, lastError string)
}

// pgStore is the production RelayStore backed by a pgxpool.Pool.
type pgStore struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

func (s *pgStore) FetchPending(ctx context.Context, batchSize int) ([]outboxRow, error) {
	const q = `
		SELECT id, topic, payload
		  FROM event_outbox
		 WHERE status = 'pending'
		 ORDER BY id ASC
		 LIMIT $1`
	dbRows, err := s.pool.Query(ctx, q, batchSize)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer dbRows.Close()

	var result []outboxRow
	for dbRows.Next() {
		var row outboxRow
		if err := dbRows.Scan(&row.ID, &row.Topic, &row.Payload); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		result = append(result, row)
	}
	return result, dbRows.Err()
}

func (s *pgStore) MarkPublished(ctx context.Context, id int64) {
	const q = `UPDATE event_outbox
	               SET status       = 'published',
	                   published_at = NOW(),
	                   last_error   = NULL
	             WHERE id = $1`
	if _, err := s.pool.Exec(ctx, q, id); err != nil {
		s.logger.ErrorContext(ctx, "outbox: markPublished failed", "outboxID", id, "err", err)
	}
}

func (s *pgStore) BumpAttempts(ctx context.Context, id int64, lastError string) {
	const q = `UPDATE event_outbox
	               SET attempts   = attempts + 1,
	                   last_error = $2
	             WHERE id = $1`
	if _, err := s.pool.Exec(ctx, q, id, lastError); err != nil {
		s.logger.ErrorContext(ctx, "outbox: bumpAttempts failed", "outboxID", id, "err", err)
	}
}

// Relay drains pending rows from event_outbox and publishes them to NATS
// JetStream. The outbox row must have been inserted in the same DB transaction
// as the state mutation it represents; the relay is the only writer that ever
// moves a row from pending → published.
type Relay struct {
	store   RelayStore
	pub     RelayPublisher
	logger  *slog.Logger
	metrics OutboxMetrics
}

// NewRelay returns a Relay backed by a real pgxpool. Pass nil for metrics to
// disable instrumentation.
func NewRelay(pool *pgxpool.Pool, pub RelayPublisher, logger *slog.Logger, metrics OutboxMetrics) *Relay {
	if metrics == nil {
		metrics = noopMetrics{}
	}
	return &Relay{
		store:   &pgStore{pool: pool, logger: logger},
		pub:     pub,
		logger:  logger,
		metrics: metrics,
	}
}

// newRelayWithStore constructs a Relay with an injected store — used in tests
// to avoid a live Postgres connection.
func newRelayWithStore(store RelayStore, pub RelayPublisher, logger *slog.Logger, metrics OutboxMetrics) *Relay {
	if metrics == nil {
		metrics = noopMetrics{}
	}
	return &Relay{store: store, pub: pub, logger: logger, metrics: metrics}
}

// DrainOnce fetches up to batchSize pending rows and publishes each one.
// Rows with a missing Namespace are bumped (attempts++) but not published so
// they surface in alerting without halting the rest of the batch.
// Returns the number of rows touched (published + skipped + failed).
func (r *Relay) DrainOnce(ctx context.Context, batchSize int) (int, error) {
	rows, err := r.store.FetchPending(ctx, batchSize)
	if err != nil {
		return 0, fmt.Errorf("outbox: fetch pending: %w", err)
	}
	for _, row := range rows {
		r.processRow(ctx, row)
	}
	return len(rows), nil
}

// processRow validates, builds the subject, publishes, then updates the DB row.
// All errors are absorbed so one bad row never stalls the rest of the batch.
func (r *Relay) processRow(ctx context.Context, row outboxRow) {
	var envelope map[string]any
	if err := json.Unmarshal(row.Payload, &envelope); err != nil {
		msg := fmt.Sprintf("payload not valid JSON: %v", err)
		r.logger.ErrorContext(ctx, "outbox relay: "+msg, "outboxID", row.ID)
		r.store.BumpAttempts(ctx, row.ID, msg)
		r.metrics.RecordSkipped("bad_json")
		return
	}

	// Namespace must be a non-empty string in the payload. Publishing without
	// it would fan the message to every wildcard consumer and violate the
	// per-LINE-user tenant boundary.
	namespace, _ := envelope["Namespace"].(string)
	if namespace == "" {
		msg := "payload missing Namespace field"
		r.logger.ErrorContext(ctx, "outbox relay: "+msg, "outboxID", row.ID, "topic", row.Topic)
		r.store.BumpAttempts(ctx, row.ID, msg)
		r.metrics.RecordSkipped("missing_namespace")
		return
	}

	subject := buildSubject(row.Topic, namespace)
	if err := r.pub.Publish(ctx, subject, envelope); err != nil {
		msg := err.Error()
		r.logger.ErrorContext(ctx, "outbox relay: nats publish failed",
			"outboxID", row.ID, "subject", subject, "err", msg)
		r.store.BumpAttempts(ctx, row.ID, msg)
		r.metrics.RecordFailure(subject)
		return
	}

	r.store.MarkPublished(ctx, row.ID)
	r.metrics.RecordPublished(subject)
}

// buildSubject appends the namespace to topic if not already present, ensuring
// the subject ends with the per-user tail that JetStream consumers filter on.
func buildSubject(topic, namespace string) string {
	if strings.HasSuffix(topic, "."+namespace) || topic == namespace {
		return topic
	}
	return topic + "." + namespace
}

// Run loops DrainOnce until ctx is cancelled. A full batch causes an immediate
// next iteration (backpressure); an empty or partial batch waits 1 s to avoid
// hammering Postgres at idle.
func (r *Relay) Run(ctx context.Context) {
	const (
		batchSize  = 200
		idleSleep  = time.Second
		errorSleep = 5 * time.Second
	)
	r.logger.InfoContext(ctx, "outbox relay started", "batchSize", batchSize)

	for {
		if ctx.Err() != nil {
			r.logger.InfoContext(ctx, "outbox relay stopping")
			return
		}

		n, err := r.DrainOnce(ctx, batchSize)
		if err != nil {
			r.logger.ErrorContext(ctx, "outbox drain error", "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(errorSleep):
			}
			continue
		}

		if n >= batchSize {
			// Batch was full — loop immediately to drain the backlog.
			continue
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(idleSleep):
		}
	}
}
