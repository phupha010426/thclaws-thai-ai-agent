package parser

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// VisionResult mirrors ImageUnderstandingResult from
// src/modules/agents/image-understanding.parser.ts so the Go core can produce
// identical structured output for slip / image understanding flows.
type VisionResult struct {
	FinalReply        string  `json:"finalReply"`
	IsSlip            bool    `json:"isSlip"`
	Amount            float64 `json:"amount,omitempty"`
	HasAmount         bool    `json:"hasAmount,omitempty"`
	Currency          string  `json:"currency,omitempty"`
	TransferAt        string  `json:"transferAt,omitempty"`
	FromAccountMasked string  `json:"fromAccountMasked,omitempty"`
	ToAccountMasked   string  `json:"toAccountMasked,omitempty"`
	FromName          string  `json:"fromName,omitempty"`
	ToName            string  `json:"toName,omitempty"`
	BankName          string  `json:"bankName,omitempty"`
	DirectionHint     string  `json:"directionHint"`
	Confidence        float64 `json:"confidence"`
}

type VisionOutcome struct {
	Result       VisionResult `json:"result"`
	UsedFallback bool         `json:"usedFallback"`
}

var (
	finalReplyField  = regexp.MustCompile(`"finalReply"\s*:\s*"((?:\\"|[^"])*)"`)
	isSlipTrueField  = regexp.MustCompile(`"isSlip"\s*:\s*true`)
	controlCharField = regexp.MustCompile(`[\x00-\x1F]+`)
)

// ParseVision converts the (often messy) vision-model reply into VisionResult.
// It accepts standard JSON, JSON-in-prose, Python repr (single quotes / True /
// False / None), and chat-parts list wrappers. When nothing parses it falls
// back to the raw text so the LINE worker can still answer the user.
func ParseVision(content string) VisionOutcome {
	unwrapped := unwrapChatParts(content)
	jsonish := extractJSONObject(unwrapped)
	if parsed, ok := parseLooseJSON(jsonish); ok {
		return VisionOutcome{Result: normalizeVision(parsed), UsedFallback: false}
	}

	fallback := strings.TrimSpace(condenseWhitespace(unwrapped))
	if fallback == "" {
		fallback = strings.TrimSpace(condenseWhitespace(content))
	}
	if fallback == "" {
		fallback = "รับรูปแล้วครับ แต่ยังอ่านรายละเอียดจากรูปไม่ได้ชัดเจน"
	}
	if unusableVisionText(fallback) {
		fallback = "รับรูปไว้แล้วครับ แต่ AI ยังอ่านรายละเอียดจากรูปนี้ไม่สำเร็จ ขอส่งรูปชัดขึ้นอีกครั้งนะครับ"
	}
	if len(fallback) > 900 {
		fallback = fallback[:900]
	}

	return VisionOutcome{
		Result: normalizeVision(map[string]any{
			"finalReply":    fallback,
			"isSlip":        false,
			"directionHint": "UNKNOWN",
			"confidence":    0.3,
		}),
		UsedFallback: true,
	}
}

func unwrapChatParts(content string) string {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "[") {
		return content
	}

	var parsed []any
	if err := json.Unmarshal([]byte(pythonReprToJSON(trimmed)), &parsed); err != nil {
		return content
	}

	parts := make([]string, 0, len(parsed))
	for _, raw := range parsed {
		switch v := raw.(type) {
		case string:
			parts = append(parts, v)
		case map[string]any:
			if t, ok := v["text"].(string); ok && t != "" {
				parts = append(parts, t)
				continue
			}
			if t, ok := v["content"].(string); ok && t != "" {
				parts = append(parts, t)
			}
		}
	}
	out := strings.TrimSpace(strings.Join(parts, "\n"))
	if out == "" {
		return content
	}
	return out
}

func extractJSONObject(content string) string {
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start < 0 || end < start {
		return content
	}
	return content[start : end+1]
}

func parseLooseJSON(content string) (map[string]any, bool) {
	if v, ok := tryJSON(content); ok {
		return v, true
	}
	stripped := controlCharField.ReplaceAllString(content, " ")
	if v, ok := tryJSON(stripped); ok {
		return v, true
	}
	if v, ok := tryJSON(pythonReprToJSON(stripped)); ok {
		return v, true
	}

	if reply := captureField(finalReplyField, content); reply != "" {
		return map[string]any{
			"finalReply":        reply,
			"isSlip":            isSlipTrueField.MatchString(content),
			"amount":            extractNumberField(content, "amount"),
			"currency":          firstNonEmpty(extractStringField(content, "currency"), "THB"),
			"transferAt":        extractStringField(content, "transferAt"),
			"fromAccountMasked": extractStringField(content, "fromAccountMasked"),
			"toAccountMasked":   extractStringField(content, "toAccountMasked"),
			"fromName":          extractStringField(content, "fromName"),
			"toName":            extractStringField(content, "toName"),
			"bankName":          extractStringField(content, "bankName"),
			"directionHint":     firstNonEmpty(extractStringField(content, "directionHint"), "UNKNOWN"),
			"confidence":        firstNonZero(extractNumberField(content, "confidence"), 0.4),
		}, true
	}
	return nil, false
}

func tryJSON(content string) (map[string]any, bool) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return nil, false
	}
	return raw, true
}

func captureField(pattern *regexp.Regexp, content string) string {
	m := pattern.FindStringSubmatch(content)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(strings.ReplaceAll(m[1], `\"`, `"`))
}

