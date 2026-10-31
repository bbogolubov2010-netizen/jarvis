package commands

import "testing"

func TestNormalizeResultURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{
			name: "duckduckgo scheme-relative redirect",
			in:   "//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fpage%3Fx%3D1",
			want: "https://example.com/page?x=1",
			ok:   true,
		},
		{
			name: "duckduckgo absolute redirect",
			in:   "https://duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.org%2F",
			want: "https://example.org/",
			ok:   true,
		},
		{
			name: "relative redirect",
			in:   "/l/?uddg=https%3A%2F%2Fexample.net%2F",
			want: "https://example.net/",
			ok:   true,
		},
		{
			name: "ordinary absolute result",
			in:   "https://example.com/article",
			want: "https://example.com/article",
			ok:   true,
		},
		{
			name: "unsafe scheme",
			in:   "javascript:alert(1)",
			ok:   false,
		},
		{
			name: "duckduckgo internal link",
			in:   "https://duckduckgo.com/y.js?ad_domain=example.com",
			ok:   false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := normalizeResultURL(test.in)
			if ok != test.ok {
				t.Fatalf("normalizeResultURL(%q) ok = %v, want %v", test.in, ok, test.ok)
			}
			if ok && got != test.want {
				t.Fatalf("normalizeResultURL(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}

func TestParseHTMLResultsResolvesDuckDuckGoRedirects(t *testing.T) {
	const page = `<div class="result">
		<a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fpage%3Fx%3D1">Example &amp; Test</a>
		<a class="result__snippet" href="#">A useful snippet.</a>
	</div>`

	results := parseHTMLResults(page)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1: %+v", len(results), results)
	}
	if results[0].URL != "https://example.com/page?x=1" {
		t.Fatalf("URL = %q", results[0].URL)
	}
	if results[0].Title != "Example & Test" {
		t.Fatalf("Title = %q", results[0].Title)
	}
}
