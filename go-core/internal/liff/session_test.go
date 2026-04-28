package liff

import (
	"testing"
	"time"

	"github.com/thaiaiagent/go-core/internal/users"
)

func TestSessionSignVerify(t *testing.T) {
	now := time.Date(2026, 4, 28, 9, 0, 0, 0, time.UTC)
	token, err := Sign(users.Identity{
		UserID:    "user-a",
		AgentID:   "agent-a",
		Namespace: "memory:user:user-a",
	}, "secret", now)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	got, err := Verify(token, "secret", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if got.UserID != "user-a" || got.AgentID != "agent-a" || got.Namespace != "memory:user:user-a" {
		t.Fatalf("unexpected session: %#v", got)
	}
}

func TestSessionRejectsTamper(t *testing.T) {
	now := time.Date(2026, 4, 28, 9, 0, 0, 0, time.UTC)
	token, err := Sign(users.Identity{UserID: "u1", AgentID: "a1", Namespace: "memory:user:u1"}, "secret", now)
	if err != nil {
		t.Fatal(err)
	}
	bytes := []byte(token)
	if bytes[0] == 'A' {
		bytes[0] = 'B'
	} else {
		bytes[0] = 'A'
	}
	tampered := string(bytes)
	if _, err := Verify(tampered, "secret", now); err == nil {
		t.Fatal("Verify() accepted tampered token")
	}
	if _, err := Verify(token, "wrong-secret", now); err == nil {
		t.Fatal("Verify() accepted wrong secret")
	}
}

func TestSessionExpires(t *testing.T) {
	now := time.Date(2026, 4, 28, 9, 0, 0, 0, time.UTC)
	token, err := Sign(users.Identity{UserID: "u1", AgentID: "a1", Namespace: "memory:user:u1"}, "secret", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(token, "secret", now.Add(11*time.Minute)); err == nil {
		t.Fatal("Verify() accepted expired token")
	}
}

func TestDeriveChannelID(t *testing.T) {
	if got := deriveChannelID("2001234567-AbCdEfGh"); got != "2001234567" {
		t.Fatalf("deriveChannelID() = %q", got)
	}
	if got := deriveChannelID("not-a-liff-id"); got != "" {
		t.Fatalf("deriveChannelID() should reject non numeric prefix, got %q", got)
	}
}
