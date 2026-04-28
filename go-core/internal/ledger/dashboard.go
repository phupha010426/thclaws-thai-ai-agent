package ledger

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/thaiaiagent/go-core/internal/thaitime"
)

type DashboardData struct {
	UpdatedAt       string                 `json:"updatedAt"`
	TodayLabel      string                 `json:"todayLabel"`
	MonthLabel      string                 `json:"monthLabel"`
	Range           DashboardRangeMeta     `json:"range"`
	Totals          DashboardTotals        `json:"totals"`
	Today           DashboardTotals        `json:"today"`
	Month           DashboardTotals        `json:"month"`
	Analytics       DashboardAnalytics     `json:"analytics"`
	TopCategories   []DashboardCategory    `json:"topCategories"`
	Cashflow        []DashboardCashflowDay `json:"cashflow"`
	Recent          []DashboardTransaction `json:"recent"`
	Projects        []ProjectSummary       `json:"projects"`
	LedgerCheck     LedgerCheck            `json:"ledgerCheck"`
	AccountantNotes []string               `json:"accountantNotes"`
}

type DashboardOptions struct {
	Now    time.Time
	From   time.Time
	To     time.Time
	Preset string
	Label  string
}

type DashboardRangeMeta struct {
	Preset string `json:"preset"`
	Label  string `json:"label"`
	From   string `json:"from"`
	To     string `json:"to"`
	Days   int    `json:"days"`
}

type DashboardTotals struct {
	Income         float64 `json:"income"`
	Expense        float64 `json:"expense"`
	Balance        float64 `json:"balance"`
	IncomeText     string  `json:"incomeText"`
	ExpenseText    string  `json:"expenseText"`
	BalanceText    string  `json:"balanceText"`
	SavingsRatePct float64 `json:"savingsRatePct"`
}

type DashboardCategory struct {
	Category string  `json:"category"`
	Amount   float64 `json:"amount"`
	Text     string  `json:"text"`
	SharePct float64 `json:"sharePct"`
}

type DashboardCashflowDay struct {
	Date        string  `json:"date"`
	Label       string  `json:"label"`
	Income      float64 `json:"income"`
	Expense     float64 `json:"expense"`
	Balance     float64 `json:"balance"`
	IncomeText  string  `json:"incomeText"`
	ExpenseText string  `json:"expenseText"`
	BalanceText string  `json:"balanceText"`
}

type DashboardTransaction struct {
	ID               string                  `json:"id"`
	Type             string                  `json:"type"`
	TypeLabel        string                  `json:"typeLabel"`
	Amount           float64                 `json:"amount"`
	AmountText       string                  `json:"amountText"`
	Category         string                  `json:"category"`
	Note             string                  `json:"note"`
	CounterpartyName string                  `json:"counterpartyName,omitempty"`
	CounterpartyRole string                  `json:"counterpartyRole,omitempty"`
	HappenedAt       string                  `json:"happenedAt"`
	HappenedAtInput  string                  `json:"happenedAtInput"`
	Allocations      []TransactionAllocation `json:"allocations,omitempty"`
}

type LedgerCheck struct {
	Debit    float64 `json:"debit"`
	Credit   float64 `json:"credit"`
	Balanced bool    `json:"balanced"`
	Text     string  `json:"text"`
}

type DashboardAnalytics struct {
	Score                     int                   `json:"score"`
	ScoreLabel                string                `json:"scoreLabel"`
	MonthProgressPct          float64               `json:"monthProgressPct"`
	ElapsedDays               int                   `json:"elapsedDays"`
	DaysInMonth               int                   `json:"daysInMonth"`
	DailyAverageExpense       float64               `json:"dailyAverageExpense"`
	DailyAverageExpenseText   string                `json:"dailyAverageExpenseText"`
	ProjectedMonthExpense     float64               `json:"projectedMonthExpense"`
	ProjectedMonthExpenseText string                `json:"projectedMonthExpenseText"`
	ExpensePaceText           string                `json:"expensePaceText"`
	WeekTrendPct              float64               `json:"weekTrendPct"`
	WeekTrendText             string                `json:"weekTrendText"`
	ActiveExpenseDays         int                   `json:"activeExpenseDays"`
	TransactionCount          int                   `json:"transactionCount"`
	LargestExpense            *DashboardTransaction `json:"largestExpense,omitempty"`
}

