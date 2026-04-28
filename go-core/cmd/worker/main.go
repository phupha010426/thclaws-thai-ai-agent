// thaiagent-worker is the Asynq processor. It dequeues LINE webhook events,
// memory compactions, brain extractions, and the nightly reflection cron and
// runs them against the brain + memory + gateway implementations.
package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/bson"

	"github.com/thaiaiagent/go-core/internal/agents"
	"github.com/thaiaiagent/go-core/internal/brain"
	"github.com/thaiaiagent/go-core/internal/cache"
	"github.com/thaiaiagent/go-core/internal/config"
	"github.com/thaiaiagent/go-core/internal/db/mongo"
	"github.com/thaiaiagent/go-core/internal/db/postgres"
	rediscli "github.com/thaiaiagent/go-core/internal/db/redis"
	"github.com/thaiaiagent/go-core/internal/dispatcher"
	"github.com/thaiaiagent/go-core/internal/ledger"
	"github.com/thaiaiagent/go-core/internal/liff"
	"github.com/thaiaiagent/go-core/internal/line"
	"github.com/thaiaiagent/go-core/internal/memory"
	"github.com/thaiaiagent/go-core/internal/parser"
	"github.com/thaiaiagent/go-core/internal/queue"
	"github.com/thaiaiagent/go-core/internal/sharedlog"
	"github.com/thaiaiagent/go-core/internal/storage"
	"github.com/thaiaiagent/go-core/internal/users"
	"github.com/thaiaiagent/go-core/internal/websearch"
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

	// Caches — shared between gateway (sml response cache) and brain (graph
	// retrieval cache). Both are namespace-strict by construction.
	smlCache := cache.NewSmlGatewayCache(redisClient, logger)
	brainCache := brain.NewBrainCache(redisClient, logger)

	gateway := agents.New(&http.Client{Timeout: time.Duration(cfg.GatewayTimeoutMs) * time.Millisecond},
		cfg.GatewayURL, cfg.GatewayAPIKey, smlCache, logger)

	wiki := brain.NewWikiBrainService(pgPool, brainCache, logger)
	embedding := brain.NewEmbeddingService(pgPool, gatewayAdapter{gateway}, cfg.EmbeddingModel, cfg.ListString(cfg.EmbeddingFallbackModels), logger)
	extractor := brain.NewExtractionService(gatewayAdapter{gateway}, redisClient, wiki, embedding,
		cfg.FastAgentModel, []string{cfg.DefaultAgentModel, cfg.GeneralAgentModel}, logger)

	dbName := mongo.DBNameFromURI(cfg.MongoURL)
	convAdapter := &brain.MongoConversationAdapter{Coll: mongoClient.Database(dbName).Collection(memory.ColConversationMessages)}
	reflector := brain.NewReflectionService(convAdapter, extractor, logger)

	memoryService := memory.New(mongoClient, dbName, logger, noopAudit{}, wikiReadAdapter{wiki: wiki})
	objectStore, err := storage.NewMinIO(
		fmt.Sprintf("%s:%d", cfg.MinioEndpoint, cfg.MinioPort),
		cfg.MinioAccessKey,
		cfg.MinioSecretKey,
		cfg.MinioBucket,
		cfg.MinioUseSSL,
		logger,
	)
	must(logger, err, "minio")
	if err := objectStore.EnsureBucket(rootCtx); err != nil {
		logger.Warn("minio bucket ensure failed; image handling may fail", "err", err)
	}

	lineClient, err := line.New(cfg.LineChannelAccessToken, &http.Client{Timeout: 10 * time.Second}, logger)
	if err != nil {
		logger.Error("line client init failed", "err", err)
		os.Exit(1)
	}
	usersSvc := users.New(pgPool)
	ledgerWriter := ledger.NewWriter(pgPool)
	summaryReader := ledger.NewSummaryReader(ledgerWriter)
	imageAccessSecret := cfg.ImageAccessSecret
	if imageAccessSecret == "" {
		imageAccessSecret = cfg.MinioSecretKey
	}
	liffHandler := liff.NewHandler(usersSvc, ledgerWriter, memoryService, imageAccessSecret, cfg.LiffID, cfg.LiffChannelID, logger)
	queueClient := queue.NewClient(asynqRedisOpt(cfg.RedisURL), cfg.RedisPrefix, logger)
	defer queueClient.Close()
	webSearchSvc := websearch.New(&http.Client{Timeout: 8 * time.Second}, logger)
	dispatchSvc := dispatcher.New(
		replierAdapter{client: lineClient},
		gatewayChatAdapter{inner: gateway, cfg: cfg},
		lineClient,
		objectStore,
		visionAdapter{inner: gateway, cfg: cfg, logger: logger},
		queueClient,
		webSearchSvc,
		liffHandler,
		usersSvc,
		memoryService,
		ledgerWriter,
		summaryReader,
		cfg.PublicBaseURL,
		imageAccessSecret,
		cfg.LiffID,
		time.Duration(cfg.LineReplyTimeoutMs)*time.Millisecond,
		logger,
	)

	worker := queue.NewWorker(asynqRedisOpt(cfg.RedisURL), 10, cfg.RedisPrefix, logger)
	worker.RegisterHandlers(
		dispatchSvc,
		memoryCompactorAdapter{ms: memoryService},
		extractorAdapter{e: extractor},
		reflectorAdapter{r: reflector},
	)

	if err := queueClient.EnsureDailyReflectionCron(rootCtx, asynqRedisOpt(cfg.RedisURL)); err != nil {
		logger.Warn("failed to install reflection cron", "err", err)
	}

	go func() {
		if err := worker.Run(rootCtx); err != nil {
			logger.Error("worker stopped", "err", err)
			os.Exit(1)
		}
	}()
	logger.Info("thaiagent-worker started")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	logger.Info("shutting down")
	cancel()
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutCancel()
	worker.Shutdown(shutCtx)
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

