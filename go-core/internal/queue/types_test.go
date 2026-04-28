package queue

import (
	"encoding/json"
	"testing"
)

// TestLineWebhookJobRoundtrip verifies that raw event bytes survive a
// marshal→unmarshal cycle without being double-encoded or truncated.
func TestLineWebhookJobRoundtrip(t *testing.T) {
	original := LineWebhookJob{
		Event:      json.RawMessage(`{"type":"message","message":{"type":"text","text":"hello"}}`),
		ReceivedAt: "2024-01-01T00:00:00Z",
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got LineWebhookJob
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(got.Event) != string(original.Event) {
		t.Errorf("Event mismatch: got %s want %s", got.Event, original.Event)
	}
	if got.ReceivedAt != original.ReceivedAt {
		t.Errorf("ReceivedAt mismatch: got %s want %s", got.ReceivedAt, original.ReceivedAt)
	}
}

// TestMemoryCompactJobRoundtrip confirms all three tenant-identity fields
// survive serialisation intact so the compactor can partition correctly.
func TestMemoryCompactJobRoundtrip(t *testing.T) {
	original := MemoryCompactJob{
		OwnerID:   "owner-123",
		AgentID:   "agent-456",
		Namespace: "Uabcdef1234567890",
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got MemoryCompactJob
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != original {
		t.Errorf("roundtrip mismatch: got %+v want %+v", got, original)
	}
}

// TestBrainExtractJobRoundtrip also checks the optional CausationID field.
func TestBrainExtractJobRoundtrip(t *testing.T) {
	cases := []BrainExtractJob{
		{OwnerID: "o1", AgentID: "a1", Namespace: "ns1", Text: "some text", CausationID: "msg-789"},
		{OwnerID: "o2", AgentID: "a2", Namespace: "ns2", Text: "other text"}, // CausationID omitted
	}
	for _, original := range cases {
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var got BrainExtractJob
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got != original {
			t.Errorf("roundtrip mismatch: got %+v want %+v", got, original)
		}
	}
}

// TestDailyReflectionJobNamespaceOmitted verifies that an empty Namespace does
// not produce a JSON key so the handler's "== empty → reflect all" branch works.
func TestDailyReflectionJobNamespaceOmitted(t *testing.T) {
	job := DailyReflectionJob{}
	data, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// The omitempty tag should suppress the Namespace key entirely.
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	if _, ok := m["Namespace"]; ok {
		t.Errorf("expected Namespace to be omitted when empty; got JSON: %s", data)
	}
}

// TestDailyReflectionJobWithNamespace checks that a non-empty Namespace
// survives the round-trip so namespace-scoped cron triggers work.
func TestDailyReflectionJobWithNamespace(t *testing.T) {
	original := DailyReflectionJob{Namespace: "Uabcdef"}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got DailyReflectionJob
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Namespace != original.Namespace {
		t.Errorf("Namespace mismatch: got %q want %q", got.Namespace, original.Namespace)
	}
}
