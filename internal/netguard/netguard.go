// Package netguard validates outbound network destinations.
//
// JARVIS may only talk to public web addresses. A model response or a fetched
// page can contain a URL that points at localhost, a router, or a cloud
// metadata endpoint; resolving and dialing through this package rejects those
// destinations before a socket is opened.
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	dialTimeout    = 10 * time.Second
	resolveTimeout = 3 * time.Second
	maxRedirects   = 5
)

// IsPublicIP reports whether an address is routable on the public internet.
// Loopback, private, link-local, carrier-grade NAT, documentation, and
// multicast ranges are rejected.
func IsPublicIP(ip net.IP) bool {
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

// IsLocalHostname reports names that always resolve to the local machine.
func IsLocalHostname(host string) bool {
	return host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") ||
		strings.HasSuffix(host, ".lan") || strings.HasSuffix(host, ".home")
}

// ResolvePublicHost resolves a host name and fails if any returned address is
// not public. Rejecting the whole set prevents a DNS-rebinding split where one
// address is public and another is local.
func ResolvePublicHost(ctx context.Context, host string) ([]net.IP, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" || IsLocalHostname(host) {
		return nil, errors.New("local hostname")
	}
	if ip := net.ParseIP(host); ip != nil {
		if !IsPublicIP(ip) {
			return nil, errors.New("private address")
		}
		return []net.IP{ip}, nil
	}

	lookupCtx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupIPAddr(lookupCtx, host)
	if err != nil || len(addresses) == 0 {
		return nil, fmt.Errorf("resolve %q: %w", host, err)
	}
	ips := make([]net.IP, 0, len(addresses))
	for _, address := range addresses {
		if !IsPublicIP(address.IP) {
			return nil, fmt.Errorf("resolve %q: non-public address", host)
		}
		ips = append(ips, address.IP)
	}
	return ips, nil
}

// AllowedPort reports whether an outbound connection may use this port.
func AllowedPort(port string) bool {
	value, err := strconv.Atoi(port)
	if err != nil || value < 1 || value > 65535 {
		return false
	}
	return value == 80 || value == 443
}

// DialHostPort dials host:port after verifying that every address it resolves
// to is public. The connection is opened to the already-resolved IP, so a
// second DNS answer cannot redirect it to a private address.
func DialHostPort(ctx context.Context, host, port string) (net.Conn, error) {
	addresses, err := ResolvePublicHost(ctx, host)
	if err != nil {
		return nil, err
	}

	dialer := &net.Dialer{Timeout: dialTimeout}
	var lastErr error
	for _, ip := range addresses {
		conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no public address")
	}
	return nil, lastErr
}

// DialContext is an http.Transport-compatible dialer restricted to public
// addresses on ports 80 and 443.
func DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if !AllowedPort(port) {
		return nil, fmt.Errorf("destination port %q is not allowed", port)
	}
	return DialHostPort(ctx, host, port)
}

// IsPublicURL reports whether a URL is an http(s) address whose host resolves
// only to public addresses. Credentials in the URL are rejected.
func IsPublicURL(ctx context.Context, raw string) bool {
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
	if !AllowedPort(portOrDefault(parsed.Port(), parsed.Scheme)) {
		return false
	}
	_, err = ResolvePublicHost(ctx, host)
	return err == nil
}

func portOrDefault(port, scheme string) string {
	if port != "" {
		return port
	}
	if scheme == "https" {
		return "443"
	}
	return "80"
}

// NewClient returns an HTTP client that refuses non-public destinations and
// re-validates every redirect. Timeouts mirror the headless browser so a slow
// or hostile endpoint cannot stall the caller.
func NewClient(timeout time.Duration) *http.Client {
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           DialContext,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          16,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.New("too many redirects")
			}
			if !IsPublicURL(req.Context(), req.URL.String()) {
				return errors.New("redirect to a non-public address")
			}
			return nil
		},
	}
}
