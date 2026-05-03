package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// BusinessTxInput captures a business transaction line.
type BusinessTxInput struct {
	Type       string    // INCOME or EXPENSE
	Amount     float64
	Quantity   float64
	UnitPrice  float64
	Category   string
	Note       string
	HappenedAt time.Time
}

// BusinessProfitSummary holds the profit summary for a business.
type BusinessProfitSummary struct {
	TotalSales     float64
	TotalPurchases float64
	TotalExpenses  float64
	GrossProfit    float64 // sales - purchases
	NetProfit      float64 // sales - purchases - expenses
}

// CreateBusinessTransaction inserts a transaction scoped to a business.
// It follows the same pattern as CreatePersonalTransaction but sets
// scope='BUSINESS' and scopeId to the given businessID.
//
// The userID is verified to be the business owner via the businesses table.
// All side effects (transaction row, journal entry, ledger_event)
// are committed in a single DB transaction so the write is atomic.
func CreateBusinessTransaction(ctx context.Context, w *Writer, userID, businessID string, input BusinessTxInput) (txID string, err error) {
	if userID == "" || businessID == "" {
		return "", fmt.Errorf("ledger business writer: userID and businessID required")
	}
	if input.Type != "INCOME" && input.Type != "EXPENSE" {
		return "", fmt.Errorf("ledger business writer: type must be INCOME or EXPENSE")
	}
	if input.HappenedAt.IsZero() {
		input.HappenedAt = time.Now().UTC()
	}

	// Verify user is business owner.
	var ownerID string
	err = w.pool.QueryRow(ctx, `SELECT "ownerId" FROM businesses WHERE id = $1`, businessID).Scan(&ownerID)
	if err != nil {
		return "", fmt.Errorf("ledger business writer: business not found: %w", err)
	}
	if ownerID != userID {
		return "", fmt.Errorf("ledger business writer: user is not the business owner")
	}

	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("ledger business writer: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Insert transaction with scope='BUSINESS'.
	if err := tx.QueryRow(ctx, `
		INSERT INTO transactions (id, "userId", namespace, scope, "scopeId", type, amount, currency, category, note, source, confidence, "happenedAt", "createdAt", "updatedAt")
		VALUES (substr(replace(gen_random_uuid()::text,'-',''),1,25), $1, $2, 'BUSINESS', $3, $4, $5::numeric, 'THB', $6, $7, 'line', 1, $8, NOW(), NOW())
		RETURNING id`,
		userID, businessID, businessID, input.Type, input.Amount, input.Category, nullableString(strings.TrimSpace(input.Note)), input.HappenedAt,
	).Scan(&txID); err != nil {
		return "", fmt.Errorf("ledger business writer: insert transaction: %w", err)
	}

	// Create journal entry.
	writeIn := WriteTxInput{
		UserID:     userID,
		Namespace:  businessID,
		Type:       input.Type,
		Amount:     input.Amount,
		Category:   input.Category,
		Note:       input.Note,
		Confidence: 1,
		HappenedAt: input.HappenedAt,
	}
	journalID, err := w.createJournalEntry(ctx, tx, writeIn, txID)
	if err != nil {
		return "", err
	}

	// Insert allocations (empty is fine — validateAllocations skips nil).
	if err := validateAllocations(ctx, tx, userID, businessID, input.Amount, nil); err != nil {
		return "", err
	}
	if err := insertAllocations(ctx, tx, userID, businessID, txID, input.Amount, nil); err != nil {
		return "", err
	}

	// Insert ledger event.
	payloadBytes, err := json.Marshal(map[string]any{
		"transactionId":  txID,
		"journalEntryId": journalID,
		"type":           input.Type,
		"amount":         input.Amount,
		"category":       input.Category,
		"note":           input.Note,
		"happenedAt":     input.HappenedAt,
		"businessId":     businessID,
	})
	if err != nil {
		return "", fmt.Errorf("ledger business writer: marshal event payload: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO ledger_events (namespace, "userId", type, payload, "occurredAt")
		VALUES ($1, $2, 'tx.recorded', $3::jsonb, $4)`,
		businessID, userID, string(payloadBytes), input.HappenedAt,
	); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return "", fmt.Errorf("ledger business writer: duplicate event: %w", err)
		}
		return "", fmt.Errorf("ledger business writer: insert event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("ledger business writer: commit: %w", err)
	}
	return txID, nil
}

// RecordBusinessSale is a convenience wrapper that records a sale (INCOME)
// for a business product. If amount is 0, it is calculated as quantity × unitPrice.
func RecordBusinessSale(ctx context.Context, w *Writer, userID, businessID, product string, amount float64, quantity float64, unitPrice float64) (string, error) {
	if amount == 0 {
		amount = quantity * unitPrice
	}
	return CreateBusinessTransaction(ctx, w, userID, businessID, BusinessTxInput{
		Type:      "INCOME",
		Amount:    amount,
		Quantity:  quantity,
		UnitPrice: unitPrice,
		Category:  "รายได้จากการขาย",
		Note:      product,
	})
}

// RecordBusinessPurchase is a convenience wrapper that records a purchase (EXPENSE)
// for a business item. If amount is 0, it is calculated as quantity × unitCost.
func RecordBusinessPurchase(ctx context.Context, w *Writer, userID, businessID, item string, amount float64, quantity float64, unitCost float64) (string, error) {
	if amount == 0 {
		amount = quantity * unitCost
	}
	return CreateBusinessTransaction(ctx, w, userID, businessID, BusinessTxInput{
		Type:      "EXPENSE",
		Amount:    amount,
		Quantity:  quantity,
		UnitPrice: unitCost,
		Category:  "ต้นทุนสินค้า",
		Note:      item,
	})
}

// FetchBusinessRange returns business transaction rows for a business
// in the given time range [from, to]. Filters by scope='BUSINESS' and scopeId.
func FetchBusinessRange(ctx context.Context, w *Writer, businessID string, from, to string) ([]SummaryRow, error) {
	if businessID == "" {
		return nil, fmt.Errorf("ledger business reader: businessID required")
	}

	rows, err := w.pool.Query(ctx, `
		SELECT type, amount, category, COALESCE(note, ''), "happenedAt"
		FROM transactions
		WHERE scope = 'BUSINESS' AND "scopeId" = $1 AND "deletedAt" IS NULL
		  AND "happenedAt" BETWEEN $2::timestamptz AND $3::timestamptz
		ORDER BY "happenedAt" DESC`, businessID, from, to)
	if err != nil {
		return nil, fmt.Errorf("ledger business reader: query: %w", err)
	}
	defer rows.Close()

	var out []SummaryRow
	for rows.Next() {
		var r SummaryRow
		var amt pgtype.Numeric
		if err := rows.Scan(&r.Type, &amt, &r.Category, &r.Note, &r.HappenedAt); err != nil {
			return nil, err
		}
		f, err := amt.Float64Value()
		if err == nil && f.Valid {
			r.Amount = f.Float64
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ComputeBusinessProfit computes a P&L summary for a business in the
// given time range [from, to]. It separates sales (INCOME), cost of goods
// sold (EXPENSE with category "ต้นทุนสินค้า"), and total expenses.
func ComputeBusinessProfit(ctx context.Context, w *Writer, businessID string, from, to string) (BusinessProfitSummary, error) {
	if businessID == "" {
		return BusinessProfitSummary{}, fmt.Errorf("ledger business: businessID required")
	}

	var s BusinessProfitSummary
	err := w.pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN type = 'INCOME' THEN amount ELSE 0 END), 0)::float8,
			COALESCE(SUM(CASE WHEN type = 'EXPENSE' AND category = 'ต้นทุนสินค้า' THEN amount ELSE 0 END), 0)::float8,
			COALESCE(SUM(CASE WHEN type = 'EXPENSE' THEN amount ELSE 0 END), 0)::float8
		FROM transactions
		WHERE scope = 'BUSINESS' AND "scopeId" = $1 AND "deletedAt" IS NULL
		  AND "happenedAt" BETWEEN $2::timestamptz AND $3::timestamptz`,
		businessID, from, to,
	).Scan(&s.TotalSales, &s.TotalPurchases, &s.TotalExpenses)
	if err != nil {
		return BusinessProfitSummary{}, fmt.Errorf("ledger business: profit summary: %w", err)
	}

	s.GrossProfit = s.TotalSales - s.TotalPurchases
	s.NetProfit = s.TotalSales - s.TotalExpenses
	return s, nil
}
