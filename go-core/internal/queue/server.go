package queue

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/hibiken/asynq"
)

// Worker wraps asynq.Server and owns the handler mux. It is the single entry
// point for registering all job-type handlers and starting the processing loop.
type Worker struct {
	server *asynq.Server
	mux    *asynq.ServeMux
	logger *slog.Logger
}

// NewWorker returns a Worker configured with the given concurrency limit and
// Redis prefix. Concurrency defaults to 10 in production; callers can pass a
// lower value for test environments.
func NewWorker(redisOpt asynq.RedisClientOpt, concurrency int, prefix string, logger *slog.Logger) *Worker {
	if concurrency <= 0 {
		concurrency = 10
	}
	srv := asynq.NewServer(redisOpt, asynq.Config{
		// Distribute concurrency across queues by priority: LINE events get
		// 50% of slots so webhook latency stays low; memory jobs share the rest.
		Queues: map[string]int{
			QueueLineEvents: 5,
			QueueMemory:     3,
			QueueDefault:    2,
		},
		Concurrency: concurrency,
	})
	return &Worker{server: srv, mux: asynq.NewServeMux(), logger: logger}
}

// Handle registers a handler for the given task type. The adapter converts the
// asynq.Task into the handler function signature used by this package.
func (w *Worker) Handle(taskType string, h asynq.Handler) {
	w.mux.Handle(taskType, h)
}

// HandleFunc registers a handler function for the given task type.
func (w *Worker) HandleFunc(taskType string, fn func(context.Context, *asynq.Task) error) {
	w.mux.HandleFunc(taskType, fn)
}

// RegisterHandlers wires every typed handler into the mux. Dependencies are
// passed in so this package never imports concrete brain/memory packages —
// only the interfaces defined in handlers.go.
func (w *Worker) RegisterHandlers(
	lineDispatcher LineEventDispatcher,
	compactor MemoryCompactor,
	extractor Extractor,
	reflector Reflector,
) {
	w.HandleFunc(TypeLineWebhook, HandleLineWebhook(lineDispatcher, w.logger))
	w.HandleFunc(TypeMemoryCompact, HandleMemoryCompact(compactor, w.logger))
	w.HandleFunc(TypeBrainExtract, HandleBrainExtract(extractor, w.logger))
	w.HandleFunc(TypeDailyReflection, HandleDailyReflection(reflector, w.logger))
}

// Run starts processing jobs and blocks until ctx is cancelled. It returns
// only after all in-flight jobs have finished (graceful drain).
func (w *Worker) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		if err := w.server.Run(w.mux); err != nil {
			errCh <- fmt.Errorf("queue worker: %w", err)
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		w.server.Shutdown()
		// Drain any error that surfaces during shutdown.
		<-errCh
		return nil
	case err := <-errCh:
		return err
	}
}

// Shutdown drains in-flight jobs and stops the server. It is idempotent.
func (w *Worker) Shutdown(_ context.Context) {
	w.server.Shutdown()
}