func (w *Writer) Dashboard(ctx context.Context, userID, namespace string, now time.Time) (DashboardData, error) {
	return w.DashboardWithRange(ctx, userID, namespace, DashboardOptions{Now: now})
}

func (w *Writer) DashboardWithRange(ctx context.Context, userID, namespace string, opt DashboardOptions) (DashboardData, error) {
	if userID == "" || namespace == "" {
		return DashboardData{}, fmt.Errorf("ledger dashboard: userID and namespace required")
	}
	now := opt.Now
	if now.IsZero() {
		now = thaitime.Now()
	}
	todayFrom, todayTo := LocalDayRange(now)
	rangeFrom, rangeTo, rangeLabel, preset := normalizeDashboardRange(opt, now)
	rangeFromText := rangeFrom.UTC().Format(time.RFC3339Nano)
	rangeToText := rangeTo.UTC().Format(time.RFC3339Nano)
	rangeDays := inclusiveDays(rangeFrom, rangeTo)

	totals, err := w.FetchUserTotals(ctx, userID, namespace)
	if err != nil {
		return DashboardData{}, err
	}
	todayTotals, err := w.fetchTotalsRange(ctx, userID, namespace, todayFrom, todayTo)
	if err != nil {
		return DashboardData{}, err
	}
	monthTotals, err := w.fetchTotalsRange(ctx, userID, namespace, rangeFromText, rangeToText)
	if err != nil {
		return DashboardData{}, err
	}
	categories, err := w.fetchExpenseCategories(ctx, userID, namespace, rangeFromText, rangeToText, monthTotals.Expense)
	if err != nil {
		return DashboardData{}, err
	}
	cashflow, err := w.fetchCashflowRange(ctx, userID, namespace, rangeFrom, rangeTo)
	if err != nil {
		return DashboardData{}, err
	}
	recent, err := w.fetchRecentEventsRange(ctx, userID, namespace, rangeFromText, rangeToText, 200)
	if err != nil {
		return DashboardData{}, err
	}
	projects, err := w.ListProjects(ctx, userID, namespace, rangeFromText, rangeToText)
	if err != nil {
		return DashboardData{}, err
	}
	check, err := w.fetchLedgerCheck(ctx, namespace, rangeFromText, rangeToText)
	if err != nil {
		return DashboardData{}, err
	}
	analytics, err := w.buildAnalytics(ctx, userID, namespace, now, rangeFrom, rangeTo, preset, rangeFromText, rangeToText, monthTotals, categories, cashflow, check)
	if err != nil {
		return DashboardData{}, err
	}

	data := DashboardData{
		UpdatedAt:  thaitime.ShortDateTime(now),
		TodayLabel: thaitime.ShortDate(now),
		MonthLabel: rangeLabel,
		Range: DashboardRangeMeta{
			Preset: preset,
			Label:  rangeLabel,
			From:   rangeFrom.In(thaitime.Location()).Format("2006-01-02"),
			To:     rangeTo.In(thaitime.Location()).Format("2006-01-02"),
			Days:   rangeDays,
		},
		Totals:        decorateTotals(totals.Income, totals.Expense),
		Today:         decorateTotals(todayTotals.Income, todayTotals.Expense),
		Month:         decorateTotals(monthTotals.Income, monthTotals.Expense),
		Analytics:     analytics,
		TopCategories: categories,
		Cashflow:      cashflow,
		Recent:        recent,
		Projects:      projects,
		LedgerCheck:   check,
	}
	data.AccountantNotes = buildDashboardNotes(data)
	return data, nil
}

