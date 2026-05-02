package household

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestCreateHousehold(t *testing.T) {
	ctx := context.Background()
	svc := New(testPool(t))

	ownerID := createTestUser(t, ctx, svc)

	name := "บ้านสุขสันต์"
	hh, err := svc.Create(ctx, ownerID, name)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if hh.ID == "" {
		t.Error("expected non-empty ID")
	}
	if hh.Name != name {
		t.Errorf("expected name %q, got %q", name, hh.Name)
	}
	if hh.OwnerID != ownerID {
		t.Errorf("expected ownerID %q, got %q", ownerID, hh.OwnerID)
	}

	// Owner should be auto-added as member
	members, err := svc.ListMembers(ctx, hh.ID)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(members))
	}
	if members[0].Role != "owner" {
		t.Errorf("expected role 'owner', got %q", members[0].Role)
	}
	if members[0].UserID != ownerID {
		t.Errorf("expected member userID %q, got %q", ownerID, members[0].UserID)
	}
}

func TestCreateHouseholdEmptyName(t *testing.T) {
	ctx := context.Background()
	svc := New(testPool(t))
	ownerID := createTestUser(t, ctx, svc)

	_, err := svc.Create(ctx, ownerID, "")
	if err == nil {
		t.Error("expected error for empty name")
	}
}

func TestAddAndRemoveMember(t *testing.T) {
	ctx := context.Background()
	svc := New(testPool(t))

	ownerID := createTestUser(t, ctx, svc)
	memberID := createTestUser(t, ctx, svc)

	hh, err := svc.Create(ctx, ownerID, "บ้าน")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	m, err := svc.AddMember(ctx, hh.ID, memberID, "member", ownerID)
	if err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if m.Role != "member" {
		t.Errorf("expected 'member', got %q", m.Role)
	}

	members, err := svc.ListMembers(ctx, hh.ID)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 2 {
		t.Errorf("expected 2 members, got %d", len(members))
	}

	// Remove member
	err = svc.RemoveMember(ctx, hh.ID, memberID, ownerID)
	if err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}

	members, err = svc.ListMembers(ctx, hh.ID)
	if err != nil {
		t.Fatalf("ListMembers after remove: %v", err)
	}
	if len(members) != 1 {
		t.Errorf("expected 1 member after remove, got %d", len(members))
	}
}

func TestCannotRemoveOwner(t *testing.T) {
	ctx := context.Background()
	svc := New(testPool(t))

	ownerID := createTestUser(t, ctx, svc)

	hh, err := svc.Create(ctx, ownerID, "บ้าน")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	err = svc.RemoveMember(ctx, hh.ID, ownerID, ownerID)
	if err == nil {
		t.Error("expected error when removing owner")
	}
}

func TestNonOwnerCannotAddMember(t *testing.T) {
	ctx := context.Background()
	svc := New(testPool(t))

	ownerID := createTestUser(t, ctx, svc)
	randoID := createTestUser(t, ctx, svc)
	memberID := createTestUser(t, ctx, svc)

	hh, err := svc.Create(ctx, ownerID, "บ้าน")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = svc.AddMember(ctx, hh.ID, memberID, "member", randoID)
	if err == nil {
		t.Error("expected error when non-owner adds member")
	}
}

func TestListByOwner(t *testing.T) {
	ctx := context.Background()
	svc := New(testPool(t))

	ownerID := createTestUser(t, ctx, svc)

	hh1, err := svc.Create(ctx, ownerID, "บ้าน")
	if err != nil {
		t.Fatalf("Create 1: %v", err)
	}
	hh2, err := svc.Create(ctx, ownerID, "คอนโด")
	if err != nil {
		t.Fatalf("Create 2: %v", err)
	}

	list, err := svc.ListByOwner(ctx, ownerID)
	if err != nil {
		t.Fatalf("ListByOwner: %v", err)
	}
	if len(list) < 2 {
		t.Errorf("expected at least 2, got %d", len(list))
	}

	ids := map[string]bool{}
	for _, hh := range list {
		ids[hh.ID] = true
	}
	if !ids[hh1.ID] || !ids[hh2.ID] {
		t.Error("missing created households in list")
	}
}

func TestBelongsTo(t *testing.T) {
	ctx := context.Background()
	svc := New(testPool(t))

	ownerID := createTestUser(t, ctx, svc)
	memberID := createTestUser(t, ctx, svc)
	outsiderID := createTestUser(t, ctx, svc)

	hh, err := svc.Create(ctx, ownerID, "บ้าน")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err = svc.AddMember(ctx, hh.ID, memberID, "member", ownerID)
	if err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	ok, err := svc.BelongsTo(ctx, hh.ID, ownerID)
	if err != nil || !ok {
		t.Errorf("owner should belong: ok=%v err=%v", ok, err)
	}
	ok, err = svc.BelongsTo(ctx, hh.ID, memberID)
	if err != nil || !ok {
		t.Errorf("member should belong: ok=%v err=%v", ok, err)
	}
	ok, err = svc.BelongsTo(ctx, hh.ID, outsiderID)
	if err != nil || ok {
		t.Errorf("outsider should NOT belong: ok=%v err=%v", ok, err)
	}
}

// createTestUser inserts a minimal user row for FK constraints.
func createTestUser(t testing.TB, ctx context.Context, svc *Service) string {
	t.Helper()
	userID := uuid.NewString()
	_, err := svc.pool.Exec(ctx, `
		INSERT INTO users (id, "lineUserId", language, "createdAt", "updatedAt")
		VALUES ($1, 'test-line-'||$1, 'th', NOW(), NOW())
		ON CONFLICT (id) DO NOTHING`, userID)
	if err != nil {
		t.Fatalf("createTestUser: %v", err)
	}
	return userID
}
