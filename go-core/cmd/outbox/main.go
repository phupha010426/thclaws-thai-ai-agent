// thaiagent-outbox drains the Postgres event_outbox into NATS JetStream.
// Lives in its own process so a LINE traffic spike on the worker cannot
// starve the relay loop.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/thaiaiagent/go-core/internal/config"
	"github.com/thaiaiagent/go-core/internal/db/postgres"
	"github.com/thaiaiagent/go-core/internal/events"
	"github.com/thaiaiagent/go-core/internal/sharedlog"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	logger := sharedlog.New(cfg.LogLevel)
	rootCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pgPool, err := postgres.New(rootCtx, cfg.PostgresURL)
	must(logger, err, "postgres")
	defer pgPool.Close()

	publisher, err := events.NewPublisher(rootCtx, cfg.NatsURL, cfg.NatsStreamName, logger)
	must(logger, err, "nats")
	defer publisher.Close()

	relay := events.NewRelay(pgPool, publisher, logger, noopMetrics{})

	go relay.Run(rootCtx)
	logger.Info("thaiagent-outbox started")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	logger.Info("shutting down")
	cancel()
	time.Sleep(500 * time.Millisecond)
}

func must(logger *slog.Logger, err error, what string) {
	if err != nil {
		logger.Error("startup failed", "component", what, "err", err)
		os.Exit(1)
	}
}

type noopMetrics struct{}

func (noopMetrics) RecordPublished(_ string)  {}
func (noopMetrics) RecordFailure(_ string)    {}
func (noopMetrics) RecordSkipped(_ string)    {}
