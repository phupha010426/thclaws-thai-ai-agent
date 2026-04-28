package events

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// ─── Subject validation ───────────────────────────────────────────────────────

func TestIsNamespacedSubject_valid(t *testing.T) {
	cases := []string{
		"line.event.Uabcdef1234567890",
		"thclaws.plan.something.Uabcdef",
		"ledger.tx.Uabcdef",
		"wiki.fact.text.Uabcdef1234567890",
	}
	for _, s := range cases {
		if !isNamespacedSubject(s) {
			t.Errorf("expected %q to be a valid namespaced subject", s)
		}
	}
}

func TestIsNamespacedSubject_invalid(t *testing.T) {
	cases := []string{
		"line.event",       // only two tokens — no namespace tail
		"line.event.*",     // wildcard tail
		"line.event.>",     // fan-out tail
		"line.event.text.", // empty tail
		"nons",             // single token
		"",                 // empty string
	}
	for _, s := range cases {
		if isNamespacedSubject(s) {
			t.Errorf("expected %q to be rejected as a namespaced subject", s)
		}
	}
}

// ─── buildSubject ─────────────────────────────────────────────────────────────

func TestBuildSubject(t *testing.T) {
	cases := []struct {
		topic     string
		namespace string
		want      string
	}{
		{"line.event.text", "Uabc", "line.event.text.Uabc"},
		// topic already ends with namespace — must not double-append
		{"line.event.text.Uabc", "Uabc", "line.event.text.Uabc"},
		// topic equals namespace (edge case)
		{"Uabc", "Uabc", "Uabc"},
	}
	for _, tc := range cases {
		got := buildSubject(tc.topic, tc.namespace)
		if got != tc.want {
			t.Errorf("buildSubject(%q, %q) = %q; want %q", tc.topic, tc.namespace, got, tc.want)
		}
	}
}

// ─── Mock store + publisher ───────────────────────────────────────────────────

type mockStore struct {
	rows       []outboxRow
	published  []int64
	bumped     map[int64]string // id → lastError
}

func newMockStore(rows []outboxRow) *mockStore {
	return &mockStore{rows: rows, bumped: make(map[int64]string)}
}

func (s *mockStore) FetchPending(_ context.Context, batchSize int) ([]outboxRow, error) {
	if batchSize < len(s.rows) {
		return s.rows[:batchSize], nil
	}
	return s.rows, nil
}

func (s *mockStore) MarkPublished(_ context.Context, id int64) {
	s.published = append(s.published, id)
}

func (s *mockStore) BumpAttempts(_ context.Context, id int64, lastError string) {
	s.bumped[id] = lastError
}

type mockPublisher struct {
	published []publishCall
	failWith  error
}

type publishCall struct {
	subject string
	payload any
}

func (m *mockPublisher) Publish(_ context.Context, subject string, payload any) error {
	if m.failWith != nil {
		return m.failWith
	}
	m.published = append(m.published, publishCall{subject: subject, payload: payload})
	return nil
}

type mockMetrics struct {
	published int
	failed    int
	skipped   int
}

func (m *mockMetrics) RecordPublished(string) { m.published++ }
func (m *mockMetrics) RecordFailure(string)   { m.failed++ }
func (m *mockMetrics) RecordSkipped(string)   { m.skipped++ }

// ─── Relay drain logic ────────────────────────────────────────────────────────

func TestDrainOnce_publishesValidRows(t *testing.T) {
	store := newMockStore([]outboxRow{
		{ID: 1, Topic: "line.event.text", Payload: []byte(`{"Namespace":"Uabc","data":"hello"}`)},
	})
	pub := &mockPublisher{}
	m := &mockMetrics{}
	relay := newRelayWithStore(store, pub, newTestLogger(), m)

	n, err := relay.DrainOnce(context.Background(), 10)
	if err != nil {
		t.Fatalf("DrainOnce error: %v", err)
	}
	if n != 1 {
		t.Errorf("expected n=1; got %d", n)
	}
	if len(pub.published) != 1 {
		t.Fatalf("expected 1 publish call; got %d", len(pub.published))
	}
	if pub.published[0].subject != "line.event.text.Uabc" {
		t.Errorf("wrong subject: %q", pub.published[0].subject)
	}
	if len(store.published) != 1 || store.published[0] != 1 {
		t.Errorf("expected row 1 marked published; got %v", store.published)
	}
	if m.published != 1 || m.skipped != 0 || m.failed != 0 {
		t.Errorf("metrics mismatch: published=%d skipped=%d failed=%d", m.published, m.skipped, m.failed)
	}
}

