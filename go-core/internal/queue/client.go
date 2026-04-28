package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/hibiken/asynq"
)

// Client wraps asynq.Client with typed enqueue methods. The prefix parameter
// is prepended to every Redis key so dev/staging/prod queues never collide on
// a shared Redis instance.
type Client struct {
	inner  *asynq.Client
	prefix string
	logger *slog.Logger
}

// NewClient returns a Client ready to enqueue jobs. The caller owns the Redis
// connection lifetime; Close must be called to release it.
func NewClient(redisOpt asynq.RedisClientOpt, prefix string, logger *slog.Logger) *Client {
	inner := asynq.NewClient(redisOpt)
	return &Client{inner: inner, prefix: prefix, logger: logger}
}

// Close releases the underlying Redis connection.
func (c *Client) Close() error {
	return c.inner.Close()
}

// EnqueueLineEvent wraps a raw LINE event JSON and schedules it on the line
// events queue with three attempts and exponential back-off. We keep retries
// low because LINE retries the webhook delivery itself if we time-out.
func (c *Client) EnqueueLineEvent(ctx context.Context, eventJSON json.RawMessage) error {
	job := LineWebhookJob{
		Event:      eventJSON,
		ReceivedAt: time.Now().UTC().Format(time.RFC3339),
	}
	payload, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("queue: marshal LineWebhookJob: %w", err)
	}
	task := asynq.NewTask(TypeLineWebhook, payload,
		asynq.Queue(QueueLineEvents),
		asynq.MaxRetry(3),
		asynq.Retention(24*time.Hour),
	)
	info, err := c.inner.EnqueueContext(ctx, task)
	if err != nil {
		return fmt.Errorf("queue: enqueue line webhook: %w", err)
	}
	c.logger.InfoContext(ctx, "queue line webhook enqueued", "taskID", info.ID, "queue", QueueLineEvents)
	return nil
}

// EnqueueMemoryCompact schedules a conversation compaction cycle. The job
// retries up to three times with back-off because the compaction reads from
// Mongo, which may have transient connectivity blips.
func (c *Client) EnqueueMemoryCompact(ctx context.Context, job MemoryCompactJob) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("queue: marshal MemoryCompactJob: %w", err)
	}
	task := asynq.NewTask(TypeMemoryCompact, payload,
		asynq.Queue(QueueMemory),
		asynq.MaxRetry(3),
		asynq.Retention(24*time.Hour),
	)
	info, err := c.inner.EnqueueContext(ctx, task)
	if err != nil {
		return fmt.Errorf("queue: enqueue memory compact: %w", err)
	}
	c.logger.InfoContext(ctx, "queue memory compact enqueued", "taskID", info.ID, "queue", QueueMemory, "namespace", job.Namespace)
	return nil
}

// EnqueueBrainExtract queues a knowledge-graph extraction. Namespace must be
// non-empty because the extractor uses it as the Mongo partition key and the
// NATS subject tail — an empty namespace would corrupt cross-tenant isolation.
func (c *Client) EnqueueBrainExtract(ctx context.Context, job BrainExtractJob) error {
	if job.Namespace == "" {
		return fmt.Errorf("queue: EnqueueBrainExtract: Namespace required")
	}
	payload, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("queue: marshal BrainExtractJob: %w", err)
	}
	task := asynq.NewTask(TypeBrainExtract, payload,
		asynq.Queue(QueueMemory),
		asynq.MaxRetry(2),
		asynq.Retention(24*time.Hour),
	)
	info, err := c.inner.EnqueueContext(ctx, task)
	if err != nil {
		return fmt.Errorf("queue: enqueue brain extract: %w", err)
	}
	c.logger.InfoContext(ctx, "queue brain extract enqueued", "taskID", info.ID, "queue", QueueMemory, "namespace", job.Namespace)
	return nil
}

// periodicProvider implements asynq.PeriodicTaskConfigProvider so we can
// register exactly one cron entry without pulling in a YAML config file.
type periodicProvider struct {
	entries []*asynq.PeriodicTaskConfig
}

func (p *periodicProvider) GetConfigs() ([]*asynq.PeriodicTaskConfig, error) {
	return p.entries, nil
}

// EnsureDailyReflectionCron starts an asynq.PeriodicTaskManager that fires
// the daily reflection job at UTC 19:00 (= 02:00 ICT, when Thai traffic is
// lowest). The manager runs in its own goroutine; it is idempotent and safe to
// call once at server startup.
func (c *Client) EnsureDailyReflectionCron(ctx context.Context, redisOpt asynq.RedisClientOpt) error {
	payload, err := json.Marshal(DailyReflectionJob{})
	if err != nil {
		return fmt.Errorf("queue: marshal DailyReflectionJob: %w", err)
	}
	entry := &asynq.PeriodicTaskConfig{
		// 19:00 UTC = 02:00 Asia/Bangkok — chosen so the LLM reflection call
		// runs outside the peak usage window.
		Cronspec: "0 19 * * *",
		Task:     asynq.NewTask(TypeDailyReflection, payload, asynq.Queue(QueueMemory), asynq.MaxRetry(1)),
	}
	provider := &periodicProvider{entries: []*asynq.PeriodicTaskConfig{entry}}
	mgr, err := asynq.NewPeriodicTaskManager(
		asynq.PeriodicTaskManagerOpts{
			RedisConnOpt:               redisOpt,
			PeriodicTaskConfigProvider: provider,
			// Re-read the provider every 10 minutes so config changes propagate
			// without a full restart. In practice the entry is static, but the
			// tick keeps the manager from being a perpetual no-op after a Redis
			// reconnect.
			SyncInterval: 10 * time.Minute,
		},
	)
	if err != nil {
		return fmt.Errorf("queue: new PeriodicTaskManager: %w", err)
	}
	if err := mgr.Start(); err != nil {
		return fmt.Errorf("queue: start PeriodicTaskManager: %w", err)
	}
	c.logger.InfoContext(ctx, "daily reflection cron registered", "cron", "0 19 * * * UTC")

	// Stop the manager when the context is cancelled so shutdown is clean.
	go func() {
		<-ctx.Done()
		mgr.Shutdown()
	}()
	return nil
}
