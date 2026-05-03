package ledger

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// FetchHouseholdRange returns household transaction rows for a household
// in the given time range. The userID is used to verify the caller is a
// member of the household.
func (w *Writer) FetchHouseholdRange(ctx context.Context, userID, householdID, from, to string) ([]SummaryRow, error) {
	if userID == "" || householdID == "" {
		return nil, fmt.Errorf("ledger household: userID and householdID required")
	}
	rows, err := w.pool.Query(ctx, `
		SELECT ht.type, ht.amount, ht.category, COALESCE(ht.note, ''), ht."happenedAt"
		FROM household_transactions ht
		JOIN household_members hm ON hm."householdId" = ht."householdId" AND hm."userId" = $1
		WHERE ht."householdId" = $2 AND ht."deletedAt" IS NULL
		  AND ht."happenedAt" BETWEEN $3::timestamptz AND $4::timestamptz
		ORDER BY ht."happenedAt" DESC`, userID, householdID, from, to)
	if err != nil {
		return nil, fmt.Errorf("ledger household: query range: %w", err)
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

// HouseholdSummary builds a Thai-language summary for a household in the
// given time range. It separates income and expense, calculates the balance,
// and formats a readable message with line-item details.
func HouseholdSummary(ctx context.Context, writer *Writer, userID, householdID, from, to string) (string, error) {
	rows, err := writer.FetchHouseholdRange(ctx, userID, householdID, from, to)
	if err != nil {
		return "", err
	}

	expense := sumByType(rows, "EXPENSE")
	income := sumByType(rows, "INCOME")
	balance := income - expense

	var b strings.Builder
	b.WriteString("รายรับ ")
	b.WriteString(formatMoney(income))
	b.WriteString(" บาท | รายจ่าย ")
	b.WriteString(formatMoney(expense))
	b.WriteString(" บาท | คงเหลือ ")
	b.WriteString(formatMoney(balance))
	b.WriteString(" บาท")

	expenseLines := filterByType(rows, "EXPENSE")
	incomeLines := filterByType(rows, "INCOME")
	if len(expenseLines) == 0 && len(incomeLines) == 0 {
		b.WriteString("\nยังไม่มีรายการในบ้านในช่วงนี้ครับ")
		return b.String(), nil
	}

	if len(expenseLines) > 0 {
		b.WriteString("\nรายจ่าย:")
		appendRows(&b, expenseLines, false, 8)
	}
	if len(incomeLines) > 0 {
		b.WriteString("\nรายรับ:")
		appendRows(&b, incomeLines, false, 5)
	}
	cats := categoryTotals(expenseLines)
	if len(cats) > 0 {
		b.WriteString("\nรวมรายจ่ายตามหมวด:")
		for i, c := range cats {
			if i >= 5 {
				break
			}
			b.WriteString("\n- ")
			b.WriteString(c.Category)
			b.WriteString(" ")
			b.WriteString(formatMoney(c.Amount))
			b.WriteString(" บาท")
		}
	}
	return b.String(), nil
}

// CreateHousehold inserts a new household and adds the creator as OWNER.
func (w *Writer) CreateHousehold(ctx context.Context, userID, name string) (Household, error) {
	name = strings.TrimSpace(name)
	if userID == "" || name == "" {
		return Household{}, fmt.Errorf("ledger household: userID and name required")
	}
	var h Household
	err := w.pool.QueryRow(ctx, `
		INSERT INTO households (name, "createdBy", "createdAt", "updatedAt")
		VALUES ($1, $2, NOW(), NOW())
		RETURNING id, name, "createdBy", "createdAt", "updatedAt"`,
		name, userID,
	).Scan(&h.ID, &h.Name, &h.CreatedBy, &h.CreatedAt, &h.UpdatedAt)
	if err != nil {
		return Household{}, fmt.Errorf("ledger household: create: %w", err)
	}
	// Add creator as OWNER
	_, err = w.pool.Exec(ctx, `
		INSERT INTO household_members ("householdId", "userId", role, "joinedAt")
		VALUES ($1, $2, 'OWNER', NOW())
		ON CONFLICT ("householdId", "userId") DO NOTHING`,
		h.ID, userID)
	if err != nil {
		return Household{}, fmt.Errorf("ledger household: add owner: %w", err)
	}
	h.MemberCount = 1
	return h, nil
}

// ListMyHouseholds returns households where the user is a member.
func (w *Writer) ListMyHouseholds(ctx context.Context, userID string) ([]Household, error) {
	if userID == "" {
		return nil, fmt.Errorf("ledger household: userID required")
	}
	rows, err := w.pool.Query(ctx, `
		SELECT h.id, h.name, h."createdBy", h."createdAt", h."updatedAt",
		       hm.role,
		       (SELECT COUNT(*) FROM household_members WHERE "householdId" = h.id) AS member_count
		FROM households h
		JOIN household_members hm ON hm."householdId" = h.id AND hm."userId" = $1
		ORDER BY h."updatedAt" DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("ledger household: list: %w", err)
	}
	defer rows.Close()

	var out []Household
	for rows.Next() {
		var h Household
		if err := rows.Scan(&h.ID, &h.Name, &h.CreatedBy, &h.CreatedAt, &h.UpdatedAt, &h.Role, &h.MemberCount); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// AddHouseholdMember adds a user to a household. Only OWNER can add members.
func (w *Writer) AddHouseholdMember(ctx context.Context, ownerUserID, householdID, memberUserID string) error {
	if ownerUserID == "" || householdID == "" || memberUserID == "" {
		return fmt.Errorf("ledger household: ownerUserID, householdID and memberUserID required")
	}
	// Verify caller is OWNER
	var role string
	err := w.pool.QueryRow(ctx, `
		SELECT role FROM household_members
		WHERE "householdId" = $1 AND "userId" = $2`,
		householdID, ownerUserID).Scan(&role)
	if err != nil {
		return fmt.Errorf("ledger household: not a member: %w", err)
	}
	if role != "OWNER" {
		return fmt.Errorf("ledger household: only owner can add members")
	}
	_, err = w.pool.Exec(ctx, `
		INSERT INTO household_members ("householdId", "userId", role, "joinedAt")
		VALUES ($1, $2, 'MEMBER', NOW())
		ON CONFLICT ("householdId", "userId") DO NOTHING`,
		householdID, memberUserID)
	if err != nil {
		return fmt.Errorf("ledger household: add member: %w", err)
	}
	return nil
}

// HouseholdDashboard returns a dashboard summary for a household.
func (w *Writer) HouseholdDashboard(ctx context.Context, userID, householdID string, from, to time.Time) (HouseholdDashboardData, error) {
	if userID == "" || householdID == "" {
		return HouseholdDashboardData{}, fmt.Errorf("ledger household: userID and householdID required")
	}

	// Verify membership
	var role string
	err := w.pool.QueryRow(ctx, `
		SELECT role FROM household_members
		WHERE "householdId" = $1 AND "userId" = $2`,
		householdID, userID).Scan(&role)
	if err != nil {
		return HouseholdDashboardData{}, fmt.Errorf("ledger household: not a member: %w", err)
	}

	fromText := from.UTC().Format(time.RFC3339Nano)
	toText := to.UTC().Format(time.RFC3339Nano)

	// Get household info
	var h Household
	err = w.pool.QueryRow(ctx, `
		SELECT h.id, h.name, h."createdBy", h."createdAt", h."updatedAt",
		       (SELECT COUNT(*) FROM household_members WHERE "householdId" = h.id) AS member_count
		FROM households h WHERE h.id = $1`, householdID,
	).Scan(&h.ID, &h.Name, &h.CreatedBy, &h.CreatedAt, &h.UpdatedAt, &h.MemberCount)
	if err != nil {
		return HouseholdDashboardData{}, fmt.Errorf("ledger household: household not found: %w", err)
	}
	h.Role = role

	// Totals in range
	var totals BalanceTotals
	err = w.pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN type = 'INCOME' THEN amount ELSE 0 END), 0)::float8,
			COALESCE(SUM(CASE WHEN type = 'EXPENSE' THEN amount ELSE 0 END), 0)::float8
		FROM household_transactions
		WHERE "householdId" = $1 AND "deletedAt" IS NULL
		  AND "happenedAt" BETWEEN $2::timestamptz AND $3::timestamptz`,
		householdID, fromText, toText,
	).Scan(&totals.Income, &totals.Expense)
	if err != nil {
		return HouseholdDashboardData{}, fmt.Errorf("ledger household: totals: %w", err)
	}

	// Recent transactions
	recentRows, err := w.pool.Query(ctx, `
		SELECT id, type, amount::float8, category, COALESCE(note, ''), "userId", "happenedAt"
		FROM household_transactions
		WHERE "householdId" = $1 AND "deletedAt" IS NULL
		  AND "happenedAt" BETWEEN $2::timestamptz AND $3::timestamptz
		ORDER BY "happenedAt" DESC
		LIMIT 20`, householdID, fromText, toText)
	if err != nil {
		return HouseholdDashboardData{}, fmt.Errorf("ledger household: recent: %w", err)
	}
	defer recentRows.Close()

	var recent []HouseholdTransactionSummary
	for recentRows.Next() {
		var tx HouseholdTransactionSummary
		var happened time.Time
		if err := recentRows.Scan(&tx.ID, &tx.Type, &tx.Amount, &tx.Category, &tx.Note, &tx.UserID, &happened); err != nil {
			return HouseholdDashboardData{}, err
		}
		tx.AmountText = formatMoney(tx.Amount)
		tx.TypeLabel = typeLabel(tx.Type)
		tx.HappenedAt = happened.Format(time.RFC3339)
		recent = append(recent, tx)
	}
	if err := recentRows.Err(); err != nil {
		return HouseholdDashboardData{}, err
	}

	// Member list
	memberRows, err := w.pool.Query(ctx, `
		SELECT hm."userId", hm.role, hm."joinedAt"
		FROM household_members hm
		WHERE hm."householdId" = $1
		ORDER BY hm."joinedAt" ASC`, householdID)
	if err != nil {
		return HouseholdDashboardData{}, fmt.Errorf("ledger household: members: %w", err)
	}
	defer memberRows.Close()

	var members []HouseholdMemberInfo
	for memberRows.Next() {
		var m HouseholdMemberInfo
		if err := memberRows.Scan(&m.UserID, &m.Role, &m.JoinedAt); err != nil {
			return HouseholdDashboardData{}, err
		}
		members = append(members, m)
	}
	if err := memberRows.Err(); err != nil {
		return HouseholdDashboardData{}, err
	}

	return HouseholdDashboardData{
		Household:      h,
		Totals:         decorateTotals(totals.Income, totals.Expense),
		Recent:         recent,
		Members:        members,
		From:           from.Format("2006-01-02"),
		To:             to.Format("2006-01-02"),
	}, nil
}

// CreateHouseholdTransaction inserts a transaction into the household.
func (w *Writer) CreateHouseholdTransaction(ctx context.Context, userID, householdID string, in HouseholdTransactionInput) (string, error) {
	if userID == "" || householdID == "" {
		return "", fmt.Errorf("ledger household: userID and householdID required")
	}
	if in.Type != "INCOME" && in.Type != "EXPENSE" {
		return "", fmt.Errorf("ledger household: type must be INCOME or EXPENSE")
	}
	if in.Amount <= 0 {
		return "", fmt.Errorf("ledger household: amount must be positive")
	}

	// Verify membership
	var exists bool
	err := w.pool.QueryRow(ctx, `
		SELECT true FROM household_members
		WHERE "householdId" = $1 AND "userId" = $2`,
		householdID, userID).Scan(&exists)
	if err != nil {
		return "", fmt.Errorf("ledger household: not a member: %w", err)
	}

	if in.HappenedAt.IsZero() {
		in.HappenedAt = time.Now().UTC()
	}

	var txID string
	err = w.pool.QueryRow(ctx, `
		INSERT INTO household_transactions (id, "householdId", "userId", type, amount, category, note, "happenedAt", "createdAt", "updatedAt")
		VALUES (substr(replace(gen_random_uuid()::text,'-',''),1,25), $1, $2, $3, $4::numeric, $5, $6, $7, NOW(), NOW())
		RETURNING id`,
		householdID, userID, in.Type, in.Amount, in.Category, nullableString(strings.TrimSpace(in.Note)), in.HappenedAt,
	).Scan(&txID)
	if err != nil {
		return "", fmt.Errorf("ledger household: insert transaction: %w", err)
	}
	return txID, nil
}

// Household holds a household row.
type Household struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	CreatedBy   string    `json:"createdBy"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Role        string    `json:"role,omitempty"`
	MemberCount int       `json:"memberCount"`
}

// HouseholdTransactionInput is the input for creating a household transaction.
type HouseholdTransactionInput struct {
	Type       string    `json:"type"`
	Amount     float64   `json:"amount"`
	Category   string    `json:"category"`
	Note       string    `json:"note"`
	HappenedAt time.Time `json:"happenedAt"`
}

// HouseholdTransactionSummary is a transaction row for dashboard display.
type HouseholdTransactionSummary struct {
	ID         string  `json:"id"`
	Type       string  `json:"type"`
	TypeLabel  string  `json:"typeLabel"`
	Amount     float64 `json:"amount"`
	AmountText string  `json:"amountText"`
	Category   string  `json:"category"`
	Note       string  `json:"note"`
	UserID     string  `json:"userId"`
	HappenedAt string  `json:"happenedAt"`
}

// HouseholdMemberInfo is a member row.
type HouseholdMemberInfo struct {
	UserID   string    `json:"userId"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joinedAt"`
}

// HouseholdDashboardData is the full dashboard response.
type HouseholdDashboardData struct {
	Household Household                    `json:"household"`
	Totals    DashboardTotals              `json:"totals"`
	Recent    []HouseholdTransactionSummary `json:"recent"`
	Members   []HouseholdMemberInfo        `json:"members"`
	From      string                       `json:"from"`
	To        string                       `json:"to"`
}