// gatewayAdapter bridges agents.GatewayService -> brain.GatewayClient. The
// brain package keeps a slim interface so it can be unit tested without the
// full HTTP plumbing in agents.
type gatewayAdapter struct{ inner *agents.GatewayService }

func (g gatewayAdapter) Embed(ctx context.Context, req brain.EmbedRequest) ([]float64, error) {
	return g.inner.Embed(ctx, agents.EmbedRequest{Model: req.Model, FallbackModels: req.FallbackModels, Text: req.Text})
}

func (g gatewayAdapter) Chat(ctx context.Context, req brain.ChatRequest) (string, error) {
	msgs := make([]map[string]any, 0, len(req.Messages))
	for _, m := range req.Messages {
		msgs = append(msgs, map[string]any{"role": m.Role, "content": m.Content})
	}
	return g.inner.Chat(ctx, agents.ChatRequest{
		Namespace:      req.Namespace,
		Model:          req.Model,
		FallbackModels: req.FallbackModels,
		MaxTokens:      req.MaxTokens,
		Messages:       msgs,
	})
}

// wikiReadAdapter satisfies memory.WikiBrainReader without coupling memory ->
// brain at the import level (the interface lives in memory, the impl in brain).
type wikiReadAdapter struct{ wiki *brain.WikiBrainService }

func (a wikiReadAdapter) ListByImportance(ctx context.Context, namespace string, limit int) ([]memory.WikiPageSummary, error) {
	pages, err := a.wiki.ListByImportance(ctx, namespace, limit)
	if err != nil {
		return nil, err
	}
	out := make([]memory.WikiPageSummary, 0, len(pages))
	for _, p := range pages {
		summary := ""
		if p.Summary != nil {
			summary = *p.Summary
		}
		out = append(out, memory.WikiPageSummary{
			Title:      p.Title,
			Kind:       p.Kind,
			Summary:    summary,
			Importance: p.Importance,
			UpdatedAt:  p.UpdatedAt,
		})
	}
	return out, nil
}

type noopAudit struct{}

func (noopAudit) Log(_ context.Context, _, _, _ string, _ map[string]any) error { return nil }

type memoryCompactorAdapter struct{ ms *memory.MemoryService }

func (a memoryCompactorAdapter) CompactConversation(ctx context.Context, ownerID, agentID, namespace string) error {
	_, err := a.ms.CompactConversationIfNeeded(ctx, memory.CompactInput{OwnerID: ownerID, AgentID: agentID, Namespace: namespace})
	return err
}

type extractorAdapter struct{ e *brain.ExtractionService }

func (a extractorAdapter) ExtractAndStore(ctx context.Context, in queue.ExtractInput) error {
	_, _, err := a.e.ExtractAndStore(ctx, brain.ExtractInput{
		Namespace:   in.Namespace,
		OwnerID:     in.OwnerID,
		AgentID:     in.AgentID,
		Text:        in.Text,
		CausationID: in.CausationID,
	})
	return err
}

type reflectorAdapter struct{ r *brain.ReflectionService }

func (a reflectorAdapter) ReflectNamespace(ctx context.Context, namespace string) error {
	return a.r.ReflectNamespace(ctx, namespace)
}
func (a reflectorAdapter) ReflectAllActive(ctx context.Context) error {
	return a.r.ReflectAllActive(ctx)
}

// stub used by reflection adapter when iterating namespaces; bson import
// retained because the underlying mongo driver requires it for queries.
var _ = bson.M{}

type replierAdapter struct{ client *line.Client }

func (a replierAdapter) ReplyText(ctx context.Context, replyToken, text string) error {
	return a.client.ReplyText(ctx, replyToken, text)
}