func (w *Writer) fetchTotalsRange(ctx context.Context, userID, namespace, from, to string) (BalanceTotals, error) {
	var totals BalanceTotals
	err := w.pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN type = 'INCOME' THEN amount ELSE 0 END), 0)::float8,
			COALESCE(SUM(CASE WHEN type = 'EXPENSE' THEN amount ELSE 0 END), 0)::float8
		FROM transactions
		WHERE "userId" = $1 AND namespace = $2 AND "deletedAt" IS NULL
		  AND "happenedAt" BETWEEN $3::timestamptz AND $4::timestamptz`,
		userID, namespace, from, to,
	).Scan(&totals.Income, &totals.Expense)
	if err != nil {
		return BalanceTotals{}, fmt.Errorf("ledger dashboard: range totals: %w", err)
	}
	return totals, nil
}

func (w *Writer) fetchExpenseCategories(ctx context.Context, userID, namespace, from, to string, totalExpense float64) ([]DashboardCategory, error) {
	rows, err := w.pool.Query(ctx, `
		SELECT category, COALESCE(SUM(amount), 0)::float8
		FROM transactions
		WHERE "userId" = $1 AND namespace = $2 AND "deletedAt" IS NULL AND type = 'EXPENSE'
		  AND "happenedAt" BETWEEN $3::timestamptz AND $4::timestamptz
		GROUP BY category
		ORDER BY SUM(amount) DESC, category ASC
		LIMIT 8`, userID, namespace, from, to)
	if err != nil {
		return nil, fmt.Errorf("ledger dashboard: categories: %w", err)
	}
	defer rows.Close()

	out := []DashboardCategory{}
	for rows.Next() {
		var c DashboardCategory
		if err := rows.Scan(&c.Category, &c.Amount); err != nil {
			return nil, err
		}
		c.Text = formatMoney(c.Amount)
		if totalExpense > 0 {
			c.SharePct = roundPct(c.Amount * 100 / totalExpense)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (w *Writer) fetchCashflow(ctx context.Context, userID, namespace string, now time.Time, days int) ([]DashboardCashflowDay, error) {
	if days <= 0 {
		days = 14
	}
	loc := thaitime.Location()
	t := now.In(loc)
	start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(days - 1))
	end := start.AddDate(0, 0, days).Add(-time.Nanosecond)

	points := make([]DashboardCashflowDay, 0, days)
	index := map[string]int{}
	for i := 0; i < days; i++ {
		day := start.AddDate(0, 0, i)
		key := day.Format("2006-01-02")
		index[key] = i
		points = append(points, DashboardCashflowDay{
			Date:  key,
			Label: thaitime.ShortDate(day),
		})
	}

	rows, err := w.pool.Query(ctx, `
		SELECT to_char(("happenedAt" AT TIME ZONE 'Asia/Bangkok')::date, 'YYYY-MM-DD') AS day_key,
		       type,
		       COALESCE(SUM(amount), 0)::float8
		FROM transactions
		WHERE "userId" = $1 AND namespace = $2 AND "deletedAt" IS NULL
		  AND "happenedAt" BETWEEN $3::timestamptz AND $4::timestamptz
		GROUP BY day_key, type`, userID, namespace, start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("ledger dashboard: cashflow: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var dayKey, txType string
		var amount float64
		if err := rows.Scan(&dayKey, &txType, &amount); err != nil {
			return nil, err
		}
		i, ok := index[dayKey]
		if !ok {
			continue
		}
		switch txType {
		case "INCOME":
			points[i].Income = amount
		case "EXPENSE":
			points[i].Expense = amount
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range points {
		points[i].Balance = points[i].Income - points[i].Expense
		points[i].IncomeText = formatMoney(points[i].Income)
		points[i].ExpenseText = formatMoney(points[i].Expense)
		points[i].BalanceText = formatMoney(points[i].Balance)
	}
	return points, nil
}

func (w *Writer) fetchCashflowRange(ctx context.Context, userID, namespace string, from, to time.Time) ([]DashboardCashflowDay, error) {
	loc := thaitime.Location()
	start := localDayStart(from)
	end := endOfLocalDay(to)
	days := inclusiveDays(start, end)
	if days <= 0 {
		days = 1
	}
	if days > 366 {
		start = localDayStart(end.AddDate(-1, 0, 1))
		days = inclusiveDays(start, end)
	}

	points := make([]DashboardCashflowDay, 0, days)
	index := map[string]int{}
	for i := 0; i < days; i++ {
		day := start.In(loc).AddDate(0, 0, i)
		key := day.Format("2006-01-02")
		index[key] = i
		points = append(points, DashboardCashflowDay{
			Date:  key,
			Label: thaitime.ShortDate(day),
		})
	}

	rows, err := w.pool.Query(ctx, `
		SELECT to_char(("happenedAt" AT TIME ZONE 'Asia/Bangkok')::date, 'YYYY-MM-DD') AS day_key,
		       type,
		       COALESCE(SUM(amount), 0)::float8
		FROM transactions
		WHERE "userId" = $1 AND namespace = $2 AND "deletedAt" IS NULL
		  AND "happenedAt" BETWEEN $3::timestamptz AND $4::timestamptz
		GROUP BY day_key, type`, userID, namespace, start.UTC().Format(time.RFC3339Nano), end.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("ledger dashboard: cashflow range: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var dayKey, txType string
		var amount float64
		if err := rows.Scan(&dayKey, &txType, &amount); err != nil {
			return nil, err
		}
		i, ok := index[dayKey]
		if !ok {
			continue
		}
		switch txType {
		case "INCOME":
			points[i].Income = amount
		case "EXPENSE":
			points[i].Expense = amount
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range points {
		points[i].Balance = points[i].Income - points[i].Expense
		points[i].IncomeText = formatMoney(points[i].Income)
		points[i].ExpenseText = formatMoney(points[i].Expense)
		points[i].BalanceText = formatMoney(points[i].Balance)
	}
	return points, nil
}

func (w *Writer) fetchRecentEvents(ctx context.Context, userID, namespace string, limit int) ([]DashboardTransaction, error) {
	if limit <= 0 {
		limit = 12
	}
	rows, err := w.pool.Query(ctx, `
		SELECT id, type, amount::float8, category, COALESCE(note, ''),
		       COALESCE("counterpartyName", ''), COALESCE("counterpartyRole", ''), "happenedAt"
		FROM transactions
		WHERE "userId" = $1 AND namespace = $2 AND "deletedAt" IS NULL
		ORDER BY "happenedAt" DESC
		LIMIT $3`, userID, namespace, limit)
	if err != nil {
		return nil, fmt.Errorf("ledger dashboard: recent events: %w", err)
	}
	defer rows.Close()

	out := []DashboardTransaction{}
	ids := []string{}
	for rows.Next() {
		var item DashboardTransaction
		var happened time.Time
		if err := rows.Scan(&item.ID, &item.Type, &item.Amount, &item.Category, &item.Note, &item.CounterpartyName, &item.CounterpartyRole, &happened); err != nil {
			return nil, err
		}
		item.TypeLabel = typeLabel(item.Type)
		item.AmountText = formatMoney(item.Amount)
		item.HappenedAt = thaitime.ShortDateTime(happened)
		item.HappenedAtInput = happened.In(thaitime.Location()).Format("2006-01-02T15:04")
		out = append(out, item)
		ids = append(ids, item.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	allocs, err := w.fetchAllocationsByTransactions(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Allocations = allocs[out[i].ID]
	}
	return out, nil
}

func (w *Writer) fetchRecentEventsRange(ctx context.Context, userID, namespace, from, to string, limit int) ([]DashboardTransaction, error) {
	if limit <= 0 {
		limit = 24
	}
	rows, err := w.pool.Query(ctx, `
		SELECT id, type, amount::float8, category, COALESCE(note, ''),
		       COALESCE("counterpartyName", ''), COALESCE("counterpartyRole", ''), "happenedAt"
		FROM transactions
		WHERE "userId" = $1 AND namespace = $2 AND "deletedAt" IS NULL
		  AND "happenedAt" BETWEEN $3::timestamptz AND $4::timestamptz
		ORDER BY "happenedAt" DESC
		LIMIT $5`, userID, namespace, from, to, limit)
	if err != nil {
		return nil, fmt.Errorf("ledger dashboard: recent events range: %w", err)
	}
	defer rows.Close()

	out := []DashboardTransaction{}
	ids := []string{}
	for rows.Next() {
		var item DashboardTransaction
		var happened time.Time
		if err := rows.Scan(&item.ID, &item.Type, &item.Amount, &item.Category, &item.Note, &item.CounterpartyName, &item.CounterpartyRole, &happened); err != nil {
			return nil, err
		}
		item.TypeLabel = typeLabel(item.Type)
		item.AmountText = formatMoney(item.Amount)
		item.HappenedAt = thaitime.ShortDateTime(happened)
		item.HappenedAtInput = happened.In(thaitime.Location()).Format("2006-01-02T15:04")
		out = append(out, item)
		ids = append(ids, item.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	allocs, err := w.fetchAllocationsByTransactions(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Allocations = allocs[out[i].ID]
	}
	return out, nil
}

func (w *Writer) fetchLedgerCheck(ctx context.Context, namespace, from, to string) (LedgerCheck, error) {
	var check LedgerCheck
	err := w.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(jl.debit), 0)::float8, COALESCE(SUM(jl.credit), 0)::float8
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl."journalEntryId"
		WHERE je.namespace = $1 AND je.status = 'POSTED'
		  AND je."occurredAt" BETWEEN $2::timestamptz AND $3::timestamptz`,
		namespace, from, to,
	).Scan(&check.Debit, &check.Credit)
	if err != nil {
		return LedgerCheck{}, fmt.Errorf("ledger dashboard: ledger check: %w", err)
	}
	check.Balanced = math.Abs(check.Debit-check.Credit) < 0.01
	if check.Balanced {
		check.Text = "บัญชีแยกประเภทช่วงที่เลือกสมดุล เดบิตเท่ากับเครดิต"
	} else {
		check.Text = "บัญชีแยกประเภทช่วงที่เลือกยังไม่สมดุล ควรตรวจรายการล่าสุด"
	}
	return check, nil
}

