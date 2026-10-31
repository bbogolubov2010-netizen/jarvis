package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"

	"Jarvis/internal/netguard"
)

type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
	Time    string `json:"time"`
}

type ddgAnswer struct {
	Abstract      string `json:"Abstract"`
	AbstractText  string `json:"AbstractText"`
	AbstractURL   string `json:"AbstractURL"`
	Answer        string `json:"Answer"`
	AnswerType    string `json:"AnswerType"`
	Definition    string `json:"Definition"`
	DefinitionURL string `json:"DefinitionURL"`
	Image         string `json:"Image"`
	Heading       string `json:"Heading"`
	Results       []struct {
		Text     string `json:"Text"`
		FirstURL string `json:"FirstURL"`
	} `json:"Results"`
	RelatedTopics []struct {
		Text     string `json:"Text"`
		FirstURL string `json:"FirstURL"`
		Topics   []struct {
			Text     string `json:"Text"`
			FirstURL string `json:"FirstURL"`
		} `json:"Topics"`
	} `json:"RelatedTopics"`
	Infobox json.RawMessage `json:"Infobox"`
}

func RegisterWebCommands(m *Manager) {
	m.Register(Command{
		Name:    "search",
		Aliases: []string{"google", "web", "find", "lookup", "research"},
		Handler: SearchWeb,
	})
}

func SearchWeb(query string) string {
	return SearchWebContext(context.Background(), query)
}

// SearchWebContext is the cancellable form used by voice commands. Stopping
// voice input cancels in-flight HTTP requests instead of waiting for timeouts.
func SearchWebContext(ctx context.Context, query string) string {
	if ctx == nil {
		ctx = context.Background()
	}
	if query == "" {
		return "Usage: /search <query>\nSearches the web for any information using DuckDuckGo.\nExample: /search latest news about AI"
	}
	if err := ctx.Err(); err != nil {
		return "Search cancelled, Sir."
	}

	// 1. Try instant answer API (for definitions, facts, summaries)
	answer := getInstantAnswerContext(ctx, query)
	if answer != "" {
		return answer
	}
	if err := ctx.Err(); err != nil {
		return "Search cancelled, Sir."
	}

	// 2. Try HTML search for full results
	results := searchHTMLContext(ctx, query)
	if len(results) > 0 {
		var b strings.Builder
		b.WriteString(fmt.Sprintf("Search results for \"%s\":\n\n", query))
		for i, r := range results {
			if i >= 8 {
				break
			}
			b.WriteString(fmt.Sprintf("%d. %s\n", i+1, r.Title))
			b.WriteString(fmt.Sprintf("   %s\n", r.Content))
			if r.URL != "" {
				b.WriteString(fmt.Sprintf("   %s\n", r.URL))
			}
			b.WriteString("\n")
		}
		return b.String()
	}

	return fmt.Sprintf("I couldn't find results for \"%s\" via DuckDuckGo. Try a different query or I can open a browser search for you.", query)
}

func getInstantAnswer(query string) string {
	return getInstantAnswerContext(context.Background(), query)
}

func getInstantAnswerContext(ctx context.Context, query string) string {
	apiURL := fmt.Sprintf("https://api.duckduckgo.com/?q=%s&format=json&no_html=1&skip_disambig=1", url.QueryEscape(query))

	client := netguard.NewClient(8 * time.Second)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return ""
	}
	resp, err := client.Do(request)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ""
	}

	var answer ddgAnswer
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&answer); err != nil {
		return ""
	}

	// Direct answer (weather, calculations, etc.)
	if answer.Answer != "" && answer.Answer != "//" {
		if answer.Heading != "" {
			return fmt.Sprintf("%s:\n%s", answer.Heading, answer.Answer)
		}
		return answer.Answer
	}

	// Abstract (encyclopedic info)
	if answer.Abstract != "" {
		text := truncateRunes(answer.Abstract, 600)
		result := text
		if answer.AbstractURL != "" {
			result += "\n\nSource: " + answer.AbstractURL
		}
		return result
	}

	// Definition
	if answer.Definition != "" {
		result := answer.Definition
		if answer.DefinitionURL != "" {
			result += "\n\nSource: " + answer.DefinitionURL
		}
		return result
	}

	return ""
}

func searchHTML(query string) []SearchResult {
	return searchHTMLContext(context.Background(), query)
}

func searchHTMLContext(ctx context.Context, query string) []SearchResult {
	client := netguard.NewClient(15 * time.Second)
	form := url.Values{"q": {query}}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://html.duckduckgo.com/html/", strings.NewReader(form.Encode()))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Referer", "https://duckduckgo.com/")

	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return nil
	}

	return parseHTMLResults(string(body))
}

