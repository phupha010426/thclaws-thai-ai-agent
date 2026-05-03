package ledger_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/thaiaiagent/go-core/internal/ledger"
)

// TestCreateBusinessTransaction_RequiresUserIDAndBusinessID verifies that
// empty userID or businessID is rejected before any DB operation.
func TestCreateBusinessTransaction_RequiresUserIDAndBusinessID(t *testing.T) {
	t.Parallel()

	w := ledger.NewWriter(nil) // nil pool intentional — panics if it reaches DB
	input := ledger.BusinessTxInput{
		Type:   "EXPENSE",
		Amount: 100,
	}

	_, err := ledger.CreateBusinessTransaction(context.Background(), w, "", "b1", input)
	if err == nil {
		t.Fatal("expected error for empty userID, got nil")
	}

	_, err = ledger.CreateBusinessTransaction(context.Background(), w, "u1", "", input)
	if err == nil {
		t.Fatal("expected error for empty businessID, got nil")
	}
}

// TestCreateBusinessTransaction_RejectsInvalidType verifies that
// types other than INCOME/EXPENSE are rejected.
func TestCreateBusinessTransaction_RejectsInvalidType(t *testing.T) {
	t.Parallel()

	w := ledger.NewWriter(nil)
	input := ledger.BusinessTxInput{
		Type:   "TRANSFER",
		Amount: 100,
	}

	_, err := ledger.CreateBusinessTransaction(context.Background(), w, "u1", "b1", input)
	if err == nil {
		t.Fatal("expected error for invalid type TRANSFER, got nil")
	}
}

// TestRecordBusinessSale_CalculatesAmount verifies that when amount is 0,
// it is calculated as quantity × unitPrice. Since this reaches the DB,
// we test it in the integration test below.
func TestRecordBusinessSale_CalculatesAmount(t *testing.T) {
	// Input validation happens inside CreateBusinessTransaction.
	// With a nil pool, this will panic once it tries to verify owner.
	// So we test the calculation logic indirectly via integration test.
	t.Skip("calculation logic tested in integration test")
}

// TestFetchBusinessRange_RequiresBusinessID verifies that an empty
// businessID is rejected.
func TestFetchBusinessRange_RequiresBusinessID(t *testing.T) {
	t.Parallel()

	w := ledger.NewWriter(nil)
	_, err := ledger.FetchBusinessRange(context.Background(), w, "", "2024-01-01T00:00:00Z", "2024-12-31T23:59:59Z")
	if err == nil {
		t.Fatal("expected error for empty businessID, got nil")
	}
}

// TestComputeBusinessProfit_RequiresBusinessID verifies that an empty
// businessID is rejected.
func TestComputeBusinessProfit_RequiresBusinessID(t *testing.T) {
	t.Parallel()

	w := ledger.NewWriter(nil)
	_, err := ledger.ComputeBusinessProfit(context.Background(), w, "", "2024-01-01T00:00:00Z", "2024-12-31T23:59:59Z")
	if err == nil {
		t.Fatal("expected error for empty businessID, got nil")
	}
}

