package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultTimeoutMs = 30_000
	defaultMaxTokens = 1200
)

// GatewayCache is the subset of cache.SmlGatewayCache that GatewayService needs.
// Using an interface lets tests inject a stub without spinning up Redis.
type GatewayCache interface {
	Get(ctx context.Context, namespace, model string, messages, schema any, strategy, prefer, exclude string) (string, bool)
	Set(ctx context.Context, namespace, model string, messages, schema any, strategy, prefer, exclude, value string)
}

type GatewayService struct {
	http    *http.Client
	baseURL string
	apiKey  string
	cache   GatewayCache
	log     *slog.Logger
}

func New(httpClient *http.Client, baseURL, apiKey string, c GatewayCache, log *slog.Logger) *GatewayService {
	return &GatewayService{
		http:    httpClient,
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		cache:   c,
		log:     log,
	}
}

// ChatRequest mirrors the TypeScript chat() input struct.
type ChatRequest struct {
	Namespace        string
	SessionID        string
	Model            string
	FallbackModels   []string
	Messages         any
	TimeoutMs        int
	MaxTokens        int
	ResponseFormat   map[string]any
	Strategy         string // "fastest" | "strongest"
	MaxLatencyMs     *int   // nil = use gateway default
	PreferProviders  string
	ExcludeProviders string
	Cacheable        bool
}

// EmbedRequest mirrors the TypeScript embed() input struct.
type EmbedRequest struct {
	Model          string
	FallbackModels []string
	Text           string
	TimeoutMs      int
}

// StructuredRequest mirrors the TypeScript structured() input struct.
// The caller is responsible for unmarshalling the returned json.RawMessage
// because Go generics cannot constrain HTTP response deserialization at
// the service boundary without introducing reflection overhead.
type StructuredRequest struct {
	Namespace        string
	Model            string
	FallbackModels   []string
	Messages         any
	Schema           map[string]any
	MaxRetries       int
	TimeoutMs        int
	Strategy         string
	MaxLatencyMs     *int
	PreferProviders  string
	ExcludeProviders string
}

func (s *GatewayService) Chat(ctx context.Context, req ChatRequest) (string, error) {
	strategy := coalesce(req.Strategy, "fastest")
	if req.Cacheable && req.Namespace != "" {
		if hit, ok := s.cache.Get(ctx, req.Namespace, req.Model, req.Messages, nil,
			strategy, req.PreferProviders, req.ExcludeProviders); ok {
			s.log.Info("SMLGateway cache hit", "model", req.Model, "namespace", req.Namespace)
			return hit, nil
		}
	}

	models := unique(append([]string{req.Model}, req.FallbackModels...))
	var errs []string
	for _, model := range models {
		content, err := s.chatOnce(ctx, model, req)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %s", model, truncate(err.Error(), 300)))
			s.log.Warn("SMLGateway chat model failed", "model", model, "err", err)
			continue
		}
		if looksWrongLanguage(content) {
			errs = append(errs, fmt.Sprintf("%s: wrong language response", model))
			s.log.Warn("SMLGateway chat model failed", "model", model, "err", "wrong language response")
			continue
		}
		if model != req.Model {
			s.log.Warn("SMLGateway chat fallback model used", "primaryModel", req.Model, "model", model)
		}
		if req.Cacheable && req.Namespace != "" {
			s.cache.Set(ctx, req.Namespace, req.Model, req.Messages, nil,
				strategy, req.PreferProviders, req.ExcludeProviders, content)
		}
		return content, nil
	}
	return "", fmt.Errorf("SMLGateway chat exhausted: %s", strings.Join(errs, " | "))
}

func (s *GatewayService) Embed(ctx context.Context, req EmbedRequest) ([]float64, error) {
	models := unique(append([]string{req.Model}, req.FallbackModels...))
	var errs []string
	for _, model := range models {
		vec, err := s.embedOnce(ctx, model, req)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %s", model, truncate(err.Error(), 200)))
			continue
		}
		if model != req.Model {
			s.log.Warn("SMLGateway embed fallback used", "primary", req.Model, "used", model)
		}
		return vec, nil
	}
	return nil, fmt.Errorf("SMLGateway embed exhausted: %s", strings.Join(errs, " | "))
}

