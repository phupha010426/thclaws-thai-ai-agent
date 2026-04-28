package ledger

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/thaiaiagent/go-core/internal/thaitime"
)

type ProjectSummary struct {
	ID                  string  `json:"id"`
	Name                string  `json:"name"`
	Description         string  `json:"description,omitempty"`
	Status              string  `json:"status"`
	Color               string  `json:"color"`
	BudgetAmount        float64 `json:"budgetAmount,omitempty"`
	BudgetText          string  `json:"budgetText,omitempty"`
	RemainingBudget     float64 `json:"remainingBudget,omitempty"`
	RemainingBudgetText string  `json:"remainingBudgetText,omitempty"`
	BudgetUsedPct       float64 `json:"budgetUsedPct,omitempty"`
	TargetDate          string  `json:"targetDate,omitempty"`
	TargetDateInput     string  `json:"targetDateInput,omitempty"`
	Income              float64 `json:"income"`
	Expense             float64 `json:"expense"`
	Balance             float64 `json:"balance"`
	IncomeText          string  `json:"incomeText"`
	ExpenseText         string  `json:"expenseText"`
	BalanceText         string  `json:"balanceText"`
}

type AllocationInput struct {
	ProjectID string  `json:"projectId"`
	Amount    float64 `json:"amount"`
	Note      string  `json:"note,omitempty"`
}

type TransactionAllocation struct {
	ProjectID   string  `json:"projectId"`
	ProjectName string  `json:"projectName"`
	Amount      float64 `json:"amount"`
	AmountText  string  `json:"amountText"`
	Percent     float64 `json:"percent"`
}

type ProjectInput struct {
	UserID       string
	Namespace    string
	Name         string
	Description  string
	Color        string
	BudgetAmount float64
	TargetDate   time.Time
}

type TransactionUpdateInput struct {
	UserID           string
	AgentID          string
	Namespace        string
	TransactionID    string
	Type             string
	Amount           float64
	Category         string
	Note             string
	CounterpartyName string
	CounterpartyRole string
	HappenedAt       time.Time
	Allocations      []AllocationInput
}

