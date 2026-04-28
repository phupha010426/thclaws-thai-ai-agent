package agents

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
)

// stubCache records calls so tests can assert cache behaviour without Redis.
type stubCache struct {
	store map[string]string
	hits  atomic.Int32
	sets  atomic.Int32
}

func newStubCache() *stubCache {
	return &stubCache{store: make(map[string]string)}
}

func (c *stubCache) key(namespace, model string) string {
	return namespace + "|" + model
}

func (c *stubCache) Get(_ context.Context, namespace, model string, _, _ any, _, _, _ string) (string, bool) {
	v, ok := c.store[c.key(namespace, model)]
	if ok {
		c.hits.Add(1)
	}
	return v, ok
}

func (c *stubCache) Set(_ context.Context, namespace, model string, _, _ any, _, _, _, value string) {
	c.store[c.key(namespace, model)] = value
	c.sets.Add(1)
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func chatResponse(content string) []byte {
	b, _ := json.Marshal(map[string]any{
		"choices": []any{
			map[string]any{"message": map[string]any{"content": content}},
		},
	})
	return b
}

func TestChat_CacheHitSkipsHTTP(t *testing.T) {
	httpCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpCalls++
		w.Write(chatResponse("from-server"))
	}))
	defer srv.Close()

	c := newStubCache()
	c.store["ns1|gpt-4o"] = "cached-answer"

	gw := New(srv.Client(), srv.URL, "key", c, testLogger())
	result, err := gw.Chat(context.Background(), ChatRequest{
		Namespace: "ns1",
		Model:     "gpt-4o",
		Messages:  []any{},
		Cacheable: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "cached-answer" {
		t.Errorf("expected cached-answer, got %q", result)
	}
	if httpCalls != 0 {
		t.Errorf("expected 0 HTTP calls on cache hit, got %d", httpCalls)
	}
}

func TestChat_CacheMissCallsServerAndCaches(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(chatResponse("สวัสดีครับ"))
	}))
	defer srv.Close()

	c := newStubCache()
	gw := New(srv.Client(), srv.URL, "key", c, testLogger())

	result, err := gw.Chat(context.Background(), ChatRequest{
		Namespace: "ns2",
		Model:     "gpt-4o",
		Messages:  []any{},
		Cacheable: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "สวัสดีครับ" {
		t.Errorf("unexpected result: %q", result)
	}
	if c.sets.Load() != 1 {
		t.Errorf("expected 1 cache set, got %d", c.sets.Load())
	}
}

func TestChat_FallbackModelOnFirstFailure(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] == "bad-model" {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("error"))
			return
		}
		w.Write(chatResponse("fallback-ok"))
	}))
	defer srv.Close()

	gw := New(srv.Client(), srv.URL, "key", newStubCache(), testLogger())
	result, err := gw.Chat(context.Background(), ChatRequest{
		Model:          "bad-model",
		FallbackModels: []string{"good-model"},
		Messages:       []any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "fallback-ok" {
		t.Errorf("expected fallback-ok, got %q", result)
	}
	if callCount != 2 {
		t.Errorf("expected 2 HTTP calls, got %d", callCount)
	}
}

func TestChat_SendsSessionIDToGateway(t *testing.T) {
	var gotUser string
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotUser, _ = body["user"].(string)
		gotHeader = r.Header.Get("X-ThClaws-Session-ID")
		w.Write(chatResponse("สวัสดีครับ"))
	}))
	defer srv.Close()

	gw := New(srv.Client(), srv.URL, "key", newStubCache(), testLogger())
	result, err := gw.Chat(context.Background(), ChatRequest{
		Namespace: "ns-session",
		SessionID: "sess-123",
		Model:     "sml/auto",
		Messages:  []any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "สวัสดีครับ" {
		t.Fatalf("unexpected result: %q", result)
	}
	if gotUser != "sess-123" {
		t.Fatalf("expected OpenAI user=sess-123, got %q", gotUser)
	}
	if gotHeader != "sess-123" {
		t.Fatalf("expected session header sess-123, got %q", gotHeader)
	}
}

func TestChat_MissingNamespaceBypassesCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(chatResponse("direct"))
	}))
	defer srv.Close()

	c := newStubCache()
	// Seed a value that would match if namespace were used — it must NOT be returned.
	c.store["|gpt-4o"] = "should-not-be-returned"

	gw := New(srv.Client(), srv.URL, "key", c, testLogger())
	result, err := gw.Chat(context.Background(), ChatRequest{
		Namespace: "", // no namespace
		Model:     "gpt-4o",
		Messages:  []any{},
		Cacheable: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "direct" {
		t.Errorf("expected direct from server, got %q", result)
	}
	if c.hits.Load() != 0 {
		t.Errorf("cache should not have been read without namespace")
	}
	if c.sets.Load() != 0 {
		t.Errorf("cache should not have been written without namespace")
	}
}

func TestEmbed_ReturnsVector(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := json.Marshal(map[string]any{
			"data": []any{
				map[string]any{"embedding": []float64{0.1, 0.2, 0.3}},
			},
		})
		w.Write(b)
	}))
	defer srv.Close()

	gw := New(srv.Client(), srv.URL, "key", newStubCache(), testLogger())
	vec, err := gw.Embed(context.Background(), EmbedRequest{
		Model: "bge-m3",
		Text:  "hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vec) != 3 {
		t.Errorf("expected 3-dim vector, got %d", len(vec))
	}
	if vec[0] != 0.1 || vec[1] != 0.2 || vec[2] != 0.3 {
		t.Errorf("unexpected vector: %v", vec)
	}
}

