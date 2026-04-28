package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const cashAccountCode = "1010"

// WriteTxInput captures one expense/income line. The caller is responsible
// for knowing the userID and agentID — the dispatcher derives these from the
// LINE userId via the user/agent services.
type WriteTxInput struct {
	UserID           string
	AgentID          string
	Namespace        string
	Type             string // INCOME | EXPENSE
	Amount           float64
	Category         string
	Note             string
	CounterpartyName string
	CounterpartyRole string
	Confidence       float64
	CausationID      string
	HappenedAt       time.Time
	Allocations      []AllocationInput
}

type Writer struct {
	pool *pgxpool.Pool
}

func NewWriter(pool *pgxpool.Pool) *Writer { return &Writer{pool: pool} }

// CreatePersonalTransaction inserts the row into the legacy `transactions`
// table (Node + Go share it) and appends a ledger_event so projections can
// rebuild from event log later. Both writes happen in a single tx so a
// crash mid-write never leaves the projection out of sync.
func (w *Writer) CreatePersonalTransaction(ctx context.Context, in WriteTxInput) (txID string, err error) {
	if in.UserID == "" || in.Namespace == "" {
		return "", fmt.Errorf("ledger writer: userID and namespace required")
	}
	if in.Confidence == 0 {
		in.Confidence = 1
	}
	if in.HappenedAt.IsZero() {
		in.HappenedAt = time.Now().UTC()
	}
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("ledger writer: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if in.CausationID != "" {
		var payload []byte
		err := tx.QueryRow(ctx, `
			SELECT payload
			FROM ledger_events
			WHERE namespace = $1 AND "causationId" = $2 AND type = 'tx.recorded'
			ORDER BY id ASC
			LIMIT 1`,
			in.Namespace, in.CausationID,
		).Scan(&payload)
		if err == nil {
			var existing struct {
				TransactionID string `json:"transactionId"`
			}
			_ = json.Unmarshal(payload, &existing)
			if existing.TransactionID != "" {
				return existing.TransactionID, nil
			}
			return "", nil
		}
	}

	if err := tx.QueryRow(ctx, `
		INSERT INTO transactions (id, "userId", "agentId", namespace, scope, type, amount, currency, category, note, "counterpartyName", "counterpartyRole", source, confidence, "happenedAt", "createdAt", "updatedAt")
		VALUES (substr(replace(gen_random_uuid()::text,'-',''),1,25), $1, $2, $3, 'PERSONAL', $4, $5::numeric, 'THB', $6, $7, $8, $9, 'line', $10, $11, NOW(), NOW())
		RETURNING id`,
		in.UserID, in.AgentID, in.Namespace, in.Type, in.Amount, in.Category, in.Note,
		nullableString(strings.TrimSpace(in.CounterpartyName)), nullableString(strings.TrimSpace(in.CounterpartyRole)),
		in.Confidence, in.HappenedAt,
	).Scan(&txID); err != nil {
		return "", fmt.Errorf("ledger writer: insert transaction: %w", err)
	}
	journalID, err := w.createJournalEntry(ctx, tx, in, txID)
	if err != nil {
		return "", err
	}
	if err := validateAllocations(ctx, tx, in.UserID, in.Namespace, in.Amount, in.Allocations); err != nil {
		return "", err
	}
	if err := insertAllocations(ctx, tx, in.UserID, in.Namespace, txID, in.Amount, in.Allocations); err != nil {
		return "", err
	}

	payloadBytes, err := json.Marshal(map[string]any{
		"transactionId":    txID,
		"journalEntryId":   journalID,
		"type":             in.Type,
		"amount":           in.Amount,
		"category":         in.Category,
		"note":             in.Note,
		"counterpartyName": strings.TrimSpace(in.CounterpartyName),
		"counterpartyRole": strings.TrimSpace(in.CounterpartyRole),
		"happenedAt":       in.HappenedAt,
		"allocations":      in.Allocations,
	})
	if err != nil {
		return "", fmt.Errorf("ledger writer: marshal event payload: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO ledger_events (namespace, "userId", type, payload, "causationId", "occurredAt")
		VALUES ($1, $2, 'tx.recorded', $3::jsonb, $4, $5)`,
		in.Namespace, in.UserID, string(payloadBytes), nullableString(in.CausationID), in.HappenedAt,
	); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && in.CausationID != "" {
			_ = tx.Rollback(ctx)
			return w.findExistingTransactionID(ctx, in.Namespace, in.CausationID)
		}
		return "", fmt.Errorf("ledger writer: insert event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("ledger writer: commit: %w", err)
	}
	return txID, nil
}

type txer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func (w *Writer) createJournalEntry(ctx context.Context, tx txer, in WriteTxInput, txID string) (string, error) {
	cashID, err := ensureAccount(ctx, tx, in.Namespace, cashAccount)
	if err != nil {
		return "", err
	}
	target := accountForTransaction(in.Type, in.Category)
	targetID, err := ensureAccount(ctx, tx, in.Namespace, target)
	if err != nil {
		return "", err
	}
	var journalID string
	err = tx.QueryRow(ctx, `
		INSERT INTO journal_entries (namespace, "userId", "agentId", "sourceTransactionId", memo, status, "causationId", "occurredAt", "createdAt")
		VALUES ($1, $2, $3, $4, $5, 'POSTED', $6, $7, NOW())
		RETURNING id`,
		in.Namespace, in.UserID, nullableString(in.AgentID), txID, in.Note, nullableString(in.CausationID), in.HappenedAt,
	).Scan(&journalID)
	if err != nil {
		return "", fmt.Errorf("ledger writer: insert journal entry: %w", err)
	}

	switch in.Type {
	case "INCOME":
		if err := insertJournalLine(ctx, tx, journalID, in.Namespace, cashID, in.Amount, 0, in.Note); err != nil {
			return "", err
		}
		if err := insertJournalLine(ctx, tx, journalID, in.Namespace, targetID, 0, in.Amount, in.Note); err != nil {
			return "", err
		}
	case "EXPENSE":
		if err := insertJournalLine(ctx, tx, journalID, in.Namespace, targetID, in.Amount, 0, in.Note); err != nil {
			return "", err
		}
		if err := insertJournalLine(ctx, tx, journalID, in.Namespace, cashID, 0, in.Amount, in.Note); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("ledger writer: unsupported transaction type for journal: %s", in.Type)
	}
	return journalID, nil
}

func ensureAccount(ctx context.Context, tx txer, namespace string, spec accountSpec) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `
		INSERT INTO chart_accounts (namespace, code, name, "accountType", "normalBalance", "isSystem", "createdAt", "updatedAt")
		VALUES ($1, $2, $3, $4, $5, true, NOW(), NOW())
		ON CONFLICT (namespace, code) DO UPDATE SET
			name = EXCLUDED.name,
			"accountType" = EXCLUDED."accountType",
			"normalBalance" = EXCLUDED."normalBalance",
			"updatedAt" = NOW()
		RETURNING id`,
		namespace, spec.Code, spec.Name, spec.AccountType, spec.NormalBalance,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("ledger writer: ensure account %s: %w", spec.Code, err)
	}
	return id, nil
}

func insertJournalLine(ctx context.Context, tx txer, journalID, namespace, accountID string, debit, credit float64, memo string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO journal_lines ("journalEntryId", namespace, "accountId", debit, credit, currency, memo, "createdAt")
		VALUES ($1, $2, $3, $4::numeric, $5::numeric, 'THB', $6, NOW())`,
		journalID, namespace, accountID, debit, credit, memo,
	)
	if err != nil {
		return fmt.Errorf("ledger writer: insert journal line: %w", err)
	}
	return nil
}

func accountForTransaction(txType, category string) accountSpec {
	switch txType {
	case "INCOME":
		if spec, ok := incomeAccounts[category]; ok {
			return spec
		}
		return incomeAccounts["รายรับอื่น ๆ"]
	case "EXPENSE":
		if spec, ok := expenseAccounts[category]; ok {
			return spec
		}
		return expenseAccounts["อื่น ๆ"]
	default:
		return expenseAccounts["อื่น ๆ"]
	}
}

func (w *Writer) findExistingTransactionID(ctx context.Context, namespace, causationID string) (string, error) {
	var payload []byte
	err := w.pool.QueryRow(ctx, `
		SELECT payload
		FROM ledger_events
		WHERE namespace = $1 AND "causationId" = $2 AND type = 'tx.recorded'
		ORDER BY id ASC
		LIMIT 1`, namespace, causationID).Scan(&payload)
	if err != nil {
		return "", fmt.Errorf("ledger writer: duplicate lookup: %w", err)
	}
	var existing struct {
		TransactionID string `json:"transactionId"`
	}
	_ = json.Unmarshal(payload, &existing)
	return existing.TransactionID, nil
}

// SummaryRow is one transactions row needed by the summary builder.
type SummaryRow struct {
	Type       string
	Amount     float64
	Category   string
	Note       string
	HappenedAt time.Time
}

type BalanceTotals struct {
	Income  float64
	Expense float64
}

type accountSpec struct {
	Code          string
	Name          string
	AccountType   string
	NormalBalance string
}

var cashAccount = accountSpec{Code: cashAccountCode, Name: "เงินสด/เงินฝากพร้อมใช้", AccountType: "ASSET", NormalBalance: "DEBIT"}

var incomeAccounts = map[string]accountSpec{
	"เงินเดือน":       {Code: "4010", Name: "รายได้เงินเดือน", AccountType: "INCOME", NormalBalance: "CREDIT"},
	"รายได้จากการขาย": {Code: "4020", Name: "รายได้จากการขาย", AccountType: "INCOME", NormalBalance: "CREDIT"},
	"รายรับอื่น ๆ":    {Code: "4099", Name: "รายรับอื่น ๆ", AccountType: "INCOME", NormalBalance: "CREDIT"},
}

var expenseAccounts = map[string]accountSpec{
	"อาหารและเครื่องดื่ม": {Code: "5010", Name: "ค่าอาหารและเครื่องดื่ม", AccountType: "EXPENSE", NormalBalance: "DEBIT"},
	"ค่าน้ำค่าไฟ":         {Code: "5020", Name: "ค่าน้ำค่าไฟ", AccountType: "EXPENSE", NormalBalance: "DEBIT"},
	"เดินทาง":             {Code: "5030", Name: "ค่าเดินทาง", AccountType: "EXPENSE", NormalBalance: "DEBIT"},
	"ของใช้เข้าบ้าน":      {Code: "5040", Name: "ของใช้เข้าบ้าน", AccountType: "EXPENSE", NormalBalance: "DEBIT"},
	"สุขภาพ":              {Code: "5050", Name: "ค่าสุขภาพ", AccountType: "EXPENSE", NormalBalance: "DEBIT"},
	"การศึกษา":            {Code: "5060", Name: "ค่าการศึกษา", AccountType: "EXPENSE", NormalBalance: "DEBIT"},
	"อื่น ๆ":              {Code: "5099", Name: "ค่าใช้จ่ายอื่น ๆ", AccountType: "EXPENSE", NormalBalance: "DEBIT"},
}

// FetchUserRange returns rows in [from, to] for a userID. Used by the
// summary service. Filters by userID instead of namespace because the
// transactions table is keyed by userID directly.
func (w *Writer) FetchUserRange(ctx context.Context, userID, namespace string, from, to string) ([]SummaryRow, error) {
	if userID == "" || namespace == "" {
		return nil, fmt.Errorf("ledger reader: userID and namespace required")
	}
	rows, err := w.pool.Query(ctx, `
		SELECT type, amount, category, COALESCE(note, ''), "happenedAt"
		FROM transactions
		WHERE "userId" = $1 AND namespace = $2 AND "deletedAt" IS NULL
		  AND "happenedAt" BETWEEN $3::timestamptz AND $4::timestamptz
		ORDER BY "happenedAt" DESC`, userID, namespace, from, to)
	if err != nil {
		return nil, fmt.Errorf("ledger reader: query: %w", err)
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

func (w *Writer) FetchUserTotals(ctx context.Context, userID, namespace string) (BalanceTotals, error) {
	if userID == "" || namespace == "" {
		return BalanceTotals{}, fmt.Errorf("ledger reader: userID and namespace required")
	}
	var totals BalanceTotals
	err := w.pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN type = 'INCOME' THEN amount ELSE 0 END), 0)::float8 AS income,
			COALESCE(SUM(CASE WHEN type = 'EXPENSE' THEN amount ELSE 0 END), 0)::float8 AS expense
		FROM transactions
		WHERE "userId" = $1 AND namespace = $2 AND "deletedAt" IS NULL`, userID, namespace).Scan(&totals.Income, &totals.Expense)
	if err != nil {
		return BalanceTotals{}, fmt.Errorf("ledger reader: totals: %w", err)
	}
	return totals, nil
}