func parseHTMLResults(htmlContent string) []SearchResult {
	var results []SearchResult
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return results
	}

	seen := make(map[string]bool)

	var extract func(*html.Node)
	extract = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			var href, title string
			for _, attr := range n.Attr {
				if attr.Key == "href" {
					href = attr.Val
				}
				if attr.Key == "class" && strings.Contains(attr.Val, "result__a") {
					title = extractText(n)
				}
			}
			if normalized, ok := normalizeResultURL(href); title != "" && ok && !seen[normalized] {
				content := findSnippet(n)
				seen[normalized] = true
				results = append(results, SearchResult{
					Title:   cleanTitle(title),
					URL:     normalized,
					Content: content,
					Time:    time.Now().Format("15:04"),
				})
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extract(c)
		}
	}
	extract(doc)

	// Fallback: line-based parsing if the DOM walker found nothing.
	if len(results) == 0 {
		return fallbackParse(htmlContent)
	}

	return results
}

// normalizeResultURL accepts absolute result links and unwraps DuckDuckGo's
// scheme-relative /l/?uddg= redirect. DuckDuckGo currently emits links such as
// "//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com", which are not usable
// as-is and would otherwise be discarded by a simple http-prefix check.
func normalizeResultURL(raw string) (string, bool) {
	raw = strings.TrimSpace(html.UnescapeString(raw))
	if raw == "" {
		return "", false
	}
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	} else if strings.HasPrefix(raw, "/l/") || strings.HasPrefix(raw, "/l?") {
		raw = "https://duckduckgo.com" + raw
	}

	for depth := 0; depth < 3; depth++ {
		parsed, err := url.Parse(raw)
		if err != nil {
			return "", false
		}
		scheme := strings.ToLower(parsed.Scheme)
		if scheme != "http" && scheme != "https" {
			return "", false
		}
		host := strings.ToLower(parsed.Hostname())
		if host == "" {
			return "", false
		}

		if host == "duckduckgo.com" || strings.HasSuffix(host, ".duckduckgo.com") {
			target := parsed.Query().Get("uddg")
			if target == "" {
				return "", false
			}
			raw = strings.TrimSpace(target)
			if strings.HasPrefix(raw, "//") {
				raw = "https:" + raw
			}
			continue
		}
		return parsed.String(), true
	}
	return "", false
}

func extractText(n *html.Node) string {
	var text string
	var extract func(*html.Node)
	extract = func(n *html.Node) {
		if n.Type == html.TextNode {
			text += n.Data
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extract(c)
		}
	}
	extract(n)
	return strings.TrimSpace(text)
}

func findSnippet(n *html.Node) string {
	// Look for sibling with class result__snippet
	for n = n.Parent; n != nil; n = n.Parent {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Data == "a" {
				for _, attr := range c.Attr {
					if attr.Key == "class" && strings.Contains(attr.Val, "result__snippet") {
						return extractText(c)
					}
				}
			}
			// Try deeper
			if c.Type == html.ElementNode {
				for d := c.FirstChild; d != nil; d = d.NextSibling {
					if d.Type == html.ElementNode && d.Data == "a" {
						for _, attr := range d.Attr {
							if attr.Key == "class" && strings.Contains(attr.Val, "result__snippet") {
								return extractText(d)
							}
						}
					}
				}
			}
		}
	}
	return ""
}

func cleanTitle(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return s
}

func fallbackParse(content string) []SearchResult {
	var results []SearchResult
	seen := make(map[string]bool)
	lines := strings.Split(content, "\n")
	inResult := false
	var currentTitle, currentURL string

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Match result links: <a rel="nofollow" href="URL" class="result__a">TITLE</a>
		if strings.Contains(line, "result__a") && strings.Contains(line, "href=\"") {
			hrefStart := strings.Index(line, "href=\"")
			if hrefStart != -1 {
				hrefStart += 6
				hrefEnd := strings.Index(line[hrefStart:], "\"")
				if hrefEnd != -1 {
					currentURL = line[hrefStart : hrefStart+hrefEnd]
					currentURL = strings.TrimSpace(currentURL)
				}
			}
			// Extract title - find text between > and </a>
			titleStart := strings.LastIndex(line, ">")
			titleEnd := strings.LastIndex(line, "</a>")
			if titleStart != -1 && titleEnd != -1 && titleStart < titleEnd {
				currentTitle = line[titleStart+1 : titleEnd]
				currentTitle = stripTags(currentTitle)
				currentTitle = cleanTitle(currentTitle)
			}

			normalized, ok := normalizeResultURL(currentURL)
			inResult = false
			if currentTitle != "" && ok && !seen[normalized] {
				seen[normalized] = true
				// Find snippet in next few lines
				snippet := ""
				inResult = true
				results = append(results, SearchResult{
					Title:   currentTitle,
					URL:     normalized,
					Content: snippet,
					Time:    time.Now().Format("15:04"),
				})
			}
			currentTitle = ""
			currentURL = ""
		} else if inResult && strings.Contains(line, "result__snippet") {
			inResult = false
			snippetStart := strings.Index(line, ">")
			snippetEnd := strings.LastIndex(line, "</a>")
			if snippetStart != -1 && snippetEnd != -1 && snippetStart < snippetEnd {
				snippet := line[snippetStart+1 : snippetEnd]
				snippet = stripTags(snippet)
				snippet = cleanTitle(snippet)
				if len(results) > 0 {
					results[len(results)-1].Content = snippet
				}
			}
		}
	}
	return results
}

func stripTags(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		} else if r == '>' {
			if inTag {
				inTag = false
				continue
			}
		}
		if !inTag {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}
