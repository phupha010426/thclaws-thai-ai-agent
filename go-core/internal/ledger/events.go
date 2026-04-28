// Package ledger implements append-only event sourcing over Postgres. The
// invariant is strict: a LedgerEvent and its optional EventOutbox row must
// either both be committed or neither — split-brain between the audit log and
// the outbox relay would allow messages to be published without a matching
// ledger entry, or vice versa.
package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LedgerEventType enumerates the domain events the ledger accepts. Using a
// named type prevents callers from passing arbitrary strings that would make
// replay logic ambiguous.
type LedgerEventType string

const (
	EventTxRecorded   LedgerEventType = "tx.recorded"
	EventTxReversed   LedgerEventType = "tx.reversed"
	EventBudgetSet    LedgerEventType = "budget.set"
	EventGoalAdvanced LedgerEventType = "goal.advanced"
	EventSlipAttached LedgerEventType = "slip.attached"
)

// CacheInvalidator is satisfied by both the gateway cache and the brain cache.
// Defined here (consumer side) to avoid an import from the cache package.
type CacheInvalidator interface {
	InvalidateNamespace(ctx context.Context, namespace string) error
}

// LedgerEventInput holds everything needed to append one event. OutboxTopic is
// optional: when empty the event is only recorded in ledger_events and no
// outbox row is written, which is appropriate when fanout is not yet needed.
type LedgerEventInput struct {
	Namespace     string
	UserID        string
	Type          LedgerEventType
	Payload       json.RawMessage
	CorrelationID string
	CausationID   string
	// OutboxTopic is the NATS subject. Leave empty to skip outbox insertion.
	OutboxTopic string
}

// LedgerEvent is the read model returned by Replay.
type LedgerEvent struct {
	ID            int64
	Namespace     string
	UserID        string
	Type          LedgerEventType
	Payload       json.RawMessage
	CorrelationID string
	CausationID   string
	OccurredAt    time.Time
}

// Service appends and replays ledger events.
type Service struct {
	pool         *pgxpool.Pool
	gatewayCache CacheInvalidator
	brainCache   CacheInvalidator
	logger       *slog.Logger
}

// New wires up the LedgerEventService. Both cache invalidators are required —
// pass a no-op implementation if a cache is not deployed in a given environment.
func New(pool *pgxpool.Pool, gatewayCache, brainCache CacheInvalidator, logger *slog.Logger) *Service {
	return &Service{
		pool:         pool,
		gatewayCache: gatewayCache,
		brainCache:   brainCache,
		logger:       logger,
	}
}

// Append writes a LedgerEvent and, when OutboxTopic is set, an EventOutbox row
// inside a single pgx transaction. The outbox payload embeds the event metadata
// so the relay can publish without an extra SELECT. Cache invalidation fires
// after commit and is not retried — a stale cache for one TTL is acceptable,
// but blocking the write on cache errors is not.
func (s *Service) Append(ctx context.Context, input LedgerEventInput) (int64, error) {
	if input.Namespace == "" {
		return 0, fmt.Errorf("ledger: Append: namespace required")
	}

	var eventID int64
	var occurredAt time.Time

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("ledger: Append: begin tx: %w", err)
	}
	// Always rollback if we return early — pgx ignores rollback after commit.
	defer func() { _ = tx.Rollback(ctx) }()

	const insertEvent = `
		INSERT INTO ledger_events (namespace, "userId", type, payload, "correlationId", "causationId")
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, "occurredAt"`

	correlationID := nullableString(input.CorrelationID)
	causationID := nullableString(input.CausationID)

	err = tx.QueryRow(ctx, insertEvent,
		input.Namespace,
		input.UserID,
		string(input.Type),
		[]byte(input.Payload),
		correlationID,
		causationID,
	).Scan(&eventID, &occurredAt)
	if err != nil {
		return 0, fmt.Errorf("ledger: Append: insert event: %w", err)
	}

	if input.OutboxTopic != "" {
		outboxPayload, err := json.Marshal(map[string]any{
			"eventId":       fmt.Sprintf("%d", eventID),
			"namespace":     input.Namespace,
			"userId":        input.UserID,
			"type":          string(input.Type),
			"correlationId": input.CorrelationID,
			"causationId":   input.CausationID,
			"occurredAt":    occurredAt.UTC().Format(time.RFC3339Nano),
			"data":          json.RawMessage(input.Payload),
		})
		if err != nil {
			return 0, fmt.Errorf("ledger: Append: marshal outbox payload: %w", err)
		}

		const insertOutbox = `
			INSERT INTO event_outbox (topic, payload)
			VALUES ($1, $2)`
		if _, err = tx.Exec(ctx, insertOutbox, input.OutboxTopic, outboxPayload); err != nil {
			return 0, fmt.Errorf("ledger: Append: insert outbox: %w", err)
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("ledger: Append: commit: %w", err)
	}

	s.logger.Info("ledger event appended",
		"eventId", eventID,
		"type", input.Type,
		"namespace", input.Namespace,
		"outboxTopic", input.OutboxTopic,
	)

	// Fire-and-forget cache invalidation. We use a background context so that
	// a short request deadline does not abort the invalidation after the write
	// has already succeeded.
	go func() {
		bgCtx := context.Background()
		if err := s.gatewayCache.InvalidateNamespace(bgCtx, input.Namespace); err != nil {
			s.logger.Warn("gateway cache invalidation failed", "namespace", input.Namespace, "err", err)
		}
		if err := s.brainCache.InvalidateNamespace(bgCtx, input.Namespace); err != nil {
			s.logger.Warn("brain cache invalidation failed", "namespace", input.Namespace, "err", err)
		}
	}()

	return eventID, nil
}

// Replay fetches events for a namespace ordered by id starting from (and
// exclusive of) fromID. Used by projection builders to reconstruct derived
// state without touching the mutable transactions table.
func (s *Service) Replay(ctx context.Context, namespace string, fromID int64, batchSize int) ([]LedgerEvent, error) {
	if namespace == "" {
		return nil, fmt.Errorf("ledger: Replay: namespace required")
	}
	if batchSize <= 0 {
		batchSize = 500
	}

	const q = `
		SELECT id, namespace, "userId", type, payload, "correlationId", "causationId", "occurredAt"
		FROM ledger_events
		WHERE namespace = $1 AND id > $2
		ORDER BY id ASC
		LIMIT $3`

	rows, err := s.pool.Query(ctx, q, namespace, fromID, batchSize)
	if err != nil {
		return nil, fmt.Errorf("ledger: Replay: query: %w", err)
	}
	defer rows.Close()

	var events []LedgerEvent
	for rows.Next() {
		var e LedgerEvent
		var rawPayload []byte
		var corrID, causeID *string
		if err := rows.Scan(&e.ID, &e.Namespace, &e.UserID, &e.Type,
			&rawPayload, &corrID, &causeID, &e.OccurredAt); err != nil {
			return nil, fmt.Errorf("ledger: Replay: scan: %w", err)
		}
		e.Payload = json.RawMessage(rawPayload)
		if corrID != nil {
			e.CorrelationID = *corrID
		}
		if causeID != nil {
			e.CausationID = *causeID
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ledger: Replay: rows: %w", err)
	}
	return events, nil
}

// nullableString returns nil when s is empty so pgx stores NULL rather than an
// empty string, which would break IS NULL checks in downstream queries.
func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
