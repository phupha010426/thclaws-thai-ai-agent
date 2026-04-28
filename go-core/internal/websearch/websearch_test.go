package websearch

import "testing"

func TestShouldSearch(t *testing.T) {
	if !ShouldSearch("ค้นหาข่าวล่าสุดให้หน่อย") {
		t.Fatal("expected explicit search request to search")
	}
	if !ShouldSearch("https://example.com/a") {
		t.Fatal("expected URL to search/fetch")
	}
	if ShouldSearch("กาแฟ 60") {
		t.Fatal("accounting messages should not trigger web search")
	}
	if ShouldSearch("เงินเหลือเท่าไหร่") {
		t.Fatal("ledger balance should not trigger web search")
	}
}

func TestParseDuckDuckGoHTML(t *testing.T) {
	raw := `
		<div class="result results_links results_links_deep web-result">
			<div class="links_main links_deep result__body">
				<h2 class="result__title">
					<a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fnews&amp;rut=abc">Example &amp; News</a>
				</h2>
				<a class="result__snippet" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fnews">Latest <b>update</b> today.</a>
			</div>
		</div>`
	got := ParseDuckDuckGoHTML(raw, 3)
	if len(got) != 1 {
		t.Fatalf("results = %d, want 1: %+v", len(got), got)
	}
	if got[0].URL != "https://example.com/news" {
		t.Fatalf("url = %q", got[0].URL)
	}
	if got[0].Title != "Example & News" {
		t.Fatalf("title = %q", got[0].Title)
	}
	if got[0].Snippet != "Latest update today." {
		t.Fatalf("snippet = %q", got[0].Snippet)
	}
}