func (a replierAdapter) ReplyImage(ctx context.Context, replyToken, text, imageURL string) error {
	return a.client.ReplyImage(ctx, replyToken, text, imageURL)
}

func (a replierAdapter) ReplyAccountingCard(ctx context.Context, replyToken, direction, note string, amount float64, category, occurredAt, projectText string) error {
	return a.client.ReplyAccountingCard(ctx, replyToken, direction, note, amount, category, occurredAt, projectText)
}

func (a replierAdapter) ReplySummaryCard(ctx context.Context, replyToken, title, body string) error {
	return a.client.ReplySummaryCard(ctx, replyToken, title, body)
}

func (a replierAdapter) ReplyOpenAppCard(ctx context.Context, replyToken, appURL string) error {
	return a.client.ReplyOpenAppCard(ctx, replyToken, appURL)
}

func (a replierAdapter) ReplyTextWithChoices(ctx context.Context, replyToken, text string, choices []string) error {
	return a.client.ReplyTextWithChoices(ctx, replyToken, text, choices)
}

func (a replierAdapter) ReplyTextsWithChoices(ctx context.Context, replyToken string, leadTexts []string, text string, choices []string) error {
	return a.client.ReplyTextsWithChoices(ctx, replyToken, leadTexts, text, choices)
}

type gatewayChatAdapter struct {
	inner *agents.GatewayService
	cfg   *config.Config
}

func (a gatewayChatAdapter) Chat(ctx context.Context, namespace, sessionID, model, fastModel, generalModel, prompt string) (string, error) {
	maxLatency := a.cfg.GatewayMaxLatencyMs
	maxTokens := chatMaxTokens(prompt)
	primaryModel := model
	fallbacks := []string{generalModel, fastModel}
	if maxTokens > 800 {
		primaryModel = generalModel
		fallbacks = []string{model, fastModel}
		if maxLatency < 6000 {
			maxLatency = 6000
		}
	}
	return a.inner.Chat(ctx, agents.ChatRequest{
		Namespace:        namespace,
		SessionID:        sessionID,
		Model:            primaryModel,
		FallbackModels:   fallbacks,
		MaxTokens:        maxTokens,
		Strategy:         "fastest",
		MaxLatencyMs:     &maxLatency,
		PreferProviders:  a.cfg.GatewayPreferProviders,
		ExcludeProviders: a.cfg.GatewayExcludeProviders,
		Messages: []map[string]any{
			{"role": "system", "content": "You are ThaiAiAgent running inside the thClaws session broker. Use the provided session_id as the durable conversation identity. Act as a Thai personal secretary: concise Thai, natural, lightly funny when suitable. Use ครับ only, never write ครับ/ค่ะ. Never fabricate facts, never claim a database/tool action happened unless the prompt contains real tool/ledger result. If confidence is low or context is missing, ask a short clarifying question with practical options instead of guessing. You may ask again until confident."},
			{"role": "system", "content": "Hard rule: account balances, transaction history, personal facts, and image details must come only from the provided namespace-scoped history/tool results. If no real evidence is provided for a personal claim, say you do not have enough information and ask for confirmation. For general knowledge, recipes, planning, and advice, answer normally as general advice but do not pretend it came from the user's private data."},
			{"role": "system", "content": "session_id=" + sessionID + "\nruntime=thclaws-session-broker\nnamespace=" + namespace},
			{"role": "user", "content": prompt},
		},
	})
}

func chatMaxTokens(prompt string) int {
	lower := strings.ToLower(prompt)
	longMarkers := []string{
		"สูตร", "วิธีทำ", "ทำยังไง", "ทำอย่างไร", "อธิบาย", "ละเอียด", "แผน", "ขั้นตอน",
		"เปรียบเทียบ", "วิเคราะห์", "สรุปให้ครบ", "เล่า", "เขียน", "ร่าง",
	}
	for _, marker := range longMarkers {
		if strings.Contains(lower, marker) {
			return 1400
		}
	}
	if len([]rune(prompt)) > 4500 {
		return 900
	}
	return 650
}

type visionAdapter struct {
	inner  *agents.GatewayService
	cfg    *config.Config
	logger *slog.Logger
}

