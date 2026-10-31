// Package browser performs a browser-based web search with a headless Chromium.
package browser

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"Jarvis/internal/jobobject"
	"Jarvis/internal/sanitize"

	"github.com/chromedp/chromedp"
)

const jsExtractResults = `
(() => {
	const links = document.querySelectorAll('.result__a');
	const snippets = document.querySelectorAll('.result__snippet');
	const out = [];
	for (let i = 0; i < Math.min(links.length, 5); i++) {
		const link = links[i];
		const snippet = snippets[i];
		out.push({
			title: link.textContent.trim(),
			url: link.href,
			snippet: snippet ? snippet.textContent.trim() : ''
		});
	}
	return out;
})()
`

const jsPageText = `(() => document.body ? document.body.innerText.slice(0, 8000) : '')()`

// SearchResult is one search hit.
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

var (
	jobMu sync.Mutex
	job   *jobobject.Handle
)

// InitJobObject creates a job object that terminates leftover Chromium
// processes when JARVIS exits. Failure is non-fatal.
func InitJobObject() error {
	jobMu.Lock()
	defer jobMu.Unlock()

	if job != nil {
		return nil
	}
	created, err := jobobject.Create()
	if err != nil {
		return err
	}
	job = created

	go func() {
		for attempt := 0; attempt < 30; attempt++ {
			time.Sleep(time.Second)
			if err := assignChildProcesses(); err != nil {
				return
			}
		}
	}()
	return nil
}

// CloseJobObject terminates Chromium processes started by JARVIS.
func CloseJobObject() {
	jobMu.Lock()
	defer jobMu.Unlock()
	if job != nil {
		job.Close()
		job = nil
	}
}

func assignChildProcesses() error {
	jobMu.Lock()
	handle := job
	jobMu.Unlock()
	if handle == nil {
		return nil
	}
	return handle.AssignChildProcesses()
}

// SmartSearch queries DuckDuckGo and returns results plus the sanitized text of
// the first result page.
// safePublicURL rejects non-HTTP schemes, credentials, local hostnames, and
// private/link-local addresses. Search results are untrusted input; without
// this check a malicious result could make the headless browser read a local
// service and feed its response to the model.
func safePublicURL(ctx context.Context, raw string) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" || parsed.User != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if isLocalHostname(host) {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return publicIP(ip)
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupIPAddr(lookupCtx, host)
	if err != nil || len(addresses) == 0 {
		return false
	}
	for _, address := range addresses {
		if !publicIP(address.IP) {
			return false
		}
	}
	return true
}

func publicIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	if ip4 := ip.To4(); ip4 != nil {
		// Reject special-purpose and documentation ranges in addition to the
		// standard private ranges handled by net.IP.IsPrivate.
		if ip4[0] == 0 || ip4[0] >= 224 {
			return false
		}
		if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
			return false // carrier-grade NAT
		}
		if ip4[0] == 192 && ip4[1] == 0 && (ip4[2] == 0 || ip4[2] == 2) {
			return false // IETF protocol assignments / TEST-NET-1
		}
		if ip4[0] == 198 && (ip4[1] == 18 || ip4[1] == 19) {
			return false // benchmarking
		}
		if ip4[0] == 198 && ip4[1] == 51 && ip4[2] == 100 {
			return false // TEST-NET-2
		}
		if ip4[0] == 203 && ip4[1] == 0 && ip4[2] == 113 {
			return false // TEST-NET-3
		}
	}
	return true
}

func SmartSearch(ctx context.Context, allocCtx context.Context, query string) ([]SearchResult, string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, "", fmt.Errorf("empty search query")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}

	chromeCtx, chromeCancel := chromedp.NewContext(allocCtx)
	defer chromeCancel()

	searchCtx, searchCancel := context.WithTimeout(chromeCtx, 60*time.Second)
	defer searchCancel()
	go func() {
		select {
		case <-ctx.Done():
			searchCancel()
		case <-searchCtx.Done():
		}
	}()

	_ = assignChildProcesses()

	var results []SearchResult
	err := chromedp.Run(searchCtx,
		chromedp.Navigate("https://html.duckduckgo.com/html/?q="+url.QueryEscape(query)),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Sleep(1200*time.Millisecond),
		chromedp.Evaluate(jsExtractResults, &results),
	)
	if err != nil {
		return nil, "", fmt.Errorf("search failed: %w", err)
	}
	if len(results) == 0 {
		return results, "", nil
	}

	_ = assignChildProcesses()

	firstURL := results[0].URL
	if !safePublicURL(searchCtx, firstURL) {
		return results, "", nil
	}

	var pageContent, finalURL string
	pageErr := chromedp.Run(searchCtx,
		chromedp.Navigate(firstURL),
		chromedp.Location(&finalURL),
		chromedp.Sleep(1500*time.Millisecond),
		chromedp.Evaluate(jsPageText, &pageContent),
	)
	if pageErr != nil {
		return results, "", nil
	}
	// A public result can redirect to a local or link-local address. Validate
	// the final location before reading page text.
	if !safePublicURL(searchCtx, finalURL) {
		return results, "", nil
	}

	sanitized := sanitize.SanitizeWebContent(pageContent)
	content := sanitized.Content
	if sanitized.HadInjection {
		content = "[Some page text was removed because it looked like a prompt-injection attempt.]\n" + content
	}
	return results, content, nil
}
