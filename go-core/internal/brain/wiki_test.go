package brain

import (
	"strings"
	"testing"
)

// Tests in this file focus on the pure-logic helpers inside wiki.go.
// Integration tests (pgx pool + real DB) live in a separate _integration_test
// file that requires a live Postgres instance. The helpers below are
// deterministic and require no external dependencies.

func TestNormalizeSlug(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"Hello World", "hello-world"},
		{"  spaces  ", "spaces"},
		{"already-kebab", "already-kebab"},
		{"UPPER CASE SLUG", "upper-case-slug"},
		{"multiple   spaces", "multiple-spaces"},
	}
	for _, c := range cases {
		got := normalizeSlug(c.input)
		if got != c.want {
			t.Errorf("normalizeSlug(%q) = %q; want %q", c.input, got, c.want)
		}
	}
}

func TestTokenize_Basic(t *testing.T) {
	tokens := tokenize("กาแฟ 60 บาท")
	if len(tokens) == 0 {
		t.Fatal("tokenize: expected non-empty result for Thai text")
	}
	for _, tok := range tokens {
		if len([]rune(tok)) < 2 {
			t.Errorf("tokenize: token %q shorter than min length 2", tok)
		}
	}
}

func TestTokenize_Deduplication(t *testing.T) {
	tokens := tokenize("foo foo bar bar")
	seen := make(map[string]int)
	for _, tok := range tokens {
		seen[tok]++
	}
	for tok, count := range seen {
		if count > 1 {
			t.Errorf("tokenize: token %q appeared %d times; want 1", tok, count)
		}
	}
}

func TestTokenize_MinLength(t *testing.T) {
	// Single-character tokens should be dropped.
	tokens := tokenize("a b c ก ข ค hello")
	for _, tok := range tokens {
		if len([]rune(tok)) < 2 {
			t.Errorf("tokenize: token %q shorter than 2 runes passed through", tok)
		}
	}
}

func TestTokenize_Empty(t *testing.T) {
	if len(tokenize("")) != 0 {
		t.Error("tokenize: empty string should return empty slice")
	}
	if len(tokenize("   ")) != 0 {
		t.Error("tokenize: whitespace-only string should return empty slice")
	}
}

func TestUniqueLower(t *testing.T) {
	out := uniqueLower([]string{"Foo", "foo", "BAR", "bar", "Baz"})
	if len(out) != 3 {
		t.Errorf("uniqueLower: got %v; want 3 elements", out)
	}
	for _, s := range out {
		if s != strings.ToLower(s) {
			t.Errorf("uniqueLower: %q is not lowercase", s)
		}
	}
}

func TestPageScore_AliasMatchBeatsEdgeNeighbor(t *testing.T) {
	alias := RetrievedPage{Importance: 0.5, Reason: "alias_match", Hops: 0}
	neighbor := RetrievedPage{Importance: 0.5, Reason: "edge_neighbor", Hops: 1}
	if pageScore(alias) <= pageScore(neighbor) {
		t.Errorf("alias_match score (%v) should be higher than edge_neighbor (%v)",
			pageScore(alias), pageScore(neighbor))
	}
}

func TestPageScore_HopPenalty(t *testing.T) {
	hop0 := RetrievedPage{Importance: 1.0, Reason: "alias_match", Hops: 0}
	hop1 := RetrievedPage{Importance: 1.0, Reason: "alias_match", Hops: 1}
	if pageScore(hop0) <= pageScore(hop1) {
		t.Error("0-hop page should score higher than 1-hop page")
	}
}

func TestRankPages_SlicesLimit(t *testing.T) {
	pages := map[string]RetrievedPage{
		"a": {ID: "a", Importance: 0.9, Reason: "alias_match", Hops: 0},
		"b": {ID: "b", Importance: 0.8, Reason: "alias_match", Hops: 0},
		"c": {ID: "c", Importance: 0.7, Reason: "alias_match", Hops: 0},
		"d": {ID: "d", Importance: 0.6, Reason: "alias_match", Hops: 0},
	}
	ranked := rankPages(pages, 2)
	if len(ranked) != 2 {
		t.Fatalf("rankPages: got %d results; want 2", len(ranked))
	}
	if ranked[0].ID != "a" {
		t.Errorf("rankPages: first result should be 'a' (highest importance), got %q", ranked[0].ID)
	}
}

func TestClamp01(t *testing.T) {
	cases := []struct{ in, want float64 }{
		{-1, 0},
		{0, 0},
		{0.5, 0.5},
		{1, 1},
		{2, 1},
	}
	for _, c := range cases {
		if got := clamp01(c.in); got != c.want {
			t.Errorf("clamp01(%v) = %v; want %v", c.in, got, c.want)
		}
	}
}
