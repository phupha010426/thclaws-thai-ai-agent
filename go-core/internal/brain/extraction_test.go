package brain

import (
	"strings"
	"testing"
)

func TestParseJSONLoosely_Clean(t *testing.T) {
	input := `{"pages":[],"edges":[]}`
	result, ok := parseJSONLoosely(input)
	if !ok {
		t.Fatal("parseJSONLoosely: expected ok=true for clean JSON")
	}
	if _, has := result["pages"]; !has {
		t.Error("parseJSONLoosely: missing 'pages' key")
	}
}

func TestParseJSONLoosely_WithFences(t *testing.T) {
	input := "```json\n{\"pages\":[],\"edges\":[]}\n```"
	_, ok := parseJSONLoosely(input)
	if !ok {
		t.Error("parseJSONLoosely: should handle ```json fences")
	}
}

func TestParseJSONLoosely_EmbeddedBlock(t *testing.T) {
	input := `Here is the result: {"pages":[],"edges":[]} Hope that helps.`
	_, ok := parseJSONLoosely(input)
	if !ok {
		t.Error("parseJSONLoosely: should extract first {...} block from prose")
	}
}

func TestParseJSONLoosely_Invalid(t *testing.T) {
	_, ok := parseJSONLoosely("this is just prose without any JSON")
	if ok {
		t.Error("parseJSONLoosely: should return false for non-JSON")
	}
}

func TestBuildDedupKey_Deterministic(t *testing.T) {
	k1 := buildDedupKey("ns1", "some text")
	k2 := buildDedupKey("ns1", "some text")
	if k1 != k2 {
		t.Errorf("buildDedupKey: same input produced different keys: %q vs %q", k1, k2)
	}
}

func TestBuildDedupKey_DifferentNamespace(t *testing.T) {
	k1 := buildDedupKey("ns1", "same text")
	k2 := buildDedupKey("ns2", "same text")
	if k1 == k2 {
		t.Error("buildDedupKey: different namespaces should produce different keys")
	}
}

func TestBuildDedupKey_Format(t *testing.T) {
	k := buildDedupKey("ns", "text")
	if !strings.HasPrefix(k, "brain:extract:") {
		t.Errorf("buildDedupKey: expected prefix 'brain:extract:', got %q", k)
	}
}

func TestBuildEmbedContent_Basic(t *testing.T) {
	content := buildEmbedContent("Title", "Summary", []string{"alias1", "alias2"})
	if !strings.Contains(content, "Title") {
		t.Error("buildEmbedContent: missing title")
	}
	if !strings.Contains(content, "Summary") {
		t.Error("buildEmbedContent: missing summary")
	}
	if !strings.Contains(content, "alias1") {
		t.Error("buildEmbedContent: missing aliases")
	}
}

func TestBuildEmbedContent_EmptySummary(t *testing.T) {
	content := buildEmbedContent("Title", "", nil)
	if content != "Title" {
		t.Errorf("buildEmbedContent: want 'Title', got %q", content)
	}
}

func TestValidateExtractionResult_ValidKinds(t *testing.T) {
	raw, ok := parseJSONLoosely(`{
		"pages":[{"slug":"foo","title":"Foo","kind":"person"}],
		"edges":[]
	}`)
	if !ok {
		t.Fatal("parse failed")
	}
	result, err := validateExtractionResult(raw)
	if err != nil {
		t.Fatalf("validateExtractionResult: unexpected error: %v", err)
	}
	if len(result.Pages) != 1 || result.Pages[0].Kind != "person" {
		t.Errorf("validateExtractionResult: unexpected result: %+v", result)
	}
}

func TestValidateExtractionResult_UnknownKindFallsBackToConcept(t *testing.T) {
	raw, ok := parseJSONLoosely(`{
		"pages":[{"slug":"foo","title":"Foo","kind":"alien"}],
		"edges":[]
	}`)
	if !ok {
		t.Fatal("parse failed")
	}
	result, err := validateExtractionResult(raw)
	if err != nil {
		t.Fatalf("validateExtractionResult: unexpected error: %v", err)
	}
	if result.Pages[0].Kind != "concept" {
		t.Errorf("unknown kind should fall back to 'concept', got %q", result.Pages[0].Kind)
	}
}

func TestValidateExtractionResult_MissingSlug(t *testing.T) {
	raw, ok := parseJSONLoosely(`{"pages":[{"title":"Foo"}],"edges":[]}`)
	if !ok {
		t.Fatal("parse failed")
	}
	_, err := validateExtractionResult(raw)
	if err == nil {
		t.Error("validateExtractionResult: expected error for missing slug")
	}
}

func TestTruncate(t *testing.T) {
	if truncate("hello", 10) != "hello" {
		t.Error("truncate: should not cut short strings")
	}
	if truncate("hello world", 5) != "hello" {
		t.Error("truncate: should cut at n")
	}
}