func (w *Writer) ListProjects(ctx context.Context, userID, namespace, from, to string) ([]ProjectSummary, error) {
	if userID == "" || namespace == "" {
		return nil, fmt.Errorf("ledger projects: userID and namespace required")
	}
	rows, err := w.pool.Query(ctx, `
		SELECT p.id, p.name, COALESCE(p.description, ''), p.status, p.color,
		       COALESCE(p."budgetAmount", 0)::float8, p."targetDate",
		       COALESCE(SUM(CASE WHEN t.type = 'INCOME' THEN a.amount ELSE 0 END), 0)::float8,
		       COALESCE(SUM(CASE WHEN t.type = 'EXPENSE' THEN a.amount ELSE 0 END), 0)::float8
		FROM projects p
		LEFT JOIN transaction_project_allocations a ON a."projectId" = p.id
		LEFT JOIN transactions t ON t.id = a."transactionId"
			AND t."deletedAt" IS NULL
			AND t."happenedAt" BETWEEN $3::timestamptz AND $4::timestamptz
		WHERE p."userId" = $1 AND p.namespace = $2 AND p."deletedAt" IS NULL
		GROUP BY p.id, p.name, p.description, p.status, p.color, p."budgetAmount", p."targetDate", p."sortOrder", p."updatedAt"
		ORDER BY p."sortOrder" ASC, p."updatedAt" DESC, p.name ASC`,
		userID, namespace, from, to)
	if err != nil {
		return nil, fmt.Errorf("ledger projects: list: %w", err)
	}
	defer rows.Close()

	out := []ProjectSummary{}
	for rows.Next() {
		var p ProjectSummary
		var target sql.NullTime
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Status, &p.Color, &p.BudgetAmount, &target, &p.Income, &p.Expense); err != nil {
			return nil, err
		}
		decorateProjectSummary(&p, target)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (w *Writer) CreateProject(ctx context.Context, in ProjectInput) (ProjectSummary, error) {
	name := strings.TrimSpace(in.Name)
	if in.UserID == "" || in.Namespace == "" || name == "" {
		return ProjectSummary{}, fmt.Errorf("ledger projects: userID namespace and name required")
	}
	color := cleanColor(in.Color)
	var p ProjectSummary
	var target sql.NullTime
	err := w.pool.QueryRow(ctx, `
		INSERT INTO projects ("userId", namespace, name, description, color, "budgetAmount", "targetDate", status, "createdAt", "updatedAt")
		VALUES ($1, $2, $3, $4, $5, $6::numeric, $7, 'ACTIVE', NOW(), NOW())
		RETURNING id, name, COALESCE(description, ''), status, color, COALESCE("budgetAmount", 0)::float8, "targetDate"`,
		in.UserID, in.Namespace, name, nullableString(strings.TrimSpace(in.Description)), color,
		nullablePositiveAmount(in.BudgetAmount), nullableTime(in.TargetDate),
	).Scan(&p.ID, &p.Name, &p.Description, &p.Status, &p.Color, &p.BudgetAmount, &target)
	if err != nil {
		return ProjectSummary{}, fmt.Errorf("ledger projects: create: %w", err)
	}
	decorateProjectSummary(&p, target)
	_ = w.appendLedgerEvent(ctx, in.Namespace, in.UserID, "project.created", map[string]any{"projectId": p.ID, "name": p.Name})
	return p, nil
}

func (w *Writer) UpdateProject(ctx context.Context, projectID string, in ProjectInput) (ProjectSummary, error) {
	name := strings.TrimSpace(in.Name)
	if in.UserID == "" || in.Namespace == "" || projectID == "" || name == "" {
		return ProjectSummary{}, fmt.Errorf("ledger projects: userID namespace projectID and name required")
	}
	color := cleanColor(in.Color)
	var p ProjectSummary
	var target sql.NullTime
	err := w.pool.QueryRow(ctx, `
		UPDATE projects
		SET name = $4, description = $5, color = $6, "budgetAmount" = $7::numeric, "targetDate" = $8, "updatedAt" = NOW()
		WHERE id = $1 AND "userId" = $2 AND namespace = $3 AND "deletedAt" IS NULL
		RETURNING id, name, COALESCE(description, ''), status, color, COALESCE("budgetAmount", 0)::float8, "targetDate"`,
		projectID, in.UserID, in.Namespace, name, nullableString(strings.TrimSpace(in.Description)), color,
		nullablePositiveAmount(in.BudgetAmount), nullableTime(in.TargetDate),
	).Scan(&p.ID, &p.Name, &p.Description, &p.Status, &p.Color, &p.BudgetAmount, &target)
	if err != nil {
		if err == pgx.ErrNoRows {
			return ProjectSummary{}, fmt.Errorf("ledger projects: project not found")
		}
		return ProjectSummary{}, fmt.Errorf("ledger projects: update: %w", err)
	}
	decorateProjectSummary(&p, target)
	_ = w.appendLedgerEvent(ctx, in.Namespace, in.UserID, "project.updated", map[string]any{"projectId": p.ID, "name": p.Name})
	return p, nil
}

func (w *Writer) ResolveProjectAllocations(ctx context.Context, userID, namespace, text string, amount float64) ([]AllocationInput, []ProjectSummary, error) {
	if userID == "" || namespace == "" || amount <= 0 {
		return nil, nil, nil
	}
	text = strings.TrimSpace(text)
	if !mentionsProjectContext(text) {
		return nil, nil, nil
	}
	projects, err := w.listActiveProjects(ctx, userID, namespace)
	if err != nil {
		return nil, nil, err
	}
	textKey := normalizeProjectKey(text)
	explicitName := extractExplicitProjectName(text)
	explicitKey := normalizeProjectKey(explicitName)
	matches := []ProjectSummary{}
	seen := map[string]bool{}
	for _, p := range projects {
		nameKey := normalizeProjectKey(p.Name)
		if nameKey == "" {
			continue
		}
		if explicitKey != "" {
			if nameKey == explicitKey || strings.Contains(nameKey, explicitKey) || strings.Contains(explicitKey, nameKey) {
				matches = append(matches, p)
				seen[p.ID] = true
			}
			continue
		}
		if strings.Contains(textKey, nameKey) {
			matches = append(matches, p)
			seen[p.ID] = true
		}
	}
	if len(matches) == 0 && explicitName != "" {
		p, err := w.CreateProject(ctx, ProjectInput{
			UserID:    userID,
			Namespace: namespace,
			Name:      explicitName,
			Color:     "#0f766e",
		})
		if err != nil {
			return nil, nil, err
		}
		matches = append(matches, p)
		seen[p.ID] = true
	}
	if len(matches) == 0 {
		return nil, nil, nil
	}
	allocations := splitProjectAllocations(matches, amount)
	filtered := matches[:0]
	for _, p := range matches {
		if seen[p.ID] {
			filtered = append(filtered, p)
		}
	}
	return allocations, filtered, nil
}

func (w *Writer) listActiveProjects(ctx context.Context, userID, namespace string) ([]ProjectSummary, error) {
	rows, err := w.pool.Query(ctx, `
		SELECT id, name, COALESCE(description, ''), status, color,
		       COALESCE("budgetAmount", 0)::float8, "targetDate"
		FROM projects
		WHERE "userId" = $1 AND namespace = $2 AND status = 'ACTIVE' AND "deletedAt" IS NULL
		ORDER BY "sortOrder" ASC, "updatedAt" DESC, name ASC`,
		userID, namespace)
	if err != nil {
		return nil, fmt.Errorf("ledger projects: active list: %w", err)
	}
	defer rows.Close()
	out := []ProjectSummary{}
	for rows.Next() {
		var p ProjectSummary
		var target sql.NullTime
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Status, &p.Color, &p.BudgetAmount, &target); err != nil {
			return nil, err
		}
		decorateProjectSummary(&p, target)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (w *Writer) DeleteProject(ctx context.Context, userID, namespace, projectID string) error {
	if userID == "" || namespace == "" || projectID == "" {
		return fmt.Errorf("ledger projects: userID namespace and projectID required")
	}
	tag, err := w.pool.Exec(ctx, `
		UPDATE projects
		SET status = 'ARCHIVED', "deletedAt" = NOW(), "updatedAt" = NOW()
		WHERE id = $1 AND "userId" = $2 AND namespace = $3 AND "deletedAt" IS NULL`,
		projectID, userID, namespace)
	if err != nil {
		return fmt.Errorf("ledger projects: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("ledger projects: project not found")
	}
	_ = w.appendLedgerEvent(ctx, namespace, userID, "project.deleted", map[string]any{"projectId": projectID})
	return nil
}

func (w *Writer) UpdateTransaction(ctx context.Context, in TransactionUpdateInput) error {
	in.Type = strings.ToUpper(strings.TrimSpace(in.Type))
	in.Category = strings.TrimSpace(in.Category)
	in.Note = strings.TrimSpace(in.Note)
	in.CounterpartyName = strings.TrimSpace(in.CounterpartyName)
	in.CounterpartyRole = strings.TrimSpace(in.CounterpartyRole)
	if err := validateTransactionUpdate(in); err != nil {
		return err
	}
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("ledger tx update: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var old struct {
		Type       string
		Amount     float64
		Category   string
		Note       string
		HappenedAt time.Time
	}
	if err := tx.QueryRow(ctx, `
		SELECT type, amount::float8, category, COALESCE(note, ''), "happenedAt"
		FROM transactions
		WHERE id = $1 AND "userId" = $2 AND namespace = $3 AND "deletedAt" IS NULL
		FOR UPDATE`,
		in.TransactionID, in.UserID, in.Namespace,
	).Scan(&old.Type, &old.Amount, &old.Category, &old.Note, &old.HappenedAt); err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("ledger tx update: transaction not found")
		}
		return fmt.Errorf("ledger tx update: load: %w", err)
	}

	if err := validateAllocations(ctx, tx, in.UserID, in.Namespace, in.Amount, in.Allocations); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE transactions
		SET type = $4, amount = $5::numeric, category = $6, note = $7,
		    "counterpartyName" = $8, "counterpartyRole" = $9,
		    "happenedAt" = $10, "updatedAt" = NOW()
		WHERE id = $1 AND "userId" = $2 AND namespace = $3 AND "deletedAt" IS NULL`,
		in.TransactionID, in.UserID, in.Namespace, in.Type, in.Amount, in.Category, nullableString(strings.TrimSpace(in.Note)),
		nullableString(strings.TrimSpace(in.CounterpartyName)), nullableString(strings.TrimSpace(in.CounterpartyRole)), in.HappenedAt,
	); err != nil {
		return fmt.Errorf("ledger tx update: update transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE journal_entries
		SET status = 'VOID'
		WHERE namespace = $1 AND "sourceTransactionId" = $2 AND status = 'POSTED'`,
		in.Namespace, in.TransactionID); err != nil {
		return fmt.Errorf("ledger tx update: void journal: %w", err)
	}
	journalInput := WriteTxInput{
		UserID: in.UserID, AgentID: in.AgentID, Namespace: in.Namespace, Type: in.Type, Amount: in.Amount,
		Category: in.Category, Note: in.Note, CounterpartyName: in.CounterpartyName, CounterpartyRole: in.CounterpartyRole,
		Confidence: 1, CausationID: "tx.updated:" + in.TransactionID + ":" + uuid.NewString(), HappenedAt: in.HappenedAt,
	}
	if _, err := w.createJournalEntry(ctx, tx, journalInput, in.TransactionID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM transaction_project_allocations WHERE "transactionId" = $1 AND "userId" = $2 AND namespace = $3`, in.TransactionID, in.UserID, in.Namespace); err != nil {
		return fmt.Errorf("ledger tx update: clear allocations: %w", err)
	}
	if err := insertAllocations(ctx, tx, in.UserID, in.Namespace, in.TransactionID, in.Amount, in.Allocations); err != nil {
		return err
	}
	payload := map[string]any{
		"transactionId": in.TransactionID,
		"type":          in.Type,
		"amount":        in.Amount,
		"category":      in.Category,
		"note":          in.Note,
		"happenedAt":    in.HappenedAt,
		"old": map[string]any{
			"type":       old.Type,
			"amount":     old.Amount,
			"category":   old.Category,
			"note":       old.Note,
			"happenedAt": old.HappenedAt,
		},
		"allocations": in.Allocations,
	}
	if err := insertLedgerEventTx(ctx, tx, in.Namespace, in.UserID, "tx.updated", payload); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("ledger tx update: commit: %w", err)
	}
	return nil
}

func (w *Writer) DeleteTransaction(ctx context.Context, userID, namespace, transactionID string) error {
	if userID == "" || namespace == "" || transactionID == "" {
		return fmt.Errorf("ledger tx delete: userID namespace and transactionID required")
	}
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("ledger tx delete: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		UPDATE transactions
		SET "deletedAt" = NOW(), "updatedAt" = NOW()
		WHERE id = $1 AND "userId" = $2 AND namespace = $3 AND "deletedAt" IS NULL`,
		transactionID, userID, namespace)
	if err != nil {
		return fmt.Errorf("ledger tx delete: update transaction: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("ledger tx delete: transaction not found")
	}
	if _, err := tx.Exec(ctx, `
		UPDATE journal_entries
		SET status = 'VOID'
		WHERE namespace = $1 AND "sourceTransactionId" = $2 AND status = 'POSTED'`,
		namespace, transactionID); err != nil {
		return fmt.Errorf("ledger tx delete: void journal: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM transaction_project_allocations WHERE "transactionId" = $1 AND "userId" = $2 AND namespace = $3`, transactionID, userID, namespace); err != nil {
		return fmt.Errorf("ledger tx delete: clear allocations: %w", err)
	}
	if err := insertLedgerEventTx(ctx, tx, namespace, userID, "tx.deleted", map[string]any{"transactionId": transactionID}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("ledger tx delete: commit: %w", err)
	}
	return nil
}

func (w *Writer) fetchAllocationsByTransactions(ctx context.Context, transactionIDs []string) (map[string][]TransactionAllocation, error) {
	if len(transactionIDs) == 0 {
		return map[string][]TransactionAllocation{}, nil
	}
	rows, err := w.pool.Query(ctx, `
		SELECT a."transactionId", a."projectId", COALESCE(p.name, ''), a.amount::float8, a.percent::float8
		FROM transaction_project_allocations a
		LEFT JOIN projects p ON p.id = a."projectId"
		WHERE a."transactionId" = ANY($1)
		ORDER BY p.name ASC`, transactionIDs)
	if err != nil {
		return nil, fmt.Errorf("ledger allocations: list: %w", err)
	}
	defer rows.Close()
	out := map[string][]TransactionAllocation{}
	for rows.Next() {
		var txID string
		var a TransactionAllocation
		if err := rows.Scan(&txID, &a.ProjectID, &a.ProjectName, &a.Amount, &a.Percent); err != nil {
			return nil, err
		}
		a.AmountText = formatMoney(a.Amount)
		out[txID] = append(out[txID], a)
	}
	return out, rows.Err()
}

func validateTransactionUpdate(in TransactionUpdateInput) error {
	if in.UserID == "" || in.Namespace == "" || in.TransactionID == "" {
		return fmt.Errorf("ledger tx update: userID namespace and transactionID required")
	}
	if in.Type != "INCOME" && in.Type != "EXPENSE" && in.Type != "TRANSFER" && in.Type != "SAVING" && in.Type != "DEBT" {
		return fmt.Errorf("ledger tx update: unsupported type")
	}
	if in.Type != "INCOME" && in.Type != "EXPENSE" {
		return fmt.Errorf("ledger tx update: only income and expense are editable in this MVP")
	}
	if in.Amount <= 0 {
		return fmt.Errorf("ledger tx update: amount must be positive")
	}
	if strings.TrimSpace(in.Category) == "" {
		return fmt.Errorf("ledger tx update: category required")
	}
	if in.HappenedAt.IsZero() {
		return fmt.Errorf("ledger tx update: happenedAt required")
	}
	return nil
}

func validateAllocations(ctx context.Context, tx txer, userID, namespace string, txAmount float64, allocations []AllocationInput) error {
	seen := map[string]bool{}
	sum := 0.0
	for _, a := range allocations {
		projectID := strings.TrimSpace(a.ProjectID)
		if projectID == "" || a.Amount <= 0 {
			continue
		}
		if seen[projectID] {
			return fmt.Errorf("ledger allocations: duplicate project")
		}
		seen[projectID] = true
		sum += a.Amount
		var exists bool
		if err := tx.QueryRow(ctx, `
			SELECT true
			FROM projects
			WHERE id = $1 AND "userId" = $2 AND namespace = $3 AND "deletedAt" IS NULL
			LIMIT 1`, projectID, userID, namespace).Scan(&exists); err != nil {
			if err == pgx.ErrNoRows {
				return fmt.Errorf("ledger allocations: project not found")
			}
			return fmt.Errorf("ledger allocations: project lookup: %w", err)
		}
	}
	if sum-txAmount > 0.01 {
		return fmt.Errorf("ledger allocations: allocated amount exceeds transaction amount")
	}
	return nil
}

func insertAllocations(ctx context.Context, tx txer, userID, namespace, transactionID string, txAmount float64, allocations []AllocationInput) error {
	for _, a := range allocations {
		projectID := strings.TrimSpace(a.ProjectID)
		if projectID == "" || a.Amount <= 0 {
			continue
		}
		percent := 0.0
		if txAmount > 0 {
			percent = math.Round((a.Amount/txAmount*100)*10000) / 10000
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO transaction_project_allocations (namespace, "userId", "transactionId", "projectId", amount, percent, note, "createdAt", "updatedAt")
			VALUES ($1, $2, $3, $4, $5::numeric, $6::numeric, $7, NOW(), NOW())`,
			namespace, userID, transactionID, projectID, a.Amount, percent, nullableString(strings.TrimSpace(a.Note))); err != nil {
			return fmt.Errorf("ledger allocations: insert: %w", err)
		}
	}
	return nil
}

func insertLedgerEventTx(ctx context.Context, tx txer, namespace, userID, eventType string, payload map[string]any) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("ledger event: marshal: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO ledger_events (namespace, "userId", type, payload, "occurredAt")
		VALUES ($1, $2, $3, $4::jsonb, NOW())`,
		namespace, userID, eventType, string(payloadBytes)); err != nil {
		return fmt.Errorf("ledger event: insert: %w", err)
	}
	return nil
}

func (w *Writer) appendLedgerEvent(ctx context.Context, namespace, userID, eventType string, payload map[string]any) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = w.pool.Exec(ctx, `
		INSERT INTO ledger_events (namespace, "userId", type, payload, "occurredAt")
		VALUES ($1, $2, $3, $4::jsonb, NOW())`,
		namespace, userID, eventType, string(payloadBytes))
	return err
}

func cleanColor(color string) string {
	color = strings.TrimSpace(color)
	if len(color) != 7 || color[0] != '#' {
		return "#0f766e"
	}
	for _, r := range color[1:] {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return "#0f766e"
		}
	}
	return color
}

func decorateProjectSummary(p *ProjectSummary, target sql.NullTime) {
	p.Balance = p.Income - p.Expense
	p.IncomeText = formatMoney(p.Income)
	p.ExpenseText = formatMoney(p.Expense)
	p.BalanceText = formatMoney(p.Balance)
	if p.BudgetAmount > 0 {
		p.BudgetText = formatMoney(p.BudgetAmount)
		p.RemainingBudget = p.BudgetAmount - p.Expense
		p.RemainingBudgetText = formatMoney(p.RemainingBudget)
		p.BudgetUsedPct = roundPct(p.Expense * 100 / p.BudgetAmount)
	}
	if target.Valid {
		p.TargetDate = thaitime.ShortDate(target.Time)
		p.TargetDateInput = target.Time.In(thaitime.Location()).Format("2006-01-02")
	}
}

func nullablePositiveAmount(v float64) any {
	if v <= 0 {
		return nil
	}
	return v
}

func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

func mentionsProjectContext(text string) bool {
	clean := strings.ToLower(strings.TrimSpace(text))
	return strings.Contains(clean, "โครงการ") ||
		strings.Contains(clean, "โปรเจกต์") ||
		strings.Contains(clean, "project") ||
		strings.Contains(clean, "#")
}

func normalizeProjectKey(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		switch r {
		case '#', ':', '：', ',', ';', '/', '\\', '-', '–', '—', '(', ')', '[', ']', '{', '}', '"', '\'':
			return -1
		default:
			return r
		}
	}, text)
}