func (w *Writer) buildAnalytics(ctx context.Context, userID, namespace string, now, fromTime, toTime time.Time, preset, from, to string, monthTotals BalanceTotals, categories []DashboardCategory, cashflow []DashboardCashflowDay, check LedgerCheck) (DashboardAnalytics, error) {
	loc := thaitime.Location()
	t := now.In(loc)
	daysInMonth := inclusiveDays(fromTime, toTime)
	elapsedDays := daysInMonth
	if preset == "this_month" {
		daysInMonth = time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, loc).Day()
		elapsedDays = t.Day()
	}
	if elapsedDays <= 0 {
		elapsedDays = 1
	}
	dailyAvg := monthTotals.Expense / float64(elapsedDays)
	projected := dailyAvg * float64(daysInMonth)

	txCount, activeDays, largest, err := w.fetchMonthActivity(ctx, userID, namespace, from, to)
	if err != nil {
		return DashboardAnalytics{}, err
	}
	recent7, previous7 := splitCashflowExpense(cashflow)
	trendPct := 0.0
	trendText := "ยังไม่มีข้อมูลเปรียบเทียบ 7 วัน"
	switch {
	case previous7 > 0:
		trendPct = roundPct((recent7 - previous7) * 100 / previous7)
		if trendPct > 10 {
			trendText = fmt.Sprintf("7 วันล่าสุดใช้เพิ่มขึ้น %s%% จาก 7 วันก่อน", formatMoney(trendPct))
		} else if trendPct < -10 {
			trendText = fmt.Sprintf("7 วันล่าสุดใช้ลดลง %s%% จาก 7 วันก่อน", formatMoney(math.Abs(trendPct)))
		} else {
			trendText = "7 วันล่าสุดใกล้เคียงกับ 7 วันก่อน"
		}
	case recent7 > 0:
		trendText = "เริ่มมีรายจ่ายใน 7 วันล่าสุดแล้ว"
	}

	score := 100
	if monthTotals.Income == 0 && monthTotals.Expense == 0 {
		score = 60
	} else {
		if monthTotals.Income > 0 && monthTotals.Expense > monthTotals.Income {
			score -= 35
		}
		if monthTotals.Income > 0 && monthTotals.Income-monthTotals.Expense < monthTotals.Income*0.1 {
			score -= 15
		}
		if monthTotals.Income > 0 && projected > monthTotals.Income {
			score -= 20
		}
		if len(categories) > 0 && categories[0].SharePct >= 50 {
			score -= 10
		}
		if !check.Balanced && (check.Debit > 0 || check.Credit > 0) {
			score -= 15
		}
		if activeDays == 0 && monthTotals.Expense > 0 {
			score -= 5
		}
	}
	score = clampInt(score, 0, 100)

	return DashboardAnalytics{
		Score:                     score,
		ScoreLabel:                scoreLabel(score),
		MonthProgressPct:          roundPct(float64(elapsedDays) * 100 / float64(daysInMonth)),
		ElapsedDays:               elapsedDays,
		DaysInMonth:               daysInMonth,
		DailyAverageExpense:       dailyAvg,
		DailyAverageExpenseText:   formatMoney(dailyAvg),
		ProjectedMonthExpense:     projected,
		ProjectedMonthExpenseText: formatMoney(projected),
		ExpensePaceText:           expensePaceText(monthTotals, projected),
		WeekTrendPct:              trendPct,
		WeekTrendText:             trendText,
		ActiveExpenseDays:         activeDays,
		TransactionCount:          txCount,
		LargestExpense:            largest,
	}, nil
}

