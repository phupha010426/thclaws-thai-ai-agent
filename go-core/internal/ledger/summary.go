package ledger

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/thaiaiagent/go-core/internal/thaitime"
)

// LocalRange returns Bangkok-day or Bangkok-month start/end timestamps so
// queries match the user's wall-clock day even though Postgres stores UTC.
func LocalDayRange(now time.Time) (string, string) {
	loc := thaitime.Location()
	t := now.In(loc)
	start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	end := start.Add(24*time.Hour - time.Nanosecond)
	return start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339)
}

func LocalMonthRange(now time.Time) (string, string) {
	loc := thaitime.Location()
	t := now.In(loc)
	start := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc)
	end := start.AddDate(0, 1, 0).Add(-time.Nanosecond)
	return start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339)
}

type SummaryReader struct{ writer *Writer }

func NewSummaryReader(writer *Writer) *SummaryReader { return &SummaryReader{writer: writer} }

func (s *SummaryReader) Today(ctx context.Context, userID, namespace string) (string, error) {
	now := thaitime.Now()
	from, to := LocalDayRange(now)
	rows, err := s.writer.FetchUserRange(ctx, userID, namespace, from, to)
	if err != nil {
		return "", err
	}
	return buildPeriodSummary("วันนี้ "+thaitime.ShortDate(now), rows, true), nil
}

func (s *SummaryReader) ThisMonth(ctx context.Context, userID, namespace string) (string, error) {
	now := thaitime.Now()
	from, to := LocalMonthRange(now)
	rows, err := s.writer.FetchUserRange(ctx, userID, namespace, from, to)
	if err != nil {
		return "", err
	}
	return buildPeriodSummary("เดือนนี้ "+monthLabel(now), rows, false), nil
}

func (s *SummaryReader) Balance(ctx context.Context, userID, namespace string) (string, error) {
	totals, err := s.writer.FetchUserTotals(ctx, userID, namespace)
	if err != nil {
		return "", err
	}
	balance := totals.Income - totals.Expense
	return fmt.Sprintf("ตามรายการที่บันทึกไว้ทั้งหมด รับเข้า %s บาท จ่ายออก %s บาท คงเหลือ %s บาทครับ",
		formatMoney(totals.Income), formatMoney(totals.Expense), formatMoney(balance)), nil
}

type catSum struct {
	Category string
	Amount   float64
}

func sumByType(rows []SummaryRow, t string) float64 {
	total := 0.0
	for _, r := range rows {
		if r.Type == t {
			total += r.Amount
		}
	}
	return total
}

func topExpenseCategory(rows []SummaryRow) *catSum {
	totals := map[string]float64{}
	for _, r := range rows {
		if r.Type == "EXPENSE" {
			totals[r.Category] += r.Amount
		}
	}
	var top *catSum
	for cat, amt := range totals {
		if top == nil || amt > top.Amount {
			top = &catSum{Category: cat, Amount: amt}
		}
	}
	return top
}

func buildPeriodSummary(label string, rows []SummaryRow, showTime bool) string {
	expense := sumByType(rows, "EXPENSE")
	income := sumByType(rows, "INCOME")
	balance := income - expense
	var b strings.Builder
	b.WriteString(label)
	b.WriteString("\nรับเข้า ")
	b.WriteString(formatMoney(income))
	b.WriteString(" บาท | จ่ายออก ")
	b.WriteString(formatMoney(expense))
	b.WriteString(" บาท | คงเหลือ ")
	b.WriteString(formatMoney(balance))
	b.WriteString(" บาท")

	expenseLines := filterByType(rows, "EXPENSE")
	incomeLines := filterByType(rows, "INCOME")
	if len(expenseLines) == 0 && len(incomeLines) == 0 {
		b.WriteString("\nยังไม่มีรายการที่บันทึกไว้ในช่วงนี้ครับ")
		return b.String()
	}
	if len(expenseLines) > 0 {
		b.WriteString("\nรายจ่าย:")
		appendRows(&b, expenseLines, showTime, 8)
	}
	if len(incomeLines) > 0 {
		b.WriteString("\nรายรับ:")
		appendRows(&b, incomeLines, showTime, 5)
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
	return b.String()
}

func appendRows(b *strings.Builder, rows []SummaryRow, showTime bool, limit int) {
	for i, r := range rows {
		if i >= limit {
			b.WriteString(fmt.Sprintf("\n- และอีก %s รายการ", formatInt(int64(len(rows)-limit))))
			return
		}
		label := thaitime.ShortDate(r.HappenedAt)
		if showTime {
			label = thaitime.Time(r.HappenedAt)
		}
		note := strings.TrimSpace(r.Note)
		if note == "" {
			note = r.Category
		}
		b.WriteString("\n- ")
		b.WriteString(label)
		b.WriteString(" ")
		b.WriteString(note)
		b.WriteString(" ")
		b.WriteString(formatMoney(r.Amount))
		b.WriteString(" บาท")
		if r.Category != "" && r.Category != note {
			b.WriteString(" (")
			b.WriteString(r.Category)
			b.WriteString(")")
		}
	}
}

func filterByType(rows []SummaryRow, txType string) []SummaryRow {
	out := make([]SummaryRow, 0, len(rows))
	for _, r := range rows {
		if r.Type == txType {
			out = append(out, r)
		}
	}
	return out
}

func categoryTotals(rows []SummaryRow) []catSum {
	totals := map[string]float64{}
	for _, r := range rows {
		if r.Category != "" {
			totals[r.Category] += r.Amount
		}
	}
	out := make([]catSum, 0, len(totals))
	for cat, amt := range totals {
		out = append(out, catSum{Category: cat, Amount: amt})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Amount == out[j].Amount {
			return out[i].Category < out[j].Category
		}
		return out[i].Amount > out[j].Amount
	})
	return out
}

func monthLabel(t time.Time) string {
	t = thaitime.In(t)
	return fmt.Sprintf("%s %02d", thaiMonthShort(t.Month()), (t.Year()+543)%100)
}

func thaiMonthShort(m time.Month) string {
	names := []string{"", "ม.ค.", "ก.พ.", "มี.ค.", "เม.ย.", "พ.ค.", "มิ.ย.", "ก.ค.", "ส.ค.", "ก.ย.", "ต.ค.", "พ.ย.", "ธ.ค."}
	return names[int(m)]
}

func formatMoney(value float64) string {
	if value == float64(int64(value)) {
		return formatInt(int64(value))
	}
	negative := value < 0
	if negative {
		value = -value
	}
	parts := strings.SplitN(fmt.Sprintf("%.2f", value), ".", 2)
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return fmt.Sprintf("%.2f", value)
	}
	out := formatInt(whole)
	if len(parts) == 2 {
		frac := strings.TrimRight(parts[1], "0")
		if frac != "" {
			out += "." + frac
		}
	}
	if negative && out != "0" {
		out = "-" + out
	}
	return out
}

func FormatMoney(value float64) string {
	return formatMoney(value)
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
