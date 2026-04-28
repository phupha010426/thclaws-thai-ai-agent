package parser

import "testing"

func TestParseTransactionCases(t *testing.T) {
	cases := []struct {
		text     string
		intent   Intent
		txType   TransactionType
		amount   float64
		category string
	}{
		{"กาแฟ 60", IntentRecordExpense, TxExpense, 60, "อาหารและเครื่องดื่ม"},
		{"ข้าว 55", IntentRecordExpense, TxExpense, 55, "อาหารและเครื่องดื่ม"},
		{"ค่าไฟ 980", IntentRecordExpense, TxExpense, 980, "ค่าน้ำค่าไฟ"},
		{"ค่าน้ำ 120", IntentRecordExpense, TxExpense, 120, "ค่าน้ำค่าไฟ"},
		{"เงินเดือน 25000", IntentRecordIncome, TxIncome, 25000, "เงินเดือน"},
		{"ขายของ 1500", IntentRecordIncome, TxIncome, 1500, "รายได้จากการขาย"},
		{"ซื้อของเข้าบ้าน 1200", IntentRecordExpense, TxExpense, 1200, "ของใช้เข้าบ้าน"},
		{"น้ำมันรถ 700", IntentRecordExpense, TxExpense, 700, "เดินทาง"},
		{"ค่าหมอ 450", IntentRecordExpense, TxExpense, 450, "สุขภาพ"},
		{"โบนัส 3000", IntentRecordIncome, TxIncome, 3000, "รายรับอื่น ๆ"},
	}

	for _, c := range cases {
		c := c
		t.Run(c.text, func(t *testing.T) {
			got := Parse(c.text)
			if got.Intent != c.intent {
				t.Fatalf("intent = %q, want %q", got.Intent, c.intent)
			}
			if got.Type != c.txType {
				t.Fatalf("type = %q, want %q", got.Type, c.txType)
			}
			if got.Amount != c.amount {
				t.Fatalf("amount = %v, want %v", got.Amount, c.amount)
			}
			if got.Category != c.category {
				t.Fatalf("category = %q, want %q", got.Category, c.category)
			}
		})
	}
}

func TestParseMissingAmount(t *testing.T) {
	got := Parse("กาแฟ")
	if got.Intent != IntentUnknown {
		t.Fatalf("intent = %q, want %q", got.Intent, IntentUnknown)
	}
	if got.Reason != "missing_amount" {
		t.Fatalf("reason = %q, want missing_amount", got.Reason)
	}
}

func TestParseSummaryIntents(t *testing.T) {
	if Parse("สรุปวันนี้").Intent != IntentAskTodaySummary {
		t.Fatal("expected ask_today_summary")
	}
	if Parse("วันนี้จ่ายค่าอะไรไปบ้าง").Intent != IntentAskTodaySummary {
		t.Fatal("expected ask_today_summary (loose)")
	}
	if Parse("สรุปเดือนนี้").Intent != IntentAskMonthSummary {
		t.Fatal("expected ask_month_summary")
	}
	if Parse("เดือนนี้ใช้ไปเท่าไร").Intent != IntentAskMonthSummary {
		t.Fatal("expected ask_month_summary (exact)")
	}
	if Parse("ช่วยเหลือ").Intent != IntentHelp {
		t.Fatal("expected help")
	}
}

func TestParseDateQuestionIsNotSummary(t *testing.T) {
	got := Parse("วันนี้วันอะไร")
	if got.Intent != IntentAskDate {
		t.Fatalf("intent = %q, want %q", got.Intent, IntentAskDate)
	}
	if Parse("วันนี้วันที่เท่าไร").Intent != IntentAskDate {
		t.Fatal("expected ask_date")
	}
}

func TestParseBalanceQuestionIsNotMissingAmount(t *testing.T) {
	cases := []string{"เงินเหลือเท่าไหร่", "เหลือเงินเท่าไร", "ยอดคงเหลือ", "balance", "สรุปรายรับรายจ่าย", "รายรับรายจ่ายเท่าไหร่", "สรุปบัญชี"}
	for _, text := range cases {
		got := Parse(text)
		if got.Intent != IntentAskBalance {
			t.Fatalf("%q intent = %q, want %q: %+v", text, got.Intent, IntentAskBalance, got)
		}
	}
}

func TestParseCapabilityQuestions(t *testing.T) {
	cases := []string{"ทำอะไรได้บ้าง", "มีอะไรบ้าง", "service น่ะ"}
	for _, text := range cases {
		if got := Parse(text); got.Intent != IntentHelp {
			t.Fatalf("%q intent = %q, want help", text, got.Intent)
		}
	}
}

func TestParseQuickReplyChoices(t *testing.T) {
	expense := Parse("รายจ่าย โอน 500")
	if expense.Intent != IntentRecordExpense || expense.Type != TxExpense || expense.Amount != 500 {
		t.Fatalf("expense quick reply parsed wrong: %+v", expense)
	}
	if expense.Confidence < 0.75 {
		t.Fatalf("expense confidence = %v, want >= 0.75", expense.Confidence)
	}

	income := Parse("รายรับ โอน 500")
	if income.Intent != IntentRecordIncome || income.Type != TxIncome || income.Amount != 500 {
		t.Fatalf("income quick reply parsed wrong: %+v", income)
	}

	if Parse("ไม่บันทึก").Intent != IntentIgnore {
		t.Fatal("expected ignore")
	}
}

func TestParseThaiAmountWords(t *testing.T) {
	got := Parse("เมียให้เงินสองร้อย")
	if got.Intent != IntentRecordIncome {
		t.Fatalf("intent = %q, want %q: %+v", got.Intent, IntentRecordIncome, got)
	}
	if got.Amount != 200 {
		t.Fatalf("amount = %v, want 200", got.Amount)
	}
	if got.Confidence >= 0.75 {
		t.Fatalf("confidence = %v, want low confidence so dispatcher asks first", got.Confidence)
	}
	if got.CounterpartyName != "เมีย" || got.CounterpartyRole != "from" {
		t.Fatalf("counterparty = %q/%q, want เมีย/from", got.CounterpartyName, got.CounterpartyRole)
	}

	got = Parse("เมียให้เงินสองหมื่น")
	if got.Intent != IntentRecordIncome {
		t.Fatalf("intent = %q, want %q: %+v", got.Intent, IntentRecordIncome, got)
	}
	if got.Amount != 20000 {
		t.Fatalf("amount = %v, want 20000", got.Amount)
	}
}

func TestParseCounterpartyDetails(t *testing.T) {
	cases := []struct {
		text string
		name string
		role string
	}{
		{"จ่ายให้แม่ 500", "แม่", "to"},
		{"รับจากลูกค้า 1500", "ลูกค้า", "from"},
		{"โอนให้พี่แดง 800", "พี่แดง", "to"},
	}
	for _, c := range cases {
		got := Parse(c.text)
		if got.CounterpartyName != c.name || got.CounterpartyRole != c.role {
			t.Fatalf("%q counterparty = %q/%q, want %q/%q", c.text, got.CounterpartyName, got.CounterpartyRole, c.name, c.role)
		}
	}
}

func TestParseEmpty(t *testing.T) {
	got := Parse("")
	if got.Intent != IntentUnknown || got.Reason != "empty_text" {
		t.Fatalf("got %+v, want unknown/empty_text", got)
	}
}
