// Package parser ports the deterministic Thai transaction parser from the Node service
// (src/modules/ledger/transaction-parser.service.ts) so that the Go core can answer
// the same kind of "กาแฟ 60 / สรุปวันนี้ / ช่วยเหลือ" intents without an LLM round trip.
package parser

import (
	"regexp"
	"strconv"
	"strings"
)

type Intent string

const (
	IntentRecordExpense   Intent = "record_expense"
	IntentRecordIncome    Intent = "record_income"
	IntentAskTodaySummary Intent = "ask_today_summary"
	IntentAskMonthSummary Intent = "ask_month_summary"
	IntentAskBalance      Intent = "ask_balance"
	IntentAskDate         Intent = "ask_date"
	IntentHelp            Intent = "help"
	IntentIgnore          Intent = "ignore"
	IntentUnknown         Intent = "unknown"
)

type TransactionType string

const (
	TxIncome  TransactionType = "INCOME"
	TxExpense TransactionType = "EXPENSE"
)

// Result is the union returned by Parse. When Intent is record_expense or
// record_income, the transaction fields are populated. Otherwise Reason may
// describe why the parser could not classify the input.
type Result struct {
	Intent           Intent          `json:"intent"`
	Type             TransactionType `json:"type,omitempty"`
	Amount           float64         `json:"amount,omitempty"`
	Category         string          `json:"category,omitempty"`
	Note             string          `json:"note,omitempty"`
	CounterpartyName string          `json:"counterpartyName,omitempty"`
	CounterpartyRole string          `json:"counterpartyRole,omitempty"` // from | to
	Confidence       float64         `json:"confidence,omitempty"`
	Reason           string          `json:"reason,omitempty"`
}

type rule struct {
	pattern  *regexp.Regexp
	category string
}

var (
	incomeRules = []rule{
		{regexp.MustCompile(`(?i)เงินเดือน|salary`), "เงินเดือน"},
		{regexp.MustCompile(`(?i)ขายของ|ขายได้|ยอดขาย|ลูกค้าโอน|รับเงิน`), "รายได้จากการขาย"},
		{regexp.MustCompile(`(?i)โบนัส|รายได้|ได้เงิน|ให้เงิน|ได้ตัง|ได้ตังค์`), "รายรับอื่น ๆ"},
	}
	expenseRules = []rule{
		{regexp.MustCompile(`(?i)ค่าไฟ|ไฟฟ้า|ค่าน้ำ|น้ำประปา`), "ค่าน้ำค่าไฟ"},
		{regexp.MustCompile(`(?i)น้ำมัน|รถ|แท็กซี่|รถเมล์|เดินทาง|ค่าโดยสาร`), "เดินทาง"},
		{regexp.MustCompile(`(?i)ซื้อของเข้าบ้าน|ของเข้าบ้าน|โลตัส|บิ๊กซี|ตลาด|ของใช้`), "ของใช้เข้าบ้าน"},
		{regexp.MustCompile(`(?i)กาแฟ|ชา|ข้าว|อาหาร|ก๋วยเตี๋ยว|น้ำดื่ม|ขนม|ผัก|หมู|ไก่|ไข่`), "อาหารและเครื่องดื่ม"},
		{regexp.MustCompile(`(?i)ค่าเทอม|เรียน|หนังสือ|โรงเรียน|การศึกษา`), "การศึกษา"},
		{regexp.MustCompile(`(?i)หมอ|ยา|โรงพยาบาล|สุขภาพ`), "สุขภาพ"},
	}

	helpExact         = regexp.MustCompile(`(?i)^(ช่วยเหลือ|help|วิธีใช้|ทำอะไรได้บ้าง|ทำไรได้บ้าง|มีอะไรบ้าง|ใช้ทำอะไร|ทำอะไรได้|service|service น่ะ|บริการ)$`)
	dateQuestion      = regexp.MustCompile(`(?i)^(วันนี้วันอะไร|วันนี้วันที่เท่าไร|วันนี้วันที่อะไร|วันอะไร|วันที่เท่าไร|วันที่อะไร)$`)
	todayExact        = regexp.MustCompile(`(?i)^(สรุปวันนี้|วันนี้ใช้ไปเท่าไร|วันนี้ใช้เท่าไร|วันนี้ใช้ไปกี่บาท)$`)
	todayLoose        = regexp.MustCompile(`(?i)(วันนี้.*(จ่าย|ใช้|หมด|เสีย|รายจ่าย|ค่าอะไร|ไปบ้าง|เท่าไร|กี่บาท)|สรุป.*วันนี้)`)
	monthExact        = regexp.MustCompile(`(?i)^(สรุปเดือนนี้|เดือนนี้ใช้ไปเท่าไร|เดือนนี้ใช้เท่าไร|เดือนนี้ใช้ไปกี่บาท)$`)
	monthLoose        = regexp.MustCompile(`(?i)(เดือนนี้.*(จ่าย|ใช้|หมด|เสีย|รายจ่าย|ค่าอะไร|ไปบ้าง|เท่าไร|กี่บาท)|สรุป.*เดือนนี้)`)
	accountSummaryAsk = regexp.MustCompile(`(?i)^(สรุปบัญชี|ภาพรวมบัญชี|รายงานบัญชี|สรุปการเงิน|ภาพรวมการเงิน)$`)
	incomeExpenseAsk  = regexp.MustCompile(`(?i)(สรุป)?รายรับ.*รายจ่าย|รายรับรายจ่าย|รับจ่าย.*เท่าไร|รับเข้า.*จ่ายออก`)
	balanceQuestion   = regexp.MustCompile(`(?i)(เงินเหลือ|เหลือเงิน|ยอดคงเหลือ|คงเหลือเท่าไร|คงเหลือเท่าไหร่|เหลือเท่าไร|เหลือเท่าไหร่|balance)`)
	expenseLeadingTok = regexp.MustCompile(`^(ซื้อ|จ่าย|ค่า)`)
	forcedExpense     = regexp.MustCompile(`^(รายจ่าย|ค่าใช้จ่าย)\s*`)
	forcedIncome      = regexp.MustCompile(`^(รายรับ|รายได้)\s*`)
	ignoreExact       = regexp.MustCompile(`^(โอนเฉยๆ|โอนเฉย ๆ|ไม่บันทึก|ยกเลิก)$`)
	amountPattern     = regexp.MustCompile(`(\d+(?:\.\d{1,2})?)`)
	thaiAmountPattern = regexp.MustCompile(`(ศูนย์|หนึ่ง|นึง|เอ็ด|สอง|สาม|สี่|ห้า|หก|เจ็ด|แปด|เก้า|สิบ|ยี่|ร้อย|พัน|หมื่น|แสน|ล้าน)+`)
	paidToPattern     = regexp.MustCompile(`(?:จ่ายให้|โอนให้)\s*([^\s\d]+(?:\s+[^\s\d]+){0,3})`)
	receivedFromPat   = regexp.MustCompile(`(?:รับจาก|ได้เงินจาก|รับเงินจาก|รายรับจาก|ขายให้)\s*([^\s\d]+(?:\s+[^\s\d]+){0,3})`)
	giverPattern      = regexp.MustCompile(`^([^\s\d]+(?:\s+[^\s\d]+){0,2})\s*(?:ให้เงิน|ให้ตัง|ให้ตังค์|โอนให้|ให้มา)`)
)

