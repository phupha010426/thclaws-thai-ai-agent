package ledger

import "testing"

func TestFormatMoneyUsesComma(t *testing.T) {
	cases := map[float64]string{
		0:        "0",
		60:       "60",
		1200:     "1,200",
		25000:    "25,000",
		1234.5:   "1,234.5",
		1234.56:  "1,234.56",
		-9876.5:  "-9,876.5",
		10000000: "10,000,000",
	}
	for input, want := range cases {
		if got := formatMoney(input); got != want {
			t.Fatalf("formatMoney(%v) = %q, want %q", input, got, want)
		}
	}
}