func (w *Writer) fetchMonthActivity(ctx context.Context, userID, namespace, from, to string) (int, int, *DashboardTransaction, error) {
	rows, err := w.pool.Query(ctx, `
		SELECT id, type, amount::float8, category, COALESCE(note, ''), "happenedAt"
		FROM transactions
		WHERE "userId" = $1 AND namespace = $2 AND "deletedAt" IS NULL
		  AND "happenedAt" BETWEEN $3::timestamptz AND $4::timestamptz
		ORDER BY "happenedAt" DESC`, userID, namespace, from, to)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("ledger dashboard: month activity: %w", err)
	}
	defer rows.Close()
	count := 0
	expenseDays := map[string]bool{}
	var largest *DashboardTransaction
	for rows.Next() {
		var item DashboardTransaction
		var happened time.Time
		if err := rows.Scan(&item.ID, &item.Type, &item.Amount, &item.Category, &item.Note, &happened); err != nil {
			return 0, 0, nil, err
		}
		count++
		item.TypeLabel = typeLabel(item.Type)
		item.AmountText = formatMoney(item.Amount)
		item.HappenedAt = thaitime.ShortDateTime(happened)
		item.HappenedAtInput = happened.In(thaitime.Location()).Format("2006-01-02T15:04")
		if item.Type == "EXPENSE" {
			expenseDays[thaitime.In(happened).Format("2006-01-02")] = true
			if largest == nil || item.Amount > largest.Amount {
				copy := item
				largest = &copy
			}
		}
	}
	if err := rows.Err(); err != nil {
		return 0, 0, nil, err
	}
	return count, len(expenseDays), largest, nil
}

