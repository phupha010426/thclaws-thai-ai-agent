package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/hibiken/asynq"
)

// ─── Dependency interfaces ────────────────────────────────────────────────────
// Defined here so other packages can implement them without creating an import
// cycle. The queue package depends on nothing in internal/brain or
// internal/memory; only concrete wiring in main.go bridges the gap.

// LineEventDispatcher processes a single raw LINE event payload. The
// implementation lives in internal/line.
type LineEventDispatcher interface {
	Dispatch(ctx context.Context, eventJSON json.RawMessage) error
}

// MemoryCompactor triggers a conversation compaction cycle for one agent
// scoped to a namespace. Implemented in internal/memory.
type MemoryCompactor interface {
	CompactConversation(ctx context.Context, ownerID, agentID, namespace string) error
}

// ExtractInput carries the information the Extractor needs to parse and store
// facts. CausationID is optional; when set it lets the extractor skip duplicate
// runs caused by event-replay.
type ExtractInput struct {
	OwnerID     string
	AgentID     string
	Namespace   string
	Text        string
	CausationID string
}

// Extractor parses free-text and writes knowledge-graph nodes under the given
// namespace. Implemented in internal/brain.
type Extractor interface {
	ExtractAndStore(ctx context.Context, input ExtractInput) error
}

// Reflector runs the nightly reflection cycle. When namespace is empty the
// implementation must discover and iterate all active namespaces; it must
// never aggregate data across namespaces.
type Reflector interface {
	ReflectNamespace(ctx context.Context, namespace string) error
	ReflectAllActive(ctx context.Context) error
}

// ─── Handler constructors ─────────────────────────────────────────────────────

// HandleLineWebhook returns an asynq handler function that unmarshals the
// payload and forwards the raw LINE event to the dispatcher.
func HandleLineWebhook(d LineEventDispatcher, logger *slog.Logger) func(context.Context, *asynq.Task) error {
	return func(ctx context.Context, t *asynq.Task) error {
		var job LineWebhookJob
		if err := json.Unmarshal(t.Payload(), &job); err != nil {
			// Unparseable payload cannot be retried meaningfully; return nil so
			// Asynq marks the task done and moves it to the archive.
			logger.ErrorContext(ctx, "line webhook: bad payload", "err", err)
			return nil
		}
		ctx = WithReceivedAt(ctx, job.ReceivedAt)
		if err := d.Dispatch(ctx, job.Event); err != nil {
			return fmt.Errorf("line webhook dispatch: %w", err)
		}
		return nil
	}
}

// HandleMemoryCompact returns a handler that drives one conversation compaction
// cycle. All three identity fields are required; a missing namespace would
// silently compact the wrong tenant's data.
func HandleMemoryCompact(c MemoryCompactor, logger *slog.Logger) func(context.Context, *asynq.Task) error {
	return func(ctx context.Context, t *asynq.Task) error {
		var job MemoryCompactJob
		if err := json.Unmarshal(t.Payload(), &job); err != nil {
			logger.ErrorContext(ctx, "memory compact: bad payload", "err", err)
			return nil
		}
		if job.Namespace == "" {
			logger.ErrorContext(ctx, "memory compact: missing namespace; skipping to avoid cross-tenant write")
			return nil
		}
		if err := c.CompactConversation(ctx, job.OwnerID, job.AgentID, job.Namespace); err != nil {
			return fmt.Errorf("memory compact (ns=%s): %w", job.Namespace, err)
		}
		return nil
	}
}

// HandleBrainExtract returns a handler that runs the knowledge-graph extractor.
// Namespace is mandatory; without it the extractor cannot partition its writes.
func HandleBrainExtract(e Extractor, logger *slog.Logger) func(context.Context, *asynq.Task) error {
	return func(ctx context.Context, t *asynq.Task) error {
		var job BrainExtractJob
		if err := json.Unmarshal(t.Payload(), &job); err != nil {
			logger.ErrorContext(ctx, "brain extract: bad payload", "err", err)
			return nil
		}
		if job.Namespace == "" {
			logger.ErrorContext(ctx, "brain extract: missing namespace; skipping to avoid cross-tenant write")
			return nil
		}
		input := ExtractInput{
			OwnerID:     job.OwnerID,
			AgentID:     job.AgentID,
			Namespace:   job.Namespace,
			Text:        job.Text,
			CausationID: job.CausationID,
		}
		if err := e.ExtractAndStore(ctx, input); err != nil {
			return fmt.Errorf("brain extract (ns=%s): %w", job.Namespace, err)
		}
		return nil
	}
}

// HandleDailyReflection returns a handler for the nightly cron job. When
// Namespace is empty the reflector discovers active namespaces itself so a
// single cron trigger covers the entire user base without the scheduler knowing
// which namespaces exist.
func HandleDailyReflection(r Reflector, logger *slog.Logger) func(context.Context, *asynq.Task) error {
	return func(ctx context.Context, t *asynq.Task) error {
		var job DailyReflectionJob
		if err := json.Unmarshal(t.Payload(), &job); err != nil {
			logger.ErrorContext(ctx, "daily reflection: bad payload", "err", err)
			return nil
		}
		if job.Namespace != "" {
			if err := r.ReflectNamespace(ctx, job.Namespace); err != nil {
				return fmt.Errorf("reflect namespace %s: %w", job.Namespace, err)
			}
			return nil
		}
		if err := r.ReflectAllActive(ctx); err != nil {
			return fmt.Errorf("reflect all active: %w", err)
		}
		return nil
	}
}