func (a visionAdapter) AnalyzeImage(ctx context.Context, namespace, sessionID string, data []byte, contentType string) (parser.VisionResult, error) {
	imageURL := "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data)
	maxLatency := max(a.cfg.GatewayMaxLatencyMs, 9000)
	messages := visionMessages(namespace, sessionID, imageURL)
	schema := visionSchema()

	if raw, err := a.inner.Structured(ctx, agents.StructuredRequest{
		Namespace:        namespace,
		Model:            a.cfg.VisionAgentModel,
		FallbackModels:   a.cfg.ListString(a.cfg.VisionFallbackModels),
		Messages:         messages,
		Schema:           schema,
		MaxRetries:       1,
		TimeoutMs:        a.cfg.GatewayTimeoutMs,
		Strategy:         "strongest",
		MaxLatencyMs:     &maxLatency,
		PreferProviders:  a.cfg.GatewayPreferProviders,
		ExcludeProviders: "nvidia",
	}); err == nil {
		out := parser.ParseVision(string(raw))
		if usableVision(out) {
			return out.Result, nil
		}
		a.logger.Warn("vision structured returned unusable content", "namespace", namespace, "reply", truncateLog(out.Result.FinalReply, 120))
	} else {
		a.logger.Warn("vision structured failed", "namespace", namespace, "err", err)
	}

	models := append([]string{a.cfg.VisionAgentModel}, a.cfg.ListString(a.cfg.VisionFallbackModels)...)
	for _, model := range models {
		content, err := a.inner.Chat(ctx, agents.ChatRequest{
			Namespace:        namespace,
			SessionID:        sessionID,
			Model:            model,
			TimeoutMs:        a.cfg.GatewayTimeoutMs,
			MaxTokens:        1400,
			ResponseFormat:   map[string]any{"type": "json_object"},
			Strategy:         "strongest",
			MaxLatencyMs:     &maxLatency,
			PreferProviders:  a.cfg.GatewayPreferProviders,
			ExcludeProviders: "nvidia",
			Messages:         messages,
		})
		if err != nil {
			a.logger.Warn("vision chat model failed", "namespace", namespace, "model", model, "err", err)
			continue
		}
		out := parser.ParseVision(content)
		if usableVision(out) {
			if out.UsedFallback {
				a.logger.Warn("vision chat parsed via fallback but accepted", "namespace", namespace, "model", model)
			}
			return out.Result, nil
		}
		a.logger.Warn("vision chat unusable; trying next model", "namespace", namespace, "model", model, "reply", truncateLog(out.Result.FinalReply, 120))
	}

	return parser.VisionResult{
		FinalReply:    "รับรูปไว้แล้วครับ แต่ AI อ่านรายละเอียดจากรูปนี้ไม่สำเร็จ ขอส่งรูปชัดขึ้นอีกครั้งนะครับ",
		IsSlip:        false,
		DirectionHint: "UNKNOWN",
		Confidence:    0,
	}, nil
}

func visionMessages(namespace, sessionID, imageURL string) []map[string]any {
	return []map[string]any{
		{
			"role": "system",
			"content": strings.Join([]string{
				"You are ThaiAiAgent vision secretary running inside the thClaws session broker.",
				"The image is user-provided for personal accounting OCR. This is allowed.",
				"Read the image carefully and answer in Thai only.",
				"If it is a Thai bank transfer slip or Scan to Pay slip, extract only visible facts: amount, sender, receiver, masked accounts, bank, transfer time, reference number if visible, and explain who transferred to whom.",
				"Never invent account ownership. If owner account is unclear, set directionHint UNKNOWN and ask the user to confirm whether it is income, expense, transfer, or not recorded.",
				"Return only one JSON object. No markdown. No English apology.",
			}, "\n"),
		},
		{
			"role": "user",
			"content": []map[string]any{
				{"type": "text", "text": "namespace=" + namespace + ". session_id=" + sessionID + ". อ่านรูปนี้และสรุปเป็น JSON ภาษาไทย"},
				{"type": "image_url", "image_url": map[string]any{"url": imageURL}},
			},
		},
	}
}

func visionSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"finalReply", "isSlip", "directionHint", "confidence"},
		"properties": map[string]any{
			"finalReply":        map[string]any{"type": "string"},
			"isSlip":            map[string]any{"type": "boolean"},
			"amount":            map[string]any{"type": "number"},
			"currency":          map[string]any{"type": "string"},
			"transferAt":        map[string]any{"type": "string"},
			"fromAccountMasked": map[string]any{"type": "string"},
			"toAccountMasked":   map[string]any{"type": "string"},
			"fromName":          map[string]any{"type": "string"},
			"toName":            map[string]any{"type": "string"},
			"bankName":          map[string]any{"type": "string"},
			"directionHint":     map[string]any{"type": "string", "enum": []string{"INCOME", "EXPENSE", "TRANSFER", "UNKNOWN"}},
			"confidence":        map[string]any{"type": "number"},
		},
	}
}

func usableVision(out parser.VisionOutcome) bool {
	reply := strings.TrimSpace(out.Result.FinalReply)
	if out.UsedFallback || reply == "" {
		return false
	}
	lower := strings.ToLower(reply)
	bad := []string{"i'm sorry", "i am sorry", "cannot provide", "can't provide", "cannot comply", "sorry,"}
	for _, marker := range bad {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	hasThai := false
	for _, r := range reply {
		if r >= 0x0E00 && r <= 0x0E7F {
			hasThai = true
			break
		}
	}
	return hasThai
}

func truncateLog(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
