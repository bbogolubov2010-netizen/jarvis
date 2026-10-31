package netguard

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestIsPublicIPRejectsSpecialRanges(t *testing.T) {
	rejected := []string{
		"127.0.0.1", "10.0.0.1", "172.16.0.1", "192.168.1.1", "169.254.1.1",
		"0.0.0.0", "100.64.0.1", "192.0.2.1", "198.18.0.1", "198.51.100.1",
		"203.0.113.1", "224.0.0.1", "255.255.255.255", "::1", "fe80::1",
		"fc00::1", "ff02::1",
	}
	for _, raw := range rejected {
		if IsPublicIP(net.ParseIP(raw)) {
			t.Errorf("IsPublicIP(%s) = true, want false", raw)
		}
	}

	accepted := []string{"8.8.8.8", "1.1.1.1", "93.184.216.34", "2606:4700:4700::1111"}
	for _, raw := range accepted {
		if !IsPublicIP(net.ParseIP(raw)) {
			t.Errorf("IsPublicIP(%s) = false, want true", raw)
		}
	}
}

func TestResolvePublicHostRejectsLocalNames(t *testing.T) {
	for _, host := range []string{
		"localhost", "printer.local", "router.lan", "service.internal",
		"127.0.0.1", "192.168.0.10", "::1", "",
	} {
		if _, err := ResolvePublicHost(context.Background(), host); err == nil {
			t.Errorf("ResolvePublicHost(%q) accepted a local host", host)
		}
	}
}

func TestResolvePublicHostAcceptsLiteralPublicIP(t *testing.T) {
	addresses, err := ResolvePublicHost(context.Background(), "8.8.8.8")
	if err != nil {
		t.Fatalf("ResolvePublicHost: %v", err)
	}
	if len(addresses) != 1 || !addresses[0].Equal(net.ParseIP("8.8.8.8")) {
		t.Fatalf("unexpected addresses: %v", addresses)
	}
}

func TestAllowedPort(t *testing.T) {
	for _, port := range []string{"80", "443"} {
		if !AllowedPort(port) {
			t.Errorf("AllowedPort(%q) = false, want true", port)
		}
	}
	for _, port := range []string{"", "0", "22", "8080", "65536", "abc"} {
		if AllowedPort(port) {
			t.Errorf("AllowedPort(%q) = true, want false", port)
		}
	}
}

func TestIsPublicURLRejectsLocalAndCredentialedTargets(t *testing.T) {
	rejected := []string{
		"http://localhost/",
		"http://127.0.0.1:80/",
		"http://10.0.0.1/",
		"http://169.254.169.254/latest/meta-data/",
		"http://user:pass@example.com/",
		"ftp://example.com/",
		"https://example.com:8443/",
		"",
	}
	for _, raw := range rejected {
		if IsPublicURL(context.Background(), raw) {
			t.Errorf("IsPublicURL(%q) = true, want false", raw)
		}
	}
	if !IsPublicURL(context.Background(), "http://8.8.8.8/") {
		t.Error("IsPublicURL rejected a public literal address")
	}
}

func TestClientRejectsLoopbackBeforeConnecting(t *testing.T) {
	client := NewClient(time.Second)
	response, err := client.Get("http://127.0.0.1:80/")
	if err == nil {
		_ = response.Body.Close()
		t.Fatal("client connected to a loopback address")
	}
}

func TestClientRejectsRedirectToPrivateAddress(t *testing.T) {
	client := NewClient(time.Second)
	request, err := http.NewRequest(http.MethodGet, "http://10.0.0.1/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.CheckRedirect(request, nil); err == nil {
		t.Fatal("redirect to a private address was allowed")
	}
}