// Parse mirrors transactionParserService.parse from the Node implementation.
func Parse(text string) Result {
	normalized := strings.TrimSpace(strings.ReplaceAll(text, ",", ""))
	if normalized == "" {
		return Result{Intent: IntentUnknown, Reason: "empty_text"}
	}

	if helpExact.MatchString(normalized) {
		return Result{Intent: IntentHelp}
	}
	if ignoreExact.MatchString(normalized) {
		return Result{Intent: IntentIgnore}
	}
	if dateQuestion.MatchString(normalized) {
		return Result{Intent: IntentAskDate}
	}
	if todayExact.MatchString(normalized) || todayLoose.MatchString(normalized) {
		return Result{Intent: IntentAskTodaySummary}
	}
	if monthExact.MatchString(normalized) || monthLoose.MatchString(normalized) {
		return Result{Intent: IntentAskMonthSummary}
	}
	if accountSummaryAsk.MatchString(normalized) {
		return Result{Intent: IntentAskBalance}
	}
	if incomeExpenseAsk.MatchString(normalized) {
		return Result{Intent: IntentAskBalance}
	}
	if balanceQuestion.MatchString(normalized) {
		return Result{Intent: IntentAskBalance}
	}

	match := amountPattern.FindStringSubmatch(normalized)
	amountText := ""
	amount := 0.0
	if len(match) >= 2 {
		amountText = match[1]
		parsed, err := strconv.ParseFloat(amountText, 64)
		if err != nil || parsed <= 0 {
			return Result{Intent: IntentUnknown, Reason: "invalid_amount"}
		}
		amount = parsed
	} else {
		thaiMatch := thaiAmountPattern.FindString(normalized)
		if thaiMatch == "" {
			return Result{Intent: IntentUnknown, Reason: "missing_amount"}
		}
		parsed, ok := parseThaiAmount(thaiMatch)
		if !ok || parsed <= 0 {
			return Result{Intent: IntentUnknown, Reason: "missing_amount"}
		}
		amountText = thaiMatch
		amount = parsed
	}
	if amountText == "" {
		return Result{Intent: IntentUnknown, Reason: "missing_amount"}
	}

	forceExpense := forcedExpense.MatchString(normalized)
	forceIncome := forcedIncome.MatchString(normalized)
	cleanText := strings.TrimSpace(forcedIncome.ReplaceAllString(forcedExpense.ReplaceAllString(normalized, ""), ""))

	incomeMatch := firstMatchingRule(cleanText, incomeRules)
	expenseMatch := firstMatchingRule(cleanText, expenseRules)

	isIncome := forceIncome || (incomeMatch != nil && !expenseLeadingTok.MatchString(normalized))
	if forceExpense {
		isIncome = false
	}

	var category string
	switch {
	case isIncome && incomeMatch != nil:
		category = incomeMatch.category
	case isIncome:
		category = "รายรับอื่น ๆ"
	case expenseMatch != nil:
		category = expenseMatch.category
	default:
		category = "อื่น ๆ"
	}

	note := strings.TrimSpace(strings.Replace(cleanText, amountText, "", 1))
	if note == "" {
		note = cleanText
	}
	counterpartyName, counterpartyRole := extractCounterparty(cleanText, isIncome)

	confidence := 0.95
	if category == "อื่น ๆ" || category == "รายรับอื่น ๆ" {
		confidence = 0.7
	}
	if forceExpense || forceIncome {
		confidence = 0.9
	}

	intent := IntentRecordExpense
	txType := TxExpense
	if isIncome {
		intent = IntentRecordIncome
		txType = TxIncome
	}

	return Result{
		Intent:           intent,
		Type:             txType,
		Amount:           amount,
		Category:         category,
		Note:             note,
		CounterpartyName: counterpartyName,
		CounterpartyRole: counterpartyRole,
		Confidence:       confidence,
	}
}

