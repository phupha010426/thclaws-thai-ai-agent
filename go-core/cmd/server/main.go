package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/thaiaiagent/go-core/internal/events"
	"github.com/thaiaiagent/go-core/internal/httpserver"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	addr := os.Getenv("CORE_LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	rootCtx, cancelRoot := context.WithCancel(context.Background())
	defer cancelRoot()

	// Optional NATS consumer. Empty NATS_URL keeps the service running as a
	// pure HTTP parser; presence wires JetStream subscriptions for the events
	// the Node service emits via the Postgres outbox.
	var natsConsumer *events.Consumer
	if natsURL := os.Getenv("NATS_URL"); natsURL != "" {
		c, err := events.Connect(rootCtx, logger, events.Options{
			URL:            natsURL,
			Durable:        "core-line-events",
			FilterSubjects: []string{"line.event.>", "wiki.fact.>", "ledger.tx.>"},
			Handler: func(_ context.Context, namespace string, subject string, data []byte) error {
				logger.Info("event received", "subject", subject, "namespace", namespace, "size", len(data))
				return nil
			},
		})
		if err != nil {
			logger.Error("nats consumer disabled", "err", err)
		} else {
			natsConsumer = c
		}
	} else {
		logger.Info("NATS_URL not set; running as HTTP-only core")
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           httpserver.New(logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info("thaiagent-core listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server crashed", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	logger.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if natsConsumer != nil {
		natsConsumer.Close()
	}
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("shutdown failed", "err", err)
		os.Exit(1)
	}
}
