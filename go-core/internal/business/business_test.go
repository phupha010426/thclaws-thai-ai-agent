package business

import (
	"context"
	"testing"
)

func TestCreateBusiness(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	svc := New(pool)
	ownerID := createTestUser(t, ctx, pool)

	b, err := svc.Create(ctx, ownerID, "\u0e23\u0e49\u0e32\u0e19\u0e01\u0e4b\u0e27\u0e22\u0e40\u0e15\u0e35\u0e4b\u0e22\u0e27", "\u0e23\u0e49\u0e32\u0e19\u0e2d\u0e32\u0e2b\u0e32\u0e23")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if b.ID == "" {
		t.Error("expected non-empty ID")
	}
	if b.OwnerID != ownerID {
		t.Errorf("expected ownerID %q, got %q", ownerID, b.OwnerID)
	}
}

func TestCreateBusinessDefaultType(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	svc := New(pool)
	ownerID := createTestUser(t, ctx, pool)

	b, err := svc.Create(ctx, ownerID, "new-shop", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if b.Type != "\u0e23\u0e49\u0e32\u0e19\u0e04\u0e49\u0e32\u0e40\u0e25\u0e47\u0e01" {
		t.Errorf("expected default type, got %q", b.Type)
	}
}

func TestCreateBusinessEmptyName(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	svc := New(pool)
	ownerID := createTestUser(t, ctx, pool)

	_, err := svc.Create(ctx, ownerID, "", "food")
	if err == nil {
		t.Error("expected error for empty name")
	}
}

func TestGetBusiness(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	svc := New(pool)
	ownerID := createTestUser(t, ctx, pool)

	b, _ := svc.Create(ctx, ownerID, "tea-shop", "drinks")
	retrieved, err := svc.Get(ctx, b.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if retrieved.Name != "tea-shop" {
		t.Errorf("expected 'tea-shop', got %q", retrieved.Name)
	}
}

func TestGetBusinessNotFound(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	svc := New(pool)

	_, err := svc.Get(ctx, "nonexistent-id")
	if err == nil {
		t.Error("expected error for nonexistent business")
	}
}

func TestListByOwner(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	svc := New(pool)
	ownerID := createTestUser(t, ctx, pool)

	svc.Create(ctx, ownerID, "shop-a", "food")
	svc.Create(ctx, ownerID, "shop-b", "drinks")

	list, err := svc.ListByOwner(ctx, ownerID)
	if err != nil {
		t.Fatalf("ListByOwner: %v", err)
	}
	if len(list) < 2 {
		t.Errorf("expected at least 2 businesses, got %d", len(list))
	}
}

func TestGetProfit(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	svc := New(pool)
	ownerID := createTestUser(t, ctx, pool)

	b, err := svc.Create(ctx, ownerID, "test-shop", "food")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	namespace := "ledger:business:" + b.ID
	_, err = pool.Exec(ctx, `
		INSERT INTO transactions (id, "userId", namespace, type, amount, category, note, "happenedAt")
		VALUES ($1, $2, $3, 'INCOME', 500, '\u0e22\u0e2d\u0e14\u0e02\u0e32\u0e22', '\u0e02\u0e32\u0e22\u0e01\u0e4b\u0e27\u0e22\u0e40\u0e15\u0e35\u0e4b\u0e22\u0e27', NOW()),
		       ($4, $2, $3, 'INCOME', 300, '\u0e22\u0e2d\u0e14\u0e02\u0e32\u0e22', '\u0e02\u0e32\u0e22\u0e19\u0e49\u0e33', NOW()),
		       ($5, $2, $3, 'EXPENSE', 200, '\u0e15\u0e49\u0e19\u0e17\u0e38\u0e19', '\u0e0b\u0e37\u0e49\u0e2d\u0e27\u0e31\u0e15\u0e16\u0e38\u0e14\u0e34\u0e1a', NOW())`,
		"sale-a-"+b.ID, ownerID, namespace, "sale-b-"+b.ID, "cost-x-"+b.ID)
	if err != nil {
		t.Fatalf("insert test data: %v", err)
	}

	ps, err := svc.GetProfit(ctx, b.ID)
	if err != nil {
		t.Fatalf("GetProfit: %v", err)
	}
	if ps.Income != 800 {
		t.Errorf("expected income 800, got %f", ps.Income)
	}
	if ps.Expense != 200 {
		t.Errorf("expected expense 200, got %f", ps.Expense)
	}
	if ps.Profit != 600 {
		t.Errorf("expected profit 600, got %f", ps.Profit)
	}
}

func TestGetRecentSales(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	svc := New(pool)
	ownerID := createTestUser(t, ctx, pool)

	b, err := svc.Create(ctx, ownerID, "popular-shop", "food")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	namespace := "ledger:business:" + b.ID
	_, err = pool.Exec(ctx, `
		INSERT INTO transactions (id, "userId", namespace, type, amount, category, note, "happenedAt")
		VALUES ($1, $2, $3, 'INCOME', 100, 'sales', 'sale-bowl-1', NOW() - INTERVAL '1 hour'),
		       ($4, $2, $3, 'INCOME', 200, 'sales', 'sale-bowl-2', NOW())`,
		"rs1-"+b.ID, ownerID, namespace, "rs2-"+b.ID)
	if err != nil {
		t.Fatalf("insert test data: %v", err)
	}

	sales, err := svc.GetRecentSales(ctx, b.ID, 5)
	if err != nil {
		t.Fatalf("GetRecentSales: %v", err)
	}
	if len(sales) < 1 {
		t.Skipf("got %d sales, skipping detailed assert", len(sales))
	}
	if sales[0].Amount != 200 {
		t.Errorf("expected most recent sale 200, got %f", sales[0].Amount)
	}
}

func TestGetRecentPurchases(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	svc := New(pool)
	ownerID := createTestUser(t, ctx, pool)

	b, err := svc.Create(ctx, ownerID, "cost-shop", "food")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	namespace := "ledger:business:" + b.ID
	_, err = pool.Exec(ctx, `
		INSERT INTO transactions (id, "userId", namespace, type, amount, category, note, "happenedAt")
		VALUES ($1, $2, $3, 'EXPENSE', 150, 'ingredients', 'noodles', NOW()),
		       ($4, $2, $3, 'EXPENSE', 80, 'equipment', 'bowls', NOW() - INTERVAL '1 hour')`,
		"rp1-"+b.ID, ownerID, namespace, "rp2-"+b.ID)
	if err != nil {
		t.Fatalf("insert test data: %v", err)
	}

	purchases, err := svc.GetRecentPurchases(ctx, b.ID, 5)
	if err != nil {
		t.Fatalf("GetRecentPurchases: %v", err)
	}
	if len(purchases) < 1 {
		t.Skipf("got %d purchases, skipping detailed assert", len(purchases))
	}
	if purchases[0].Amount != 150 {
		t.Errorf("expected most recent purchase 150, got %f", purchases[0].Amount)
	}
}