func extractExplicitProjectName(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	for _, marker := range []string{"โครงการ", "โปรเจกต์", "project", "#"} {
		if name := projectNameAfterMarker(text, marker); name != "" {
			return name
		}
	}
	return ""
}

func projectNameAfterMarker(text, marker string) string {
	lower := strings.ToLower(text)
	idx := strings.Index(lower, strings.ToLower(marker))
	if idx < 0 {
		return ""
	}
	segment := strings.TrimSpace(text[idx+len(marker):])
	segment = strings.Trim(segment, " \t\r\n:：-–—#")
	if segment == "" {
		return ""
	}
	for _, stop := range []string{"\n", "\r", ",", "，", ";", "|", " / ", " บาท", " จำนวน", " ค่า", " ซื้อ", " จ่าย", " รับ", " ราย"} {
		if i := strings.Index(segment, stop); i >= 0 {
			segment = segment[:i]
		}
	}
	words := strings.Fields(segment)
	if len(words) == 0 {
		return ""
	}
	nameWords := []string{}
	for _, word := range words {
		if containsDigit(word) || strings.Contains(word, "บาท") {
			break
		}
		nameWords = append(nameWords, word)
		if len(nameWords) >= 4 {
			break
		}
	}
	name := strings.TrimSpace(strings.Join(nameWords, " "))
	name = strings.Trim(name, " :：-–—#")
	if len([]rune(name)) > 80 {
		runes := []rune(name)
		name = strings.TrimSpace(string(runes[:80]))
	}
	generic := map[string]bool{
		"": true, "อะไร": true, "ไหน": true, "ใหม่": true, "นี้": true,
	}
	if generic[name] {
		return ""
	}
	return name
}

func containsDigit(text string) bool {
	for _, r := range text {
		if unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func splitProjectAllocations(projects []ProjectSummary, amount float64) []AllocationInput {
	if len(projects) == 0 || amount <= 0 {
		return nil
	}
	each := math.Floor((amount/float64(len(projects)))*100) / 100
	allocations := make([]AllocationInput, 0, len(projects))
	allocated := 0.0
	for i, p := range projects {
		v := each
		if i == len(projects)-1 {
			v = math.Round((amount-allocated)*100) / 100
		}
		if v <= 0 {
			continue
		}
		allocations = append(allocations, AllocationInput{
			ProjectID: p.ID,
			Amount:    v,
			Note:      "auto from chat",
		})
		allocated += v
	}
	return allocations
}