func extractCounterparty(text string, isIncome bool) (string, string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", ""
	}
	if match := receivedFromPat.FindStringSubmatch(text); len(match) >= 2 {
		return cleanCounterparty(match[1]), "from"
	}
	if match := giverPattern.FindStringSubmatch(text); len(match) >= 2 {
		return cleanCounterparty(match[1]), "from"
	}
	if match := paidToPattern.FindStringSubmatch(text); len(match) >= 2 {
		role := "to"
		if isIncome && strings.Contains(text, "ให้เงิน") {
			role = "from"
		}
		return cleanCounterparty(match[1]), role
	}
	return "", ""
}

func cleanCounterparty(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, " :：,，.。")
	stops := []string{"บาท", "ครับ", "ค่ะ", "นะ", "แล้ว", "เพิ่ม", "อีก"}
	parts := strings.Fields(value)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(part, " :：,，.。")
		stop := false
		for _, s := range stops {
			if part == s {
				stop = true
				break
			}
		}
		if stop {
			break
		}
		out = append(out, part)
	}
	return strings.Join(out, " ")
}

func parseThaiAmount(text string) (float64, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, false
	}
	tokens := thaiAmountTokens(text)
	if len(tokens) == 0 {
		return 0, false
	}

	total := 0
	current := 0
	lastWasUnit := false
	for _, tok := range tokens {
		switch tok {
		case "ล้าน":
			if current == 0 {
				current = 1
			}
			total += current * 1000000
			current = 0
			lastWasUnit = true
		case "แสน":
			if current == 0 || lastWasUnit {
				current += 100000
			} else {
				current *= 100000
			}
			lastWasUnit = true
		case "หมื่น":
			if current == 0 || lastWasUnit {
				current += 10000
			} else {
				current *= 10000
			}
			lastWasUnit = true
		case "พัน":
			if current == 0 || lastWasUnit {
				current += 1000
			} else {
				current *= 1000
			}
			lastWasUnit = true
		case "ร้อย":
			if current == 0 || lastWasUnit {
				current += 100
			} else {
				current *= 100
			}
			lastWasUnit = true
		case "สิบ":
			if current == 0 || lastWasUnit {
				current += 10
			} else {
				current *= 10
			}
			lastWasUnit = true
		case "ยี่":
			current += 2
			lastWasUnit = false
		default:
			d, ok := thaiDigit(tok)
			if !ok {
				return 0, false
			}
			current += d
			lastWasUnit = false
		}
	}
	return float64(total + current), total+current > 0
}

func thaiAmountTokens(text string) []string {
	var out []string
	for text != "" {
		matched := ""
		for _, tok := range []string{"ศูนย์", "หนึ่ง", "นึง", "เอ็ด", "สอง", "สาม", "สี่", "ห้า", "หก", "เจ็ด", "แปด", "เก้า", "สิบ", "ยี่", "ร้อย", "พัน", "หมื่น", "แสน", "ล้าน"} {
			if strings.HasPrefix(text, tok) {
				matched = tok
				break
			}
		}
		if matched == "" {
			break
		}
		out = append(out, matched)
		text = strings.TrimPrefix(text, matched)
	}
	return out
}

func thaiDigit(tok string) (int, bool) {
	switch tok {
	case "ศูนย์":
		return 0, true
	case "หนึ่ง", "นึง", "เอ็ด":
		return 1, true
	case "สอง":
		return 2, true
	case "สาม":
		return 3, true
	case "สี่":
		return 4, true
	case "ห้า":
		return 5, true
	case "หก":
		return 6, true
	case "เจ็ด":
		return 7, true
	case "แปด":
		return 8, true
	case "เก้า":
		return 9, true
	default:
		return 0, false
	}
}

func firstMatchingRule(text string, rules []rule) *rule {
	for i := range rules {
		if rules[i].pattern.MatchString(text) {
			return &rules[i]
		}
	}
	return nil
}
