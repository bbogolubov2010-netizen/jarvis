//go:build windows

package setup

import (
	"net/http"
	"net/url"
	"testing"
)

func installerRequest(t *testing.T, raw string) *http.Request {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func TestInstallerRedirectRequiresHTTPS(t *testing.T) {
	request := installerRequest(t, "http://1.1.1.1/OllamaSetup.exe")
	if err := installerRedirect(request, nil); err == nil {
		t.Fatal("plain HTTP redirect was accepted")
	}
}

func TestInstallerRedirectRejectsPrivateAddress(t *testing.T) {
	for _, raw := range []string{
		"https://127.0.0.1/OllamaSetup.exe",
		"https://10.0.0.1/OllamaSetup.exe",
		"https://localhost/OllamaSetup.exe",
	} {
		request := installerRequest(t, raw)
		if err := installerRedirect(request, nil); err == nil {
			t.Fatalf("redirect to %s was accepted", raw)
		}
	}
}

func TestInstallerRedirectAllowsPublicHTTPS(t *testing.T) {
	request := installerRequest(t, "https://1.1.1.1/OllamaSetup.exe")
	if err := installerRedirect(request, nil); err != nil {
		t.Fatalf("public HTTPS redirect was rejected: %v", err)
	}
}

func TestInstallerRedirectLimitsHops(t *testing.T) {
	request := installerRequest(t, "https://1.1.1.1/OllamaSetup.exe")
	via := make([]*http.Request, maxInstallerRedirects)
	for i := range via {
		via[i] = &http.Request{URL: &url.URL{Scheme: "https", Host: "1.1.1.1"}}
	}
	if err := installerRedirect(request, via); err == nil {
		t.Fatal("redirect chain longer than the limit was accepted")
	}
}

func TestNewInstallerClientHasNoDeadline(t *testing.T) {
	client := newInstallerClient()
	if client.Timeout != 0 {
		t.Fatalf("installer client timeout = %v, want 0", client.Timeout)
	}
	if client.CheckRedirect == nil {
		t.Fatal("installer client has no redirect policy")
	}
}
