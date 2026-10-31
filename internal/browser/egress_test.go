package browser

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestEgressProxyRejectsLocalHTTPTargets(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("local secret"))
	}))
	defer upstream.Close()

	proxy, err := startEgressProxy()
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()

	proxyURL, err := url.Parse(proxy.URL())
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
	}
	defer client.CloseIdleConnections()

	response, err := client.Get(upstream.URL)
	if err == nil {
		defer response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("local request status = %d, want 403", response.StatusCode)
		}
	}
}

func TestEgressProxyRejectsPrivateResolvedHost(t *testing.T) {
	proxy, err := startEgressProxy()
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()

	for _, host := range []string{"127.0.0.1", "localhost", "10.0.0.1", "printer.local"} {
		if _, err := resolvePublicHost(context.Background(), host); err == nil {
			t.Errorf("resolvePublicHost(%q) accepted a local host", host)
		}
	}
}

func TestEgressProxyAllowsDocumentedPublicAddress(t *testing.T) {
	addresses, err := resolvePublicHost(context.Background(), "8.8.8.8")
	if err != nil {
		t.Fatalf("public address rejected: %v", err)
	}
	if len(addresses) != 1 || addresses[0].String() != "8.8.8.8" {
		t.Fatalf("unexpected addresses: %v", addresses)
	}
}

func TestPublicIPRejectsSpecialRanges(t *testing.T) {
	for _, raw := range []string{
		"0.0.0.0", "100.64.0.1", "192.0.2.1", "198.18.0.1",
		"198.51.100.1", "203.0.113.1", "224.0.0.1", "255.255.255.255",
	} {
		if publicIP(net.ParseIP(raw)) {
			t.Errorf("publicIP(%s) = true, want false", raw)
		}
	}
}
