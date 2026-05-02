// Package business manages small business ledgers.
// Each business is owned by a LINE user. Sales and purchases are recorded
// through the ledger.Writer with scope=BUSINESS and scopeId=businessId.
package business

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Business represents a small business (e.g. food stall, online shop).
type Business struct {
	ID        string    `json:"id"`
	OwnerID   string    `json:"ownerId"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"createdAt"`
}

// ProfitSummary contains income, expense, and profit totals.
type ProfitSummary struct {
	Income      float64 `json:"income"`
	Expense     float64 `json:"expense"`
	Profit      float64 `json:"profit"`
	IncomeText  string  `json:"incomeText"`
	ExpenseText string  `json:"expenseText"`
	ProfitText  string  `json:"profitText"`
}

// Sale represents a recorded sale transaction.
type Sale struct {
	TransactionID string  `json:"transactionId"`
	Amount        float64 `json:"amount"`
	Note          string  `json:"note"`
	HappenedAt    string  `json:"happenedAt"`
}

// Purchase represents a recorded purchase/expense.
type Purchase struct {
	TransactionID string  `json:"transactionId"`
	Amount        float64 `json:"amount"`
	Category      string  `json:"category"`
	Note          string  `json:"note"`
	HappenedAt    string  `json:"happenedAt"`
}

// Namespace returns the business namespace for ledger isolation.
func (b *Business) Namespace() string {
	return "memory:business:" + b.ID
}

// LedgerNamespace returns the ledger scope string.
func (b *Business) LedgerNamespace() string {
	return "ledger:business:" + b.ID
}

// Service handles business operations.
type Service struct {
	pool *pgxpool.Pool
}

// New creates a business Service backed by the given connection pool.
func New(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// Create creates a new business owned by the given user.
func (s *Service) Create(ctx context.Context, ownerID, name, bizType string) (*Business, error) {
	if ownerID == "" {
		return nil, fmt.Errorf("business: ownerID required")
	}
	if name == "" {
		return nil, fmt.Errorf("business: name required")
	}
	if bizType == "" {
		bizType = "ร้านค้าเล็ก"
	}

	id := uuid.NewString()
	now := time.Now().UTC()

	_, err := s.pool.Exec(ctx, `
		INSERT INTO businesses (id, "ownerId", name, type, "createdAt")
		VALUES ($1, $2, $3, $4, $5)`,
		id, ownerID, name, bizType, now)
	if err != nil {
		return nil, fmt.Errorf("business: insert: %w", err)
	}

	return &Business{ID: id, OwnerID: ownerID, Name: name, Type: bizType, CreatedAt: now}, nil
}

// Get returns a business by ID.
func (s *Service) Get(ctx context.Context, businessID string) (*Business, error) {
	if businessID == "" {
		return nil, fmt.Errorf("business: id required")
	}
	var b Business
	err := s.pool.QueryRow(ctx, `
		SELECT id, "ownerId", name, type, "createdAt"
		FROM businesses WHERE id = $1`, businessID).
		Scan(&b.ID, &b.OwnerID, &b.Name, &b.Type, &b.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("business: not found")
	}
	return &b, err
}

// ListByOwner returns all businesses owned by the given user.
func (s *Service) ListByOwner(ctx context.Context, ownerID string) ([]Business, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, "ownerId", name, type, "createdAt"
		FROM businesses WHERE "ownerId" = $1
		ORDER BY "createdAt" DESC`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("business: list: %w", err)
	}
	defer rows.Close()

	var out []Business
	for rows.Next() {
		var b Business
		if err := rows.Scan(&b.ID, &b.OwnerID, &b.Name, &b.Type, &b.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// GetProfit returns the profit summary for a business over all time.
func (s *Service) GetProfit(ctx context.Context, businessID string) (*ProfitSummary, error) {
	namespace := "ledger:business:" + businessID
	var ps ProfitSummary

	err := s.pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN type = 'INCOME' THEN amount ELSE 0 END), 0)::float8,
			COALESCE(SUM(CASE WHEN type = 'EXPENSE' THEN amount ELSE 0 END), 0)::float8
		FROM transactions
		WHERE namespace = $1 AND "deletedAt" IS NULL`, namespace).
		Scan(&ps.Income, &ps.Expense)
	if err != nil {
		return nil, fmt.Errorf("business: profit query: %w", err)
	}

	ps.Profit = ps.Income - ps.Expense
	ps.IncomeText = formatMoney(ps.Income)
	ps.ExpenseText = formatMoney(ps.Expense)
	ps.ProfitText = formatMoney(ps.Profit)
	return &ps, nil
}

// GetRecentSales returns the most recent sales for a business.
func (s *Service) GetRecentSales(ctx context.Context, businessID string, limit int) ([]Sale, error) {
	if limit <= 0 {
		limit = 10
	}
	namespace := "ledger:business:" + businessID
	rows, err := s.pool.Query(ctx, `
		SELECT id, amount::float8, COALESCE(note, ''), "happenedAt"
		FROM transactions
		WHERE namespace = $1 AND type = 'INCOME' AND "deletedAt" IS NULL
		ORDER BY "happenedAt" DESC
		LIMIT $2`, namespace, limit)
	if err != nil {
		return nil, fmt.Errorf("business: recent sales: %w", err)
	}
	defer rows.Close()

	var out []Sale
	for rows.Next() {
		var s Sale
		var happened time.Time
		if err := rows.Scan(&s.TransactionID, &s.Amount, &s.Note, &happened); err != nil {
			return nil, err
		}
		s.HappenedAt = happened.Format(time.RFC3339)
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetRecentPurchases returns the most recent purchases for a business.
func (s *Service) GetRecentPurchases(ctx context.Context, businessID string, limit int) ([]Purchase, error) {
	if limit <= 0 {
		limit = 10
	}
	namespace := "ledger:business:" + businessID
	rows, err := s.pool.Query(ctx, `
		SELECT id, amount::float8, COALESCE(category, ''), COALESCE(note, ''), "happenedAt"
		FROM transactions
		WHERE namespace = $1 AND type = 'EXPENSE' AND "deletedAt" IS NULL
		ORDER BY "happenedAt" DESC
		LIMIT $2`, namespace, limit)
	if err != nil {
		return nil, fmt.Errorf("business: recent purchases: %w", err)
	}
	defer rows.Close()

	var out []Purchase
	for rows.Next() {
		var p Purchase
		var happened time.Time
		if err := rows.Scan(&p.TransactionID, &p.Amount, &p.Category, &p.Note, &happened); err != nil {
			return nil, err
		}
		p.HappenedAt = happened.Format(time.RFC3339)
		out = append(out, p)
	}
	return out, rows.Err()
}

func formatMoney(value float64) string {
	if value == float64(int64(value)) {
		return formatInt(int64(value))
	}
	parts := fmt.Sprintf("%.2f", value)
	return parts
}

func formatInt(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	digits := ""
	for n > 0 {
		group := n % 1000
		n /= 1000
		if n > 0 {
			digits = fmt.Sprintf(",%03d", group) + digits
		} else {
			digits = fmt.Sprintf("%d", group) + digits
		}
	}
	if neg {
		digits = "-" + digits
	}
	return digits
}