// TestBusiness_Integration runs against a real Postgres when
// POSTGRES_TEST_URL is set. It verifies:
//   - CreateBusinessTransaction verifies business owner
//   - RecordBusinessSale inserts a sale with correct category
//   - RecordBusinessPurchase inserts a purchase with correct category
//   - FetchBusinessRange returns the inserted rows
//   - ComputeBusinessProfit computes correct P&L
func TestBusiness_Integration(t *testing.T) {
	dsn := os.Getenv("POSTGRES_TEST_URL")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_URL not set; skipping integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	w := ledger.NewWriter(pool)

	// Use unique IDs to avoid collisions across test runs.
	ts := time.Now().UTC()
	userID := "u-biz-test-" + ts.Format("150405")
	businessID := "biz-" + ts.Format("20060102150405")

	// Seed a business with the test user as owner.
	_, err = pool.Exec(ctx, `
		INSERT INTO businesses (id, "ownerId", name, type, "createdAt")
		VALUES ($1, $2, 'Test Business', 'RETAIL', NOW())
		ON CONFLICT DO NOTHING`, businessID, userID)
	if err != nil {
		t.Fatalf("seed business: %v", err)
	}

	// --- Verify that non-owner is rejected ---
	input := ledger.BusinessTxInput{
		Type:   "EXPENSE",
		Amount: 100,
	}
	_, err = ledger.CreateBusinessTransaction(ctx, w, "another-user", businessID, input)
	if err == nil {
		t.Fatal("expected error for non-owner user, got nil")
	}

	// --- Record a business sale ---
	saleHappened := time.Now().UTC().Truncate(time.Millisecond)
	saleID, err := ledger.RecordBusinessSale(ctx, w, userID, businessID, "น้ำอัดลม", 0, 10, 25.50)
	if err != nil {
		t.Fatalf("RecordBusinessSale: %v", err)
	}
	if saleID == "" {
		t.Fatal("expected non-empty transaction id from sale")
	}

	// --- Record a business purchase ---
	purchaseHappened := saleHappened.Add(time.Minute)
	purchaseID, err := ledger.RecordBusinessPurchase(ctx, w, userID, businessID, "น้ำอัดลม(ลัง)", 0, 5, 120)
	if err != nil {
		t.Fatalf("RecordBusinessPurchase: %v", err)
	}
	if purchaseID == "" {
		t.Fatal("expected non-empty transaction id from purchase")
	}

	// --- Record a general business expense ---
	expenseInput := ledger.BusinessTxInput{
		Type:       "EXPENSE",
		Amount:     200,
		Category:   "อื่น ๆ",
		Note:       "ค่าเช่าแผง",
		HappenedAt: purchaseHappened.Add(time.Minute),
	}
	expenseID, err := ledger.CreateBusinessTransaction(ctx, w, userID, businessID, expenseInput)
	if err != nil {
		t.Fatalf("CreateBusinessTransaction (expense): %v", err)
	}
	if expenseID == "" {
		t.Fatal("expected non-empty transaction id from expense")
	}

	// --- Verify transaction rows have correct scope ---
	for _, test := range []struct {
		txID        string
		wantScope   string
		wantScopeID string
		wantType    string
		wantCat     string
		wantAmount  float64
	}{
		{saleID, "BUSINESS", businessID, "INCOME", "รายได้จากการขาย", 255},          // 10 × 25.50
		{purchaseID, "BUSINESS", businessID, "EXPENSE", "ต้นทุนสินค้า", 600},         // 5 × 120
		{expenseID, "BUSINESS", businessID, "EXPENSE", "อื่น ๆ", 200},
	} {
		var scope, scopeID, txType, cat string
		var amt float64
		err = pool.QueryRow(ctx, `
			SELECT scope, "scopeId", type, category, amount::float8
			FROM transactions WHERE id = $1`, test.txID,
		).Scan(&scope, &scopeID, &txType, &cat, &amt)
		if err != nil {
			t.Errorf("query transaction %s: %v", test.txID, err)
			continue
		}
		if scope != test.wantScope {
			t.Errorf("transaction %s: scope = %q, want %q", test.txID, scope, test.wantScope)
		}
		if scopeID != test.wantScopeID {
			t.Errorf("transaction %s: scopeId = %q, want %q", test.txID, scopeID, test.wantScopeID)
		}
		if txType != test.wantType {
			t.Errorf("transaction %s: type = %q, want %q", test.txID, txType, test.wantType)
		}
		if cat != test.wantCat {
			t.Errorf("transaction %s: category = %q, want %q", test.txID, cat, test.wantCat)
		}
		if amt != test.wantAmount {
			t.Errorf("transaction %s: amount = %.2f, want %.2f", test.txID, amt, test.wantAmount)
		}
	}

	// --- Fetch business range ---
	from := saleHappened.Add(-time.Hour).UTC().Format(time.RFC3339)
	to := expenseInput.HappenedAt.Add(time.Hour).UTC().Format(time.RFC3339)

	rows, err := ledger.FetchBusinessRange(ctx, w, businessID, from, to)
	if err != nil {
		t.Fatalf("FetchBusinessRange: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows from FetchBusinessRange, got %d", len(rows))
	}

	// --- Business profit summary ---
	summary, err := ledger.ComputeBusinessProfit(ctx, w, businessID, from, to)
	if err != nil {
		t.Fatalf("ComputeBusinessProfit: %v", err)
	}
	if summary.TotalSales != 255 {
		t.Errorf("TotalSales = %.2f, want 255", summary.TotalSales)
	}
	if summary.TotalPurchases != 600 {
		t.Errorf("TotalPurchases = %.2f, want 600", summary.TotalPurchases)
	}
	if summary.TotalExpenses != 800 { // 600 (purchase) + 200 (rent)
		t.Errorf("TotalExpenses = %.2f, want 800", summary.TotalExpenses)
	}
	if summary.GrossProfit != -345 { // 255 - 600
		t.Errorf("GrossProfit = %.2f, want -345", summary.GrossProfit)
	}
	if summary.NetProfit != -545 { // 255 - 800
		t.Errorf("NetProfit = %.2f, want -545", summary.NetProfit)
	}

	// --- Cleanup ---
	_, _ = pool.Exec(ctx, `DELETE FROM ledger_events WHERE namespace = $1`, businessID)
	_, _ = pool.Exec(ctx, `DELETE FROM transaction_project_allocations WHERE namespace = $1`, businessID)
	_, _ = pool.Exec(ctx, `DELETE FROM journal_lines WHERE namespace = $1`, businessID)
	_, _ = pool.Exec(ctx, `DELETE FROM journal_entries WHERE namespace = $1`, businessID)
	_, _ = pool.Exec(ctx, `DELETE FROM transactions WHERE namespace = $1`, businessID)
	_, _ = pool.Exec(ctx, `DELETE FROM businesses WHERE id = $1`, businessID)
}
