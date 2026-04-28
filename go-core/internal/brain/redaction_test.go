package brain

import (
	"strings"
	"testing"
)

func TestRedactPii_ThaiNationalID(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"เลขบัตร 1-2345-67890-12-3 ของฉัน", "เลขบัตร <id> ของฉัน"},
		{"ID: 1234567890123 นะ", "ID: <id> นะ"},
		// Hyphenated groups
		{"รหัส 3-1005-00001-23-4", "รหัส <id>"},
	}
	for _, c := range cases {
		got := RedactPii(c.input)
		if got != c.want {
			t.Errorf("ThaiID: input=%q got=%q want=%q", c.input, got, c.want)
		}
	}
}

func TestRedactPii_BankAccount(t *testing.T) {
	got := RedactPii("โอนไปที่ 123-4-56789-1 ด้วยนะ")
	if !strings.Contains(got, "<acc>") {
		t.Errorf("BankAccount: expected <acc> in %q", got)
	}
}

func TestRedactPii_Phone(t *testing.T) {
	cases := []string{
		"โทร 081-234-5678",
		"เบอร์ 02 345 6789",
		"ติดต่อ 0812345678",
	}
	for _, input := range cases {
		got := RedactPii(input)
		if !strings.Contains(got, "<phone>") {
			t.Errorf("Phone: expected <phone> in %q (input=%q)", got, input)
		}
	}
}

func TestRedactPii_Email(t *testing.T) {
	got := RedactPii("ส่งมาที่ user.name+tag@example.co.th ได้เลย")
	if !strings.Contains(got, "<email>") {
		t.Errorf("Email: expected <email> in %q", got)
	}
	if strings.Contains(got, "@") {
		t.Errorf("Email: raw @ still present in %q", got)
	}
}

func TestRedactPii_CreditCard(t *testing.T) {
	cases := []string{
		"บัตร 4111 1111 1111 1111",
		"เลข 4111-1111-1111-1111",
	}
	for _, input := range cases {
		got := RedactPii(input)
		if !strings.Contains(got, "<cc>") {
			t.Errorf("CC: expected <cc> in %q (input=%q)", got, input)
		}
	}
}

func TestRedactPii_NoMatch(t *testing.T) {
	input := "วันนี้กินข้าวที่ร้านอร่อยมาก ราคา 120 บาท"
	if RedactPii(input) != input {
		t.Errorf("NoMatch: clean text should be unchanged, got %q", RedactPii(input))
	}
}

func TestContainsPii_True(t *testing.T) {
	if !ContainsPii("email: foo@bar.com") {
		t.Error("ContainsPii: expected true for text with email")
	}
}

func TestContainsPii_False(t *testing.T) {
	if ContainsPii("ไม่มีข้อมูลส่วนตัวในประโยคนี้") {
		t.Error("ContainsPii: expected false for clean Thai text")
	}
}