func TestDrainOnce_rejectsRowMissingNamespace(t *testing.T) {
	store := newMockStore([]outboxRow{
		{ID: 2, Topic: "line.event.text", Payload: []byte(`{"data":"no namespace here"}`)},
	})
	pub := &mockPublisher{}
	m := &mockMetrics{}
	relay := newRelayWithStore(store, pub, newTestLogger(), m)

	relay.DrainOnce(context.Background(), 10)

	if len(pub.published) != 0 {
		t.Errorf("expected 0 publish calls; got %d", len(pub.published))
	}
	if _, bumped := store.bumped[2]; !bumped {
		t.Errorf("expected row 2 to have attempts bumped")
	}
	if m.skipped != 1 {
		t.Errorf("expected metrics.skipped=1; got %d", m.skipped)
	}
}

func TestDrainOnce_rejectsBadJSON(t *testing.T) {
	store := newMockStore([]outboxRow{
		{ID: 3, Topic: "line.event.text", Payload: []byte(`not-json`)},
	})
	pub := &mockPublisher{}
	m := &mockMetrics{}
	relay := newRelayWithStore(store, pub, newTestLogger(), m)

	relay.DrainOnce(context.Background(), 10)

	if len(pub.published) != 0 {
		t.Errorf("expected 0 publish calls; got %d", len(pub.published))
	}
	if m.skipped != 1 {
		t.Errorf("expected metrics.skipped=1 for bad JSON; got %d", m.skipped)
	}
}

func TestDrainOnce_natsFailureBumpsAttempts(t *testing.T) {
	store := newMockStore([]outboxRow{
		{ID: 4, Topic: "line.event.text", Payload: []byte(`{"Namespace":"Udef","data":"hi"}`)},
	})
	pub := &mockPublisher{failWith: errors.New("nats unavailable")}
	m := &mockMetrics{}
	relay := newRelayWithStore(store, pub, newTestLogger(), m)

	relay.DrainOnce(context.Background(), 10)

	if m.failed != 1 {
		t.Errorf("expected metrics.failed=1; got %d", m.failed)
	}
	if m.published != 0 {
		t.Errorf("expected metrics.published=0; got %d", m.published)
	}
	if _, bumped := store.bumped[4]; !bumped {
		t.Errorf("expected row 4 to have attempts bumped on NATS failure")
	}
	if len(store.published) != 0 {
		t.Errorf("expected row NOT marked published on NATS failure")
	}
}

func TestDrainOnce_topicAlreadyHasNamespaceSuffix(t *testing.T) {
	store := newMockStore([]outboxRow{
		{ID: 5, Topic: "line.event.text.Ughi", Payload: []byte(`{"Namespace":"Ughi"}`)},
	})
	pub := &mockPublisher{}
	relay := newRelayWithStore(store, pub, newTestLogger(), nil)

	relay.DrainOnce(context.Background(), 10)

	if len(pub.published) != 1 {
		t.Fatalf("expected 1 publish call; got %d", len(pub.published))
	}
	if pub.published[0].subject != "line.event.text.Ughi" {
		t.Errorf("double-append detected: got subject %q", pub.published[0].subject)
	}
}

// ─── Publisher subject validation (unit, no NATS) ────────────────────────────

// TestPublisherRefusesWildcardSubject exercises only the subject-guard path in
// Publisher.Publish; the NATS call is never reached because the guard fires first.
func TestPublisherRefusesWildcardSubject(t *testing.T) {
	p := &Publisher{logger: newTestLogger()}
	err := p.Publish(context.Background(), "line.event.>", map[string]any{})
	if err == nil {
		t.Fatal("expected error for wildcard subject; got nil")
	}
}

func TestPublisherRefutesTwoTokenSubject(t *testing.T) {
	p := &Publisher{logger: newTestLogger()}
	err := p.Publish(context.Background(), "line.event", map[string]any{})
	if err == nil {
		t.Fatal("expected error for two-token subject; got nil")
	}
}
