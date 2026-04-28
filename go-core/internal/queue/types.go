// Package queue wraps Asynq to provide typed job enqueuing and worker
// registration for the Thai AI Agent. Every payload that carries user data
// must include a Namespace field so the worker can scope all DB/cache
// operations to a single LINE userId without accidentally crossing tenants.
package queue

import (
	"context"
	"encoding/json"
	"time"
)

// Task type constants mirror the BullMQ job names used by the Node service so
// that jobs written by Node workers (during the migration window) can also be
// consumed here.
const (
	TypeLineWebhook     = "line:webhook"
	TypeMemoryCompact   = "memory:compact"
	TypeBrainExtract    = "memory:extract_brain"
	TypeDailyReflection = "memory:daily_reflection"
)

// Queue name constants. Each name maps to a Redis key prefix so different job
// families can get independent concurrency limits and retry budgets.
const (
	QueueLineEvents = "line_webhook_events"
	QueueMemory     = "memory_jobs"
	QueueDefault    = "default"
)

// LineWebhookJob carries the raw LINE webhook event byte-for-byte as received
// from the LINE platform. ReceivedAt is an RFC-3339 wall-clock stamp recorded
// at the HTTP handler so we can measure end-to-end latency later.
type LineWebhookJob struct {
	Event      json.RawMessage `json:"Event"`
	ReceivedAt string          `json:"ReceivedAt"`
}

type receivedAtKey struct{}

// WithReceivedAt attaches the original webhook receive time to the worker
// context. It lets downstream handlers stop expensive work before LINE's reply
// token window becomes risky.
func WithReceivedAt(ctx context.Context, value string) context.Context {
	if value == "" {
		return ctx
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return ctx
	}
	return context.WithValue(ctx, receivedAtKey{}, t)
}

func ReceivedAt(ctx context.Context) (time.Time, bool) {
	if ctx == nil {
		return time.Time{}, false
	}
	t, ok := ctx.Value(receivedAtKey{}).(time.Time)
	return t, ok
}

// MemoryCompactJob requests a conversation compaction cycle for one agent
// scoped to Namespace. Both OwnerID and AgentID are required because the
// compaction query joins across the owner→agent→conversation hierarchy.
type MemoryCompactJob struct {
	OwnerID   string `json:"OwnerID"`
	AgentID   string `json:"AgentID"`
	Namespace string `json:"Namespace"`
}

// BrainExtractJob asks the knowledge-graph extractor to parse Text and store
// facts under the given Namespace. CausationID traces back to the conversation
// message that triggered the extraction so we can avoid duplicate work.
type BrainExtractJob struct {
	OwnerID     string `json:"OwnerID"`
	AgentID     string `json:"AgentID"`
	Namespace   string `json:"Namespace"`
	Text        string `json:"Text"`
	CausationID string `json:"CausationID,omitempty"`
}

// DailyReflectionJob drives the nightly reflection cron. When Namespace is
// empty the handler must look up all namespaces that had activity in the last
// 24 hours and reflect each one independently — no cross-namespace aggregation.
type DailyReflectionJob struct {
	Namespace string `json:"Namespace,omitempty"`
}