// Structured attempts POST /structured first. On failure it falls back to
// Chat with the JSON schema embedded in the system prompt.
// Returns raw JSON so the caller controls deserialization.
func (s *GatewayService) Structured(ctx context.Context, req StructuredRequest) (json.RawMessage, error) {
	maxRetries := req.MaxRetries
	if maxRetries == 0 {
		maxRetries = 2
	}
	body := map[string]any{
		"model":       req.Model,
		"messages":    req.Messages,
		"schema":      req.Schema,
		"max_retries": maxRetries,
	}
	resp, bodyText, err := s.doRequest(ctx, "/structured", body, req.TimeoutMs,
		req.Strategy, req.MaxLatencyMs, req.PreferProviders, req.ExcludeProviders, "")

	if err == nil && resp.StatusCode < 300 {
		var envelope struct {
			Ok   bool            `json:"ok"`
			Data json.RawMessage `json:"data"`
		}
		if json.Unmarshal([]byte(bodyText), &envelope) == nil && envelope.Ok && envelope.Data != nil {
			return envelope.Data, nil
		}
	}

	logPayload := []any{"status", 0, "body", truncate(bodyText, 500)}
	if resp != nil {
		logPayload[1] = resp.StatusCode
	}
	// 401 from gateway means the structured endpoint is not provisioned; info only.
	if strings.Contains(bodyText, "HTTP 401 from gateway") {
		s.log.Info("SMLGateway structured unavailable, falling back to chat JSON", logPayload...)
	} else {
		s.log.Warn("SMLGateway structured failed, falling back to chat JSON", logPayload...)
	}

	return s.structuredViaChat(ctx, req)
}

func (s *GatewayService) chatOnce(ctx context.Context, model string, req ChatRequest) (string, error) {
	timeoutMs := req.TimeoutMs
	if timeoutMs == 0 {
		timeoutMs = defaultTimeoutMs
	}
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = defaultMaxTokens
	}
	body := map[string]any{
		"model":       model,
		"messages":    req.Messages,
		"temperature": 0,
		"max_tokens":  maxTokens,
	}
	if req.SessionID != "" {
		body["user"] = req.SessionID
	}
	if req.ResponseFormat != nil {
		body["response_format"] = req.ResponseFormat
	}
	resp, bodyText, err := s.doRequest(ctx, "/chat/completions", body, timeoutMs,
		req.Strategy, req.MaxLatencyMs, req.PreferProviders, req.ExcludeProviders, req.SessionID)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("SMLGateway chat failed %d: %s", resp.StatusCode, truncate(bodyText, 500))
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(bodyText), &result); err != nil {
		return "", fmt.Errorf("SMLGateway chat parse: %w", err)
	}
	if len(result.Choices) == 0 || result.Choices[0].Message.Content == "" {
		return "", fmt.Errorf("SMLGateway chat empty response")
	}
	return strings.TrimSpace(result.Choices[0].Message.Content), nil
}

func (s *GatewayService) embedOnce(ctx context.Context, model string, req EmbedRequest) ([]float64, error) {
	timeoutMs := req.TimeoutMs
	if timeoutMs == 0 {
		timeoutMs = defaultTimeoutMs
	}
	body := map[string]any{
		"model": model,
		"input": req.Text,
	}
	resp, bodyText, err := s.doRequest(ctx, "/embeddings", body, timeoutMs, "fastest", nil, "", "", "")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d %s", resp.StatusCode, truncate(bodyText, 100))
	}
	var result struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(bodyText), &result); err != nil {
		return nil, fmt.Errorf("embed parse: %w", err)
	}
	if len(result.Data) == 0 || len(result.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("empty response")
	}
	return result.Data[0].Embedding, nil
}

func (s *GatewayService) structuredViaChat(ctx context.Context, req StructuredRequest) (json.RawMessage, error) {
	schemaJSON, _ := json.Marshal(req.Schema)
	systemMsg := map[string]any{
		"role": "system",
		"content": strings.Join([]string{
			"Return only valid JSON matching this JSON schema.",
			"Do not wrap in markdown.",
			"The finalReply must be Thai unless the user explicitly asks for another language.",
			string(schemaJSON),
		}, "\n"),
	}
	msgs := prependMessage(req.Messages, systemMsg)

	chatReq := ChatRequest{
		Namespace:        req.Namespace,
		Model:            req.Model,
		FallbackModels:   req.FallbackModels,
		Messages:         msgs,
		TimeoutMs:        req.TimeoutMs,
		MaxTokens:        2000,
		Strategy:         req.Strategy,
		MaxLatencyMs:     req.MaxLatencyMs,
		PreferProviders:  req.PreferProviders,
		ExcludeProviders: req.ExcludeProviders,
	}

	models := unique(append([]string{req.Model}, req.FallbackModels...))
	var errs []string
	for _, model := range models {
		chatReq.Model = model
		content, err := s.chatOnce(ctx, model, chatReq)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %s", model, truncate(err.Error(), 300)))
			s.log.Warn("SMLGateway structured fallback model failed", "model", model, "err", err)
			continue
		}
		parsed, ok := parseLooseJSON(extractJSON(content))
		if !ok {
			errs = append(errs, fmt.Sprintf("%s: invalid JSON: %s", model, truncate(content, 300)))
			s.log.Warn("SMLGateway structured fallback model failed", "model", model, "err", "invalid JSON")
			continue
		}
		if hasNonThaiFinalReply(parsed) {
			errs = append(errs, fmt.Sprintf("%s: non-Thai finalReply", model))
			s.log.Warn("SMLGateway structured fallback model failed", "model", model, "err", "non-Thai finalReply")
			continue
		}
		if model != req.Model {
			s.log.Warn("SMLGateway structured fallback model used", "primaryModel", req.Model, "model", model)
		}
		return parsed, nil
	}
	return nil, fmt.Errorf("SMLGateway structured fallback exhausted: %s", strings.Join(errs, " | "))
}

