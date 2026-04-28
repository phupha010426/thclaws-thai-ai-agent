package parser

import (
	"strings"
	"testing"
)

func TestParseVisionStandardJSON(t *testing.T) {
	out := ParseVision(`{"finalReply":"hi","isSlip":false,"confidence":0.9}`)
	if out.UsedFallback {
		t.Fatal("did not expect fallback for valid JSON")
	}
	if out.Result.FinalReply != "hi" || out.Result.IsSlip || out.Result.Confidence != 0.9 {
		t.Fatalf("unexpected result: %+v", out.Result)
	}
}

func TestParseVisionPythonRepr(t *testing.T) {
	raw := `{'finalReply': 'อ่านสลิปแล้ว', 'isSlip': True, 'amount': 1234.5, 'currency': 'THB', 'directionHint': 'EXPENSE', 'confidence': 0.85}`
	out := ParseVision(raw)
	if out.UsedFallback {
		t.Fatal("Python repr should parse without fallback")
	}
	r := out.Result
	if r.FinalReply != "อ่านสลิปแล้ว" || !r.IsSlip || r.Amount != 1234.5 || r.DirectionHint != "EXPENSE" || r.Confidence != 0.85 {
		t.Fatalf("unexpected: %+v", r)
	}
}

func TestParseVisionChatPartsWithInnerJSON(t *testing.T) {
	raw := `[{'type': 'text', 'text': '{"finalReply":"hi","isSlip":false,"confidence":0.5}'}]`
	out := ParseVision(raw)
	if out.UsedFallback {
		t.Fatal("chat-parts with inner JSON should parse")
	}
	if out.Result.FinalReply != "hi" {
		t.Fatalf("finalReply = %q", out.Result.FinalReply)
	}
}

func TestParseVisionChatPartsPlainText(t *testing.T) {
	raw := `[{'type': 'text', 'text': 'อาหารไทย'}]`
	out := ParseVision(raw)
	if !out.UsedFallback {
		t.Fatal("plain Thai text inside chat-parts should fall back")
	}
	if !strings.Contains(out.Result.FinalReply, "อาหารไทย") {
		t.Fatalf("finalReply = %q, want contains อาหารไทย", out.Result.FinalReply)
	}
}

func TestParseVisionPythonNoneAndFalse(t *testing.T) {
	raw := `{'finalReply': 'ok', 'isSlip': False, 'amount': None, 'confidence': 0.4}`
	out := ParseVision(raw)
	if out.Result.IsSlip {
		t.Fatal("isSlip should be false")
	}
	if out.Result.HasAmount {
		t.Fatalf("expected no amount, got %v", out.Result.Amount)
	}
}

func TestParseVisionEmbeddedInProse(t *testing.T) {
	raw := `Here is the result: {"finalReply":"พบสลิป","isSlip":true,"amount":500,"confidence":0.7}. Done.`
	out := ParseVision(raw)
	if !out.Result.IsSlip || out.Result.Amount != 500 {
		t.Fatalf("unexpected: %+v", out.Result)
	}
}

func TestParseVisionFallbackOnGarbage(t *testing.T) {
	out := ParseVision("completely broken response")
	if !out.UsedFallback {
		t.Fatal("expected fallback for garbage input")
	}
	if out.Result.IsSlip || out.Result.Confidence != 0.3 {
		t.Fatalf("unexpected: %+v", out.Result)
	}
}
