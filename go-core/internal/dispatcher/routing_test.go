package dispatcher

import "testing"

func TestIsAmbiguousFollowUpDoesNotCatchFullAccountingSentence(t *testing.T) {
	if isAmbiguousFollowUp("เมียให้เงินสองหมื่น") {
		t.Fatal("full accounting sentence must be parsed, not treated as amount-only follow-up")
	}
}

func TestIsAmbiguousFollowUpCatchesAmountOnly(t *testing.T) {
	cases := []string{"200", "200 บาท", "สองหมื่น", "สองหมื่นบาท"}
	for _, c := range cases {
		if !isAmbiguousFollowUp(c) {
			t.Fatalf("%q should be amount-only follow-up", c)
		}
	}
}

func TestAdviceRequestsDoNotBecomeMissingAmountAccounting(t *testing.T) {
	cases := []string{
		"สูตรหุงข้าวทำซูชิ",
		"ควรวางแผนการออมเงินยังไง",
		"แนะนำวิธีประหยัดค่าใช้จ่าย",
	}
	for _, c := range cases {
		if looksAccountingRelated(c) {
			t.Fatalf("%q should not be treated as missing-amount accounting", c)
		}
	}
}

func TestFinancialAdviceRequestDetection(t *testing.T) {
	cases := []string{
		"ควรวางแผนการออมเงินยังไง",
		"ช่วยดูงบประมาณให้หน่อย",
		"แนะนำการจัดการหนี้",
	}
	for _, c := range cases {
		if !isFinancialAdviceRequest(c) {
			t.Fatalf("%q should be financial advice request", c)
		}
	}
}

func TestMiniAppRequestDetection(t *testing.T) {
	cases := []string{
		"เปิดสมุดบัญชี",
		"ขอดูกราฟรายจ่าย",
		"เปิด mini app ให้หน่อย",
	}
	for _, c := range cases {
		if !wantsMiniApp(c) {
			t.Fatalf("%q should open mini app", c)
		}
	}
}