func extractStringField(content, field string) string {
	pattern := regexp.MustCompile(`"` + regexp.QuoteMeta(field) + `"\s*:\s*"((?:\\"|[^"])*)"`)
	return captureField(pattern, content)
}

func extractNumberField(content, field string) float64 {
	pattern := regexp.MustCompile(`"` + regexp.QuoteMeta(field) + `"\s*:\s*([0-9][0-9,.]*)`)
	m := pattern.FindStringSubmatch(content)
	if len(m) < 2 {
		return 0
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
	if err != nil {
		return 0
	}
	return v
}

func normalizeVision(raw map[string]any) VisionResult {
	out := VisionResult{
		FinalReply:        firstNonEmptyString(raw, "finalReply", "final_reply", "reply", "summary"),
		IsSlip:            asBool(raw["isSlip"]) || asBool(raw["is_slip"]),
		Currency:          firstNonEmptyString(raw, "currency"),
		TransferAt:        firstNonEmptyString(raw, "transferAt", "transfer_at"),
		FromAccountMasked: firstNonEmptyString(raw, "fromAccountMasked", "from_account_masked"),
		ToAccountMasked:   firstNonEmptyString(raw, "toAccountMasked", "to_account_masked"),
		FromName:          firstNonEmptyString(raw, "fromName", "from_name"),
		ToName:            firstNonEmptyString(raw, "toName", "to_name"),
		BankName:          firstNonEmptyString(raw, "bankName", "bank_name"),
	}
	if out.FinalReply == "" {
		out.FinalReply = "อ่านรูปแล้วครับ"
	}
	if out.Currency == "" {
		out.Currency = "THB"
	}
	if amt, ok := asNumber(raw["amount"]); ok {
		out.Amount = amt
		out.HasAmount = true
	}
	if conf, ok := asNumber(raw["confidence"]); ok {
		out.Confidence = conf
	} else {
		out.Confidence = 0.5
	}
	out.DirectionHint = normalizeDirection(firstNonEmptyAny(raw["directionHint"], raw["direction_hint"]))
	return out
}

func normalizeDirection(value any) string {
	s := strings.ToUpper(strings.TrimSpace(toString(value)))
	switch s {
	case "INCOME", "EXPENSE", "TRANSFER", "UNKNOWN":
		return s
	default:
		return "UNKNOWN"
	}
}

func firstNonEmptyString(raw map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := raw[k]; ok {
			if s := strings.TrimSpace(toString(v)); s != "" {
				return s
			}
		}
	}
	return ""
}

func firstNonEmptyAny(values ...any) any {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstNonZero(value, fallback float64) float64 {
	if value == 0 {
		return fallback
	}
	return value
}

func toString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func asBool(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	default:
		return false
	}
}

func asNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case string:
		f, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(v), ",", ""), 64)
		if err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

func condenseWhitespace(content string) string {
	return regexp.MustCompile(`\s+`).ReplaceAllString(content, " ")
}

func unusableVisionText(content string) bool {
	clean := strings.TrimSpace(content)
	if clean == "" {
		return true
	}
	lower := strings.ToLower(clean)
	bad := []string{"i'm sorry", "i am sorry", "cannot provide", "can't provide", "cannot comply", "sorry,"}
	for _, marker := range bad {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	hasThai := false
	for _, r := range clean {
		if r >= 0x0E00 && r <= 0x0E7F {
			hasThai = true
			break
		}
	}
	return !hasThai && len([]rune(clean)) < 200
}

// pythonReprToJSON tolerates LLM responses that use Python repr literals
// (single quotes, True/False/None) by rewriting them as valid JSON.
func pythonReprToJSON(content string) string {
	var b strings.Builder
	b.Grow(len(content))
	var quoteCh byte
	var inString bool
	var escape bool

	bytes := []byte(content)
	for i := 0; i < len(bytes); i++ {
		ch := bytes[i]
		if inString {
			if escape {
				b.WriteByte(ch)
				escape = false
				continue
			}
			switch {
			case ch == '\\':
				b.WriteByte(ch)
				escape = true
			case ch == quoteCh:
				if quoteCh == '\'' {
					b.WriteByte('"')
				} else {
					b.WriteByte(ch)
				}
				inString = false
			case ch == '"' && quoteCh == '\'':
				b.WriteString(`\"`)
			default:
				b.WriteByte(ch)
			}
			continue
		}

		if ch == '"' || ch == '\'' {
			inString = true
			quoteCh = ch
			b.WriteByte('"')
			continue
		}

		if matchesWord(bytes, i, "True") {
			b.WriteString("true")
			i += 3
			continue
		}
		if matchesWord(bytes, i, "False") {
			b.WriteString("false")
			i += 4
			continue
		}
		if matchesWord(bytes, i, "None") {
			b.WriteString("null")
			i += 3
			continue
		}
		b.WriteByte(ch)
	}
	return b.String()
}

func matchesWord(content []byte, index int, word string) bool {
	if index+len(word) > len(content) {
		return false
	}
	if string(content[index:index+len(word)]) != word {
		return false
	}
	if index > 0 && isWordChar(content[index-1]) {
		return false
	}
	if index+len(word) < len(content) && isWordChar(content[index+len(word)]) {
		return false
	}
	return true
}

func isWordChar(ch byte) bool {
	return (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_'
}