func (s *GatewayService) doRequest(
	ctx context.Context,
	path string,
	body any,
	timeoutMs int,
	strategy string,
	maxLatencyMs *int,
	prefer, exclude string,
	sessionID string,
) (*http.Response, string, error) {
	if timeoutMs <= 0 {
		timeoutMs = defaultTimeoutMs
	}
	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, "", fmt.Errorf("doRequest marshal: %w", err)
	}

	url := s.baseURL + path
	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		return nil, "", fmt.Errorf("doRequest build: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+s.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-SMLGateway-Strategy", coalesce(strategy, "fastest"))
	if maxLatencyMs != nil {
		httpReq.Header.Set("X-SMLGateway-Max-Latency", fmt.Sprintf("%d", *maxLatencyMs))
	}
	if prefer != "" {
		httpReq.Header.Set("X-SMLGateway-Prefer", prefer)
	}
	if exclude != "" {
		httpReq.Header.Set("X-SMLGateway-Exclude", exclude)
	}
	if sessionID != "" {
		httpReq.Header.Set("X-ThaiAiAgent-Session-ID", sessionID)
		httpReq.Header.Set("X-ThClaws-Session-ID", sessionID)
	}

	resp, err := s.http.Do(httpReq)
	if err != nil {
		return nil, "", fmt.Errorf("doRequest http: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp, "", fmt.Errorf("doRequest read: %w", err)
	}
	bodyText := string(raw)

	s.log.Info("SMLGateway response",
		"status", resp.StatusCode,
		"requestId", resp.Header.Get("x-smlgateway-request-id"),
		"model", resp.Header.Get("x-smlgateway-model"),
		"provider", resp.Header.Get("x-smlgateway-provider"),
		"cache", resp.Header.Get("x-smlgateway-cache"),
		"sessionId", sessionID,
	)
	return resp, bodyText, nil
}

// looksWrongLanguage returns true when the response contains Arabic script but
// almost no Thai — Arabic models occasionally slip in Arabic text even for
// Thai-language prompts.
func looksWrongLanguage(content string) bool {
	var thai, arabic int
	for _, r := range content {
		switch {
		case r >= 0x0E00 && r <= 0x0E7F:
			thai++
		case r >= 0x0600 && r <= 0x06FF:
			arabic++
		}
	}
	return arabic >= 8 && arabic > thai
}

func hasNonThaiFinalReply(raw json.RawMessage) bool {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return false
	}
	val, ok := m["finalReply"]
	if !ok {
		return false
	}
	var s string
	if err := json.Unmarshal(val, &s); err != nil {
		return false
	}
	return looksWrongLanguage(s)
}

func extractJSON(content string) string {
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start < 0 || end <= start {
		return content
	}
	return content[start : end+1]
}

// parseLooseJSON tries standard parse then a pair of known structural repairs
// that gateway models occasionally emit.
func parseLooseJSON(content string) (json.RawMessage, bool) {
	if json.Valid([]byte(content)) {
		return json.RawMessage(content), true
	}
	repaired := content
	repaired = strings.ReplaceAll(repaired, `},"actions":`, `,"actions":`)
	repaired = strings.ReplaceAll(repaired, `},"finalReply":`, `,"finalReply":`)
	if json.Valid([]byte(repaired)) {
		return json.RawMessage(repaired), true
	}
	// Last resort: synthesise a minimal object from the finalReply string value.
	if fr := extractFinalReply(content); fr != "" {
		synthetic := fmt.Sprintf(`{"finalReply":%s,"actions":[{"type":"no_op"}]}`, jsonString(fr))
		return json.RawMessage(synthetic), true
	}
	return nil, false
}

func extractFinalReply(content string) string {
	const needle = `"finalReply"`
	idx := strings.Index(content, needle)
	if idx < 0 {
		return ""
	}
	rest := content[idx+len(needle):]
	rest = strings.TrimSpace(rest)
	if !strings.HasPrefix(rest, ":") {
		return ""
	}
	rest = strings.TrimSpace(rest[1:])
	if !strings.HasPrefix(rest, `"`) {
		return ""
	}
	// Decode the JSON string value.
	decoder := json.NewDecoder(strings.NewReader(rest))
	var s string
	if err := decoder.Decode(&s); err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func prependMessage(messages any, msg map[string]any) []any {
	switch v := messages.(type) {
	case []any:
		return append([]any{msg}, v...)
	case []map[string]any:
		out := make([]any, 0, len(v)+1)
		out = append(out, msg)
		for _, m := range v {
			out = append(out, m)
		}
		return out
	default:
		return []any{msg}
	}
}

func unique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func coalesce(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n])
}
