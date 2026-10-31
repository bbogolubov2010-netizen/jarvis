package browser

import (
	"context"
	"testing"
)

func TestSafePublicURLRejectsLocalTargets(t *testing.T) {
	tests := []struct {
		url  string
		safe bool
	}{
		{url: "https://8.8.8.8/path", safe: true},
		{url: "http://[2001:4860:4860::8888]/", safe: true},
		{url: "http://127.0.0.1:8080/", safe: false},
		{url: "http://[::1]/", safe: false},
		{url: "http://192.168.1.1/", safe: false},
		{url: "http://10.0.0.1/", safe: false},
		{url: "http://172.16.0.1/", safe: false},
		{url: "http://169.254.1.1/", safe: false},
		{url: "http://100.64.0.1/", safe: false},
		{url: "http://0.0.0.0/", safe: false},
		{url: "http://localhost/", safe: false},
		{url: "http://printer.local/", safe: false},
		{url: "http://service.internal/", safe: false},
		{url: "ftp://8.8.8.8/", safe: false},
		{url: "https://user:pass@8.8.8.8/", safe: false},
		{url: "not a url", safe: false},
	}
	for _, test := range tests {
		if got := safePublicURL(context.Background(), test.url); got != test.safe {
			t.Errorf("safePublicURL(%q) = %v, want %v", test.url, got, test.safe)
		}
	}
}
