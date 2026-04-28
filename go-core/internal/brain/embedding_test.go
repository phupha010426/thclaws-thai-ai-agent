package brain

import (
	"math"
	"strings"
	"testing"
)

func TestToVectorLiteral_Format(t *testing.T) {
	vec := []float64{1.0, 2.5, -0.333}
	lit := toVectorLiteral(vec)
	if !strings.HasPrefix(lit, "[") || !strings.HasSuffix(lit, "]") {
		t.Errorf("toVectorLiteral: expected brackets, got %q", lit)
	}
	if strings.Count(lit, ",") != 2 {
		t.Errorf("toVectorLiteral: expected 2 commas for 3-element vector, got %q", lit)
	}
}

func TestToVectorLiteral_NonFinite(t *testing.T) {
	vec := []float64{math.NaN(), math.Inf(1), math.Inf(-1)}
	lit := toVectorLiteral(vec)
	// Non-finite values must be replaced with 0 to keep the index clean.
	if strings.Contains(lit, "NaN") || strings.Contains(lit, "Inf") {
		t.Errorf("toVectorLiteral: non-finite values leaked into literal: %q", lit)
	}
	want := "[0,0,0]"
	if lit != want {
		t.Errorf("toVectorLiteral: got %q; want %q", lit, want)
	}
}

func TestToVectorLiteral_Empty(t *testing.T) {
	lit := toVectorLiteral([]float64{})
	if lit != "[]" {
		t.Errorf("toVectorLiteral: empty vector should produce '[]', got %q", lit)
	}
}

func TestNullableJSON_Nil(t *testing.T) {
	if nullableJSON(nil) != nil {
		t.Error("nullableJSON(nil) should return nil")
	}
	if nullableJSON([]byte{}) != nil {
		t.Error("nullableJSON(empty) should return nil")
	}
}

func TestNullableJSON_NonNil(t *testing.T) {
	b := []byte(`{"k":"v"}`)
	if nullableJSON(b) == nil {
		t.Error("nullableJSON with content should return non-nil")
	}
}
