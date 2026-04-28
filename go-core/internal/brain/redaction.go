// Package brain implements the per-namespace knowledge graph, embedding, and
// LLM extraction services for the Thai LINE-OA agent.
package brain

import "regexp"

// piiPattern couples a compiled regex with its replacement placeholder.
// Order matters: the Thai national ID and credit-card patterns overlap if
// applied in the wrong sequence (the CC pattern is greedier), so Thai ID is
// checked first.
type piiPattern struct {
	re          *regexp.Regexp
	placeholder string
}

var piiPatterns = []piiPattern{
	// Thai national ID — 13 digits, optional hyphens/spaces between groups.
	{regexp.MustCompile(`\b\d[-\s]?\d{4}[-\s]?\d{5}[-\s]?\d{2}[-\s]?\d\b`), "<id>"},
	// Thai bank account — NNN-N-NNNNNN-NNN style (10-13 digits with hyphens).
	{regexp.MustCompile(`\b\d{3}[-\s]\d[-\s]\d{4,6}[-\s]\d{1,3}\b`), "<acc>"},
	// Thai mobile/landline — starts with 0, 9-10 digits total.
	{regexp.MustCompile(`\b0\d{1,2}[-\s]?\d{3}[-\s]?\d{4}\b`), "<phone>"},
	// Email addresses.
	{regexp.MustCompile(`[\w.+\-]+@[\w\-]+\.[\w.\-]+`), "<email>"},
	// Credit-card-shaped numbers — 13 to 19 digits with optional separators.
	// Applied last so it cannot swallow an already-redacted <id> or <acc>.
	{regexp.MustCompile(`\b(?:\d[ \-]?){13,19}\b`), "<cc>"},
}

// RedactPii replaces every PII token found in text with a typed placeholder.
// Lossy by design — the semantic content is preserved while identifiers are
// stripped before embeddings or LLM calls.
func RedactPii(text string) string {
	out := text
	for _, p := range piiPatterns {
		out = p.re.ReplaceAllString(out, p.placeholder)
	}
	return out
}

// ContainsPii returns true when text differs after redaction, i.e. at least
// one PII pattern matched.
func ContainsPii(text string) bool {
	return RedactPii(text) != text
}