func splitCashflowExpense(cashflow []DashboardCashflowDay) (recent7, previous7 float64) {
	for i, p := range cashflow {
		if i >= len(cashflow)-7 {
			recent7 += p.Expense
		} else if i >= len(cashflow)-14 {
			previous7 += p.Expense
		}
	}
	return recent7, previous7
}

func expensePaceText(monthTotals BalanceTotals, projected float64) string {
	if monthTotals.Expense == 0 {
		return "ยังไม่มีรายจ่ายเดือนนี้"
	}
	if monthTotals.Income <= 0 {
		return "ยังไม่มีรายรับเดือนนี้ ใช้ projection เพื่อดูจังหวะรายจ่ายก่อน"
	}
	if projected > monthTotals.Income {
		return "ถ้าใช้จังหวะนี้ต่อ สิ้นเดือนอาจจ่ายเกินรายรับ"
	}
	if projected <= monthTotals.Income*0.7 {
		return "จังหวะรายจ่ายยังอยู่ในโซนสบาย"
	}
	return "จังหวะรายจ่ายเริ่มเข้าใกล้รายรับ ต้องคุมหมวดใหญ่"
}

func normalizeDashboardRange(opt DashboardOptions, now time.Time) (time.Time, time.Time, string, string) {
	loc := thaitime.Location()
	now = now.In(loc)
	if !opt.From.IsZero() && !opt.To.IsZero() {
		from := localDayStart(opt.From)
		to := endOfLocalDay(opt.To)
		if to.Before(from) {
			from = localDayStart(now)
			to = endOfLocalDay(now)
		}
		label := strings.TrimSpace(opt.Label)
		if label == "" {
			label = thaitime.ShortDate(from) + " - " + thaitime.ShortDate(to)
		}
		preset := strings.TrimSpace(opt.Preset)
		if preset == "" {
			preset = "custom"
		}
		return from, to, label, preset
	}
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	to := endOfLocalDay(now)
	return from, to, "เดือนนี้", "this_month"
}

func localDayStart(t time.Time) time.Time {
	t = t.In(thaitime.Location())
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, thaitime.Location())
}

