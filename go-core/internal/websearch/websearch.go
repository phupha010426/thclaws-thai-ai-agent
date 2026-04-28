// Package websearch provides a small allowlisted internet lookup tool for the
// thClaws broker. It fetches public web pages only when the user asks for
// fresh/current information or provides a URL, then passes short snippets to
// the model so answers can be grounded instead of guessed.
package websearch

import (
	"context"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultSearchURL = "https://html.duckduckgo.com/html/"
	maxBodyBytes     = 256 * 1024
)

var (
	urlPattern       = regexp.MustCompile(`https?://[^\s<>"']+`)
	tagPattern       = regexp.MustCompile(`<[^>]+>`)
	spacePattern     = regexp.MustCompile(`\s+`)
	resultBlockRe    = regexp.MustCompile(`(?is)<div[^>]+class="[^"]*result[^"]*"[^>]*>(.*?)</div>\s*</div>`)
	resultLinkRe     = regexp.MustCompile(`(?is)<a[^>]+class="[^"]*result__a[^"]*"[^>]+href="([^"]+)"[^>]*>(.*?)</a>`)
	resultSnippetRe  = regexp.MustCompile(`(?is)<a[^>]+class="[^"]*result__snippet[^"]*"[^>]*>(.*?)</a>|<div[^>]+class="[^"]*result__snippet[^"]*"[^>]*>(.*?)</div>`)
	pageTitleRe      = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	searchKeywordRe  = regexp.MustCompile(`(?i)(ค้นหา|หาให้|ดูเว็บ|จากเว็บ|internet|อินเทอร์เน็ต|google|กูเกิล|ข่าว|ล่าสุด|ปัจจุบัน|ตอนนี้|วันนี้|ราคา|หุ้น|คริปโต|อากาศ|ผลบอล|ตาราง|ใครเป็น|ใครคือ|เว็บ|url)`)
	accountingHintRe = regexp.MustCompile(`(?i)(กาแฟ|ข้าว|ค่าไฟ|ค่าน้ำ|เงินเดือน|ขายของ|รายรับ|รายจ่าย|สรุปวันนี้|สรุปเดือนนี้|เงินเหลือ|วันนี้วันอะไร)`)
)

type Result struct {
	Title   string
	URL     string
	Snippet string
}

type Service struct {
	http      *http.Client
	searchURL string
	logger    *slog.Logger
}

func New(httpClient *http.Client, logger *slog.Logger) *Service {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 8 * time.Second}
	}
	return &Service{http: httpClient, searchURL: defaultSearchURL, logger: logger}
}

func ShouldSearch(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || accountingHintRe.MatchString(text) {
		return false
	}
	return urlPattern.MatchString(text) || searchKeywordRe.MatchString(text)
}

func (s *Service) Search(ctx context.Context, text string, limit int) ([]Result, error) {
	if limit <= 0 || limit > 5 {
		limit = 3
	}
	if rawURL := firstURL(text); rawURL != "" {
		result, err := s.fetchURL(ctx, rawURL)
		if err != nil {
			return nil, err
		}
		return []Result{result}, nil
	}
	return s.search(ctx, text, limit)
}

func (s *Service) search(ctx context.Context, query string, limit int) ([]Result, error) {
	u, err := url.Parse(s.searchURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("q", query)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ThaiAiAgent/1.0 (+https://thaiaiagent.smlsoft.app)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("websearch: search http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("websearch: search status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("websearch: search read: %w", err)
	}
	results := ParseDuckDuckGoHTML(string(body), limit)
	if len(results) == 0 {
		return nil, fmt.Errorf("websearch: no results")
	}
	return results, nil
}

func (s *Service) fetchURL(ctx context.Context, rawURL string) (Result, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return Result{}, fmt.Errorf("websearch: invalid url")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("User-Agent", "ThaiAiAgent/1.0 (+https://thaiaiagent.smlsoft.app)")
	resp, err := s.http.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("websearch: fetch http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("websearch: fetch status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return Result{}, fmt.Errorf("websearch: fetch read: %w", err)
	}
	htmlText := string(body)
	title := cleanHTML(firstSubmatch(pageTitleRe, htmlText))
	if title == "" {
		title = parsed.Host
	}
	snippet := truncateRunes(cleanHTML(htmlText), 600)
	return Result{Title: title, URL: parsed.String(), Snippet: snippet}, nil
}

func ParseDuckDuckGoHTML(raw string, limit int) []Result {
	if limit <= 0 {
		limit = 3
	}
	blocks := resultBlockRe.FindAllStringSubmatch(raw, -1)
	results := make([]Result, 0, limit)
	seen := map[string]struct{}{}
	for _, block := range blocks {
		if len(block) < 2 {
			continue
		}
		link := resultLinkRe.FindStringSubmatch(block[1])
		if len(link) < 3 {
			continue
		}
		href := decodeDuckDuckGoURL(html.UnescapeString(link[1]))
		if href == "" {
			continue
		}
		if _, ok := seen[href]; ok {
			continue
		}
		title := cleanHTML(link[2])
		snippetMatch := resultSnippetRe.FindStringSubmatch(block[1])
		snippet := ""
		if len(snippetMatch) > 1 {
			for _, part := range snippetMatch[1:] {
				if strings.TrimSpace(part) != "" {
					snippet = cleanHTML(part)
					break
				}
			}
		}
		if title == "" {
			continue
		}
		seen[href] = struct{}{}
		results = append(results, Result{Title: title, URL: href, Snippet: truncateRunes(snippet, 500)})
		if len(results) >= limit {
			break
		}
	}
	return results
}

func firstURL(text string) string {
	raw := urlPattern.FindString(text)
	return strings.TrimRight(raw, ".,)>]}")
}

func decodeDuckDuckGoURL(raw string) string {
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	parsed, err := url.Parse(raw)
	if err == nil {
		if uddg := parsed.Query().Get("uddg"); uddg != "" {
			return uddg
		}
		if parsed.Scheme == "http" || parsed.Scheme == "https" {
			return parsed.String()
		}
	}
	return ""
}

func cleanHTML(raw string) string {
	raw = tagPattern.ReplaceAllString(raw, " ")
	raw = html.UnescapeString(raw)
	raw = spacePattern.ReplaceAllString(raw, " ")
	return strings.TrimSpace(raw)
}

func firstSubmatch(re *regexp.Regexp, text string) string {
	m := re.FindStringSubmatch(text)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func truncateRunes(text string, max int) string {
	if max <= 0 || utf8.RuneCountInString(text) <= max {
		return text
	}
	runes := []rune(text)
	return string(runes[:max])
}
