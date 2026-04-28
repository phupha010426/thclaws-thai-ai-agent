package ledger

import "testing"

func TestExtractExplicitProjectName(t *testing.T) {
	cases := map[string]string{
		"ซื้อปูน 1000 โครงการสร้างบ้าน":       "สร้างบ้าน",
		"ค่าไฟ 980 โปรเจกต์ร้านใหม่":          "ร้านใหม่",
		"ซื้อของ #สร้างร้าน 1200":             "สร้างร้าน",
		"โครงการสร้างบ้าน ค่าเหล็ก 5000":      "สร้างบ้าน",
		"ค่าแรง 3000 project home-renovation": "home-renovation",
	}
	for text, want := range cases {
		if got := extractExplicitProjectName(text); got != want {
			t.Fatalf("%q = %q, want %q", text, got, want)
		}
	}
}

func TestSplitProjectAllocations(t *testing.T) {
	projects := []ProjectSummary{{ID: "p1", Name: "บ้าน"}, {ID: "p2", Name: "ร้าน"}}
	got := splitProjectAllocations(projects, 100)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Amount != 50 || got[1].Amount != 50 {
		t.Fatalf("amounts = %.2f/%.2f, want 50/50", got[0].Amount, got[1].Amount)
	}
}
