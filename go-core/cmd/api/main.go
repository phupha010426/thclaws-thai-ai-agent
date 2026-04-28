// thaiagent-api is the public HTTP surface: LINE webhook, PDPA admin
// endpoints, health, and Prometheus metrics. It enqueues every LINE event
// onto Asynq via the queue package; the actual processing lives in
// thaiagent-worker so a webhook spike cannot block message reasoning.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"

	"github.com/thaiaiagent/go-core/internal/config"
	"github.com/thaiaiagent/go-core/internal/db/mongo"
	"github.com/thaiaiagent/go-core/internal/db/postgres"
	rediscli "github.com/thaiaiagent/go-core/internal/db/redis"
	"github.com/thaiaiagent/go-core/internal/ledger"
	"github.com/thaiaiagent/go-core/internal/liff"
	"github.com/thaiaiagent/go-core/internal/line"
	"github.com/thaiaiagent/go-core/internal/media"
	"github.com/thaiaiagent/go-core/internal/memory"
	"github.com/thaiaiagent/go-core/internal/metrics"
	"github.com/thaiaiagent/go-core/internal/pdpa"
	"github.com/thaiaiagent/go-core/internal/queue"
	"github.com/thaiaiagent/go-core/internal/sharedlog"
	"github.com/thaiaiagent/go-core/internal/storage"
	"github.com/thaiaiagent/go-core/internal/thaitime"
	"github.com/thaiaiagent/go-core/internal/users"
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

	mongoClient, err := mongo.New(rootCtx, cfg.MongoURL)
	must(logger, err, "mongo")
	defer mongoClient.Disconnect(context.Background())

	redisClient, err := rediscli.New(rootCtx, cfg.RedisURL)
	must(logger, err, "redis")
	defer redisClient.Close()

	dbName := mongo.DBNameFromURI(cfg.MongoURL)
	memoryService := memory.New(mongoClient, dbName, logger, nil, nil)
	objectStore, err := storage.NewMinIO(
		cfg.MinioEndpoint+":"+strconv.Itoa(cfg.MinioPort),
		cfg.MinioAccessKey,
		cfg.MinioSecretKey,
		cfg.MinioBucket,
		cfg.MinioUseSSL,
		logger,
	)
	must(logger, err, "minio")
	imageAccessSecret := cfg.ImageAccessSecret
	if imageAccessSecret == "" {
		imageAccessSecret = cfg.MinioSecretKey
	}
	mediaHandler := media.NewHandler(memoryService, objectStore, imageAccessSecret)
	usersSvc := users.New(pgPool)
	ledgerWriter := ledger.NewWriter(pgPool)
	liffHandler := liff.NewHandler(usersSvc, ledgerWriter, memoryService, imageAccessSecret, cfg.LiffID, cfg.LiffChannelID, logger)

	queueClient := queue.NewClient(asynqRedisOpt(cfg.RedisURL), cfg.RedisPrefix, logger)
	defer queueClient.Close()

	rateLimiter := line.NewRateLimiter(redisClient, 30, 60*time.Second, logger, ratelimitMetricsAdapter{})

	webhookHandler := line.NewWebhookHandler(
		cfg.LineChannelSecret,
		nil, // reply happens in worker, not on the webhook hop
		rateLimiter,
		lineEnqueuerAdapter{client: queueClient},
		logger,
	)

	pdpaService := pdpa.New(pgPool, mongoClient, mongo.DBNameFromURI(cfg.MongoURL), redisClient, logger)
	pdpaHandler := pdpa.NewHandler(pdpa.HandlerConfig{
		Service:       pdpaService,
		GatewayAPIKey: cfg.GatewayAPIKey,
		Logger:        logger,
	})

	r := chi.NewRouter()
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "service": "thaiagent-api", "time": thaitime.ShortDateTime(time.Now()),
		})
	})
	r.Handle("/metrics", metrics.Handler())
	mediaHandler.Register(r)
	r.Route("/liff", liffHandler.Register)
	r.Post("/webhook/line", webhookHandler.Handler)
	r.Mount("/pdpa", pdpaHandler)

	srv := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.Port),
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info("thaiagent-api listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server crashed", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	logger.Info("shutting down")
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutCancel()
	_ = srv.Shutdown(shutCtx)
}

func must(logger *slog.Logger, err error, what string) {
	if err != nil {
		logger.Error("startup failed", "component", what, "err", err)
		os.Exit(1)
	}
}

func asynqRedisOpt(url string) asynq.RedisClientOpt {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return asynq.RedisClientOpt{Addr: "redis:6379"}
	}
	return asynq.RedisClientOpt{Addr: opts.Addr, DB: opts.DB, Username: opts.Username, Password: opts.Password}
}

type lineEnqueuerAdapter struct{ client *queue.Client }

func (a lineEnqueuerAdapter) EnqueueLineEvent(ctx context.Context, env line.EventEnvelope) error {
	payload, err := json.Marshal(env)
	if err != nil {
		return err
	}
	return a.client.EnqueueLineEvent(ctx, payload)
}

type ratelimitMetricsAdapter struct{}

func (ratelimitMetricsAdapter) Inc(name string) { metrics.Inc(name) }
