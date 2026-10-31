//go:build manual

package netguard

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

// TestManualPublicRequest verifies the guarded client against a real public
// endpoint. It needs network access and is excluded from the normal CI run.
func TestManualPublicRequest(t *testing.T) {
	client := NewClient(15 * time.Second)
	response, err := client.Get("https://api.duckduckgo.com/?q=test&format=json&no_html=1")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<16))
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		t.Fatalf("status = %d, want 2xx", response.StatusCode)
	}
	if len(body) == 0 {
		t.Fatal("empty body")
	}
}

// TestManualPrivateRequestIsRejected proves the guard works at dial time.
func TestManualPrivateRequestIsRejected(t *testing.T) {
	client := NewClient(5 * time.Second)
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://192.168.1.1/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if response, err := client.Do(request); err == nil {
		_ = response.Body.Close()
		t.Fatal("client reached a private address")
	}
}