func endOfLocalDay(t time.Time) time.Time {
	return localDayStart(t).AddDate(0, 0, 1).Add(-time.Nanosecond)
}

func inclusiveDays(from, to time.Time) int {
	from = localDayStart(from)
	to = localDayStart(to)
	if to.Before(from) {
		return 0
	}
	return int(to.Sub(from).Hours()/24) + 1
}

func scoreLabel(score int) string {
	switch {
	case score >= 85:
		return "แข็งแรง"
	case score >= 70:
		return "คุมได้"
	case score >= 50:
		return "ต้องระวัง"
	default:
		return "ต้องรีบดู"
	}
}

func decorateTotals(income, expense float64) DashboardTotals {
	balance := income - expense
	out := DashboardTotals{
		Income:      income,
		Expense:     expense,
		Balance:     balance,
		IncomeText:  formatMoney(income),
		ExpenseText: formatMoney(expense),
		BalanceText: formatMoney(balance),
	}
	if income > 0 {
		out.SavingsRatePct = roundPct(balance * 100 / income)
	}
	return out
}

func buildDashboardNotes(data DashboardData) []string {
	notes := []string{}
	period := strings.TrimSpace(data.Range.Label)
	if period == "" {
		period = "ช่วงที่เลือก"
	}
	if data.Month.Income == 0 && data.Month.Expense == 0 {
		return []string{period + "ยังไม่มีรายการครับ เริ่มจากส่งในแชทว่า กาแฟ 60 หรือบันทึกในหน้านี้ได้เลย"}
	}
	if data.Month.Income > 0 && data.Month.Expense > data.Month.Income {
		notes = append(notes, period+"รายจ่ายสูงกว่ารายรับ ต้องระวังเงินสดไหลออก")
	}
	if data.Month.Income > 0 && data.Month.Balance > 0 {
		notes = append(notes, fmt.Sprintf("%sเหลือ %s บาท คิดเป็น %s%% ของรายรับ", period, formatMoney(data.Month.Balance), formatMoney(data.Month.SavingsRatePct)))
	}
	if data.Analytics.ProjectedMonthExpense > 0 {
		notes = append(notes, fmt.Sprintf("คาดการณ์รายจ่ายสิ้นเดือนประมาณ %s บาท ถ้าจังหวะใช้เงินยังเท่าเดิม", data.Analytics.ProjectedMonthExpenseText))
	}
	if len(data.TopCategories) > 0 {
		top := data.TopCategories[0]
		if top.SharePct >= 40 {
			notes = append(notes, fmt.Sprintf("รายจ่ายหนักสุดคือ %s %s%% ของรายจ่าย%s", top.Category, formatMoney(top.SharePct), period))
		} else {
			notes = append(notes, fmt.Sprintf("หมวดที่จ่ายมากสุดคือ %s %s บาท", top.Category, top.Text))
		}
	}
	if data.Today.Expense > 0 {
		notes = append(notes, fmt.Sprintf("วันนี้จ่ายไป %s บาทแล้ว", data.Today.ExpenseText))
	}
	if data.Analytics.WeekTrendText != "" {
		notes = append(notes, data.Analytics.WeekTrendText)
	}
	if data.Analytics.LargestExpense != nil {
		notes = append(notes, fmt.Sprintf("รายการจ่ายใหญ่สุด%sคือ %s %s บาท", period, noteOrCategory(*data.Analytics.LargestExpense), data.Analytics.LargestExpense.AmountText))
	}
	if data.LedgerCheck.Debit > 0 || data.LedgerCheck.Credit > 0 {
		notes = append(notes, data.LedgerCheck.Text)
	}
	if len(notes) == 0 {
		notes = append(notes, "ยังไม่มีสัญญาณผิดปกติจากรายการจริงที่บันทึกไว้ครับ")
	}
	return notes
}

func typeLabel(txType string) string {
	switch strings.ToUpper(txType) {
	case "INCOME":
		return "รายรับ"
	case "EXPENSE":
		return "รายจ่าย"
	default:
		return "รายการ"
	}
}

func noteOrCategory(item DashboardTransaction) string {
	note := strings.TrimSpace(item.Note)
	if note != "" {
		return note
	}
	if item.Category != "" {
		return item.Category
	}
	return "รายการ"
}

func roundPct(v float64) float64 {
	return math.Round(v*10) / 10
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
