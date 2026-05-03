package ledger

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// BusinessInfo represents a business record returned from the DB.
type BusinessInfo struct {
	ID        string `json:"id"`
	OwnerID   string `json:"ownerId"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	CreatedAt string `json:"createdAt"`
}

// CreateBusiness inserts a new business row and returns the created record.
func (w *Writer) CreateBusiness(ctx context.Context, ownerID, name, bizType string) (*BusinessInfo, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("ledger business: name required")
	}
	if bizType == "" {
		bizType = "ร้านค้าเล็ก"
	}
	var b BusinessInfo
	err := w.pool.QueryRow(ctx, `
		INSERT INTO businesses (id, "ownerId", name, type, "createdAt")
		VALUES (substr(replace(gen_random_uuid()::text,'-',''),1,25), $1, $2, $3, NOW())
		RETURNING id, "ownerId", name, type, "createdAt"`,
		ownerID, name, bizType,
	).Scan(&b.ID, &b.OwnerID, &b.Name, &b.Type, &b.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("ledger business: create: %w", err)
	}
	return &b, nil
}

// ListMyBusinesses returns all businesses owned by the given user.
func (w *Writer) ListMyBusinesses(ctx context.Context, ownerID string) ([]BusinessInfo, error) {
	rows, err := w.pool.Query(ctx, `
		SELECT id, "ownerId", name, type, "createdAt"
		FROM businesses
		WHERE "ownerId" = $1
		ORDER BY "createdAt" DESC`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("ledger business: list: %w", err)
	}
	defer rows.Close()
	var out []BusinessInfo
	for rows.Next() {
		var b BusinessInfo
		var ts time.Time
		if err := rows.Scan(&b.ID, &b.OwnerID, &b.Name, &b.Type, &ts); err != nil {
			return nil, err
		}
		b.CreatedAt = ts.Format(time.RFC3339)
		out = append(out, b)
	}
	return out, rows.Err()
}

// BusinessDashboardData holds the dashboard info for a business.
type BusinessDashboardData struct {
	Business         BusinessInfo       `json:"business"`
	TotalIncome      float64            `json:"totalIncome"`
	TotalExpense     float64            `json:"totalExpense"`
	TotalCoGS        float64            `json:"totalCoGS"`
	RecentTransactions []SummaryRow    `json:"recentTransactions"`
}

// BusinessDashboard returns a dashboard overview for a business,
// verifying the caller is the owner.
func (w *Writer) BusinessDashboard(ctx context.Context, userID, businessID string, from, to time.Time) (*BusinessDashboardData, error) {
	// Verify ownership
	var ownerID string
	err := w.pool.QueryRow(ctx, `SELECT "ownerId" FROM businesses WHERE id = $1`, businessID).Scan(&ownerID)
	if err != nil {
		return nil, fmt.Errorf("ledger business dashboard: business not found: %w", err)
	}
	if ownerID != userID {
		return nil, fmt.Errorf("ledger business dashboard: not the owner")
	}

	fromStr := from.UTC().Format(time.RFC3339Nano)
	toStr := to.UTC().Format(time.RFC3339Nano)

	rows, err := FetchBusinessRange(ctx, w, businessID, fromStr, toStr)
	if err != nil {
		return nil, err
	}

	totalIncome := sumByType(rows, "INCOME")
	totalExpense := sumByType(rows, "EXPENSE")

	// Compute CoGS separately
	var coGS float64
	_ = w.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount), 0)::float8
		FROM transactions
		WHERE scope = 'BUSINESS' AND "scopeId" = $1 AND "deletedAt" IS NULL
		  AND type = 'EXPENSE' AND category = 'ต้นทุนสินค้า'
		  AND "happenedAt" BETWEEN $2::timestamptz AND $3::timestamptz`,
		businessID, fromStr, toStr,
	).Scan(&coGS)

	return &BusinessDashboardData{
		Business: BusinessInfo{
			ID:   businessID,
			Name: "",
		},
		TotalIncome:         totalIncome,
		TotalExpense:        totalExpense,
		TotalCoGS:           coGS,
		RecentTransactions:  rows,
	}, nil
}