func TestEmbed_FallbackOnFirstModelFailure(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] == "bad-embed" {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		b, _ := json.Marshal(map[string]any{
			"data": []any{map[string]any{"embedding": []float64{1.0, 2.0}}},
		})
		w.Write(b)
	}))
	defer srv.Close()

	gw := New(srv.Client(), srv.URL, "key", newStubCache(), testLogger())
	vec, err := gw.Embed(context.Background(), EmbedRequest{
		Model:          "bad-embed",
		FallbackModels: []string{"good-embed"},
		Text:           "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vec) != 2 {
		t.Errorf("expected 2-dim vector, got %d", len(vec))
	}
}

func TestLooksWrongLanguage(t *testing.T) {
	arabic := "مرحبا كيف حالك اليوم يا صديقي العزيز"
	if !looksWrongLanguage(arabic) {
		t.Error("expected Arabic-heavy text to trigger wrong-language")
	}
	thai := "สวัสดีครับ ผมชื่อจอห์น"
	if looksWrongLanguage(thai) {
		t.Error("expected Thai text to pass wrong-language check")
	}
	mixed := "สวัสดี مرحبا"
	if looksWrongLanguage(mixed) {
		t.Error("mixed text with more Thai than Arabic should pass")
	}
}

func TestStructured_FallsBackToChatOnStructuredFailure(t *testing.T) {
	structuredCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/structured" {
			structuredCalled = true
			w.WriteHeader(http.StatusUnprocessableEntity)
			w.Write([]byte(`{"error":"HTTP 401 from gateway"}`))
			return
		}
		// /chat/completions returns valid JSON matching the schema.
		w.Write(chatResponse(`{"finalReply":"สวัสดี","actions":[]}`))
	}))
	defer srv.Close()

	gw := New(srv.Client(), srv.URL, "key", newStubCache(), testLogger())
	raw, err := gw.Structured(context.Background(), StructuredRequest{
		Model:    "gpt-4o",
		Messages: []any{map[string]any{"role": "user", "content": "hello"}},
		Schema:   map[string]any{"type": "object"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !structuredCalled {
		t.Error("expected /structured to be called first")
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if result["finalReply"] != "สวัสดี" {
		t.Errorf("unexpected finalReply: %v", result["finalReply"])
	}
}
