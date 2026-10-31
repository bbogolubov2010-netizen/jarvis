package browser

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"Jarvis/internal/netguard"

	"github.com/chromedp/chromedp"
)

// egressProxy is a loopback-only HTTP proxy used by Chromium. Every request
// (including subresources and redirects) has to pass through it, so private,
// loopback, link-local, and DNS-rebinding destinations are rejected before a
// socket is opened.
type egressProxy struct {
	listener  net.Listener
	server    *http.Server
	transport *http.Transport

	mu      sync.Mutex
	conns   map[net.Conn]struct{}
	closed  bool
	closeMu sync.Once
}

func startEgressProxy() (*egressProxy, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start browser egress proxy: %w", err)
	}

	proxy := &egressProxy{
		listener: listener,
		conns:    make(map[net.Conn]struct{}),
	}
	proxy.transport = &http.Transport{
		Proxy:                 nil,
		DialContext:           proxy.dialContext,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          16,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
	}
	proxy.server = &http.Server{
		Handler:           proxy,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		_ = proxy.server.Serve(listener)
	}()
	return proxy, nil
}

func (p *egressProxy) URL() string {
	return "http://" + p.listener.Addr().String()
}

func (p *egressProxy) Close() {
	p.closeMu.Do(func() {
		_ = p.server.Close()
		p.transport.CloseIdleConnections()

		p.mu.Lock()
		p.closed = true
		for conn := range p.conns {
			_ = conn.Close()
		}
		p.conns = nil
		p.mu.Unlock()
	})
}

func (p *egressProxy) track(conn net.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		_ = conn.Close()
		return false
	}
	p.conns[conn] = struct{}{}
	return true
}

func (p *egressProxy) untrack(conn net.Conn) {
	p.mu.Lock()
	if p.conns != nil {
		delete(p.conns, conn)
	}
	p.mu.Unlock()
}

func (p *egressProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		p.handleConnect(w, r)
		return
	}
	p.handleHTTP(w, r)
}

func (p *egressProxy) handleHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL == nil || !r.URL.IsAbs() || r.URL.Host == "" {
		http.Error(w, "proxy requires an absolute URL", http.StatusBadRequest)
		return
	}
	if r.URL.Scheme != "http" && r.URL.Scheme != "https" {
		http.Error(w, "only http and https are allowed", http.StatusForbidden)
		return
	}
	if r.URL.User != nil || !safePublicURL(r.Context(), r.URL.String()) {
		http.Error(w, "destination is not a public web address", http.StatusForbidden)
		return
	}
	if !allowedProxyPort(r.URL.Port(), r.URL.Scheme) {
		http.Error(w, "destination port is not allowed", http.StatusForbidden)
		return
	}

	out := r.Clone(r.Context())
	out.RequestURI = ""
	out.Header = r.Header.Clone()
	removeHopByHop(out.Header)
	out.Header.Del("Proxy-Authorization")
	out.Header.Del("Proxy-Connection")

	response, err := p.transport.RoundTrip(out)
	if err != nil {
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()

	removeHopByHop(response.Header)
	copyHeader(w.Header(), response.Header)
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(response.Body, 16<<20))
}

func (p *egressProxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	target := r.Host
	if target == "" && r.URL != nil {
		target = r.URL.Host
	}
	host, port, err := splitTarget(target, "443")
	if err != nil {
		http.Error(w, "invalid CONNECT target", http.StatusBadRequest)
		return
	}
	if !allowedProxyPort(port, "https") {
		http.Error(w, "destination port is not allowed", http.StatusForbidden)
		return
	}
	if _, err := resolvePublicHost(r.Context(), host); err != nil {
		http.Error(w, "destination is not a public web address", http.StatusForbidden)
		return
	}

	upstream, err := p.dialResolved(r.Context(), host, port)
	if err != nil {
		http.Error(w, "upstream connection failed", http.StatusBadGateway)
		return
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		_ = upstream.Close()
		http.Error(w, "proxy does not support tunneling", http.StatusInternalServerError)
		return
	}
	client, buffered, err := hijacker.Hijack()
	if err != nil {
		_ = upstream.Close()
		return
	}
	if !p.track(client) {
		_ = upstream.Close()
		return
	}
	defer p.untrack(client)
	defer client.Close()
	defer upstream.Close()

	if _, err := client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		return
	}

	// Copy any bytes already buffered by net/http before starting the tunnel.
	if buffered != nil && buffered.Reader.Buffered() > 0 {
		if _, err := io.CopyN(upstream, buffered, int64(buffered.Reader.Buffered())); err != nil {
			return
		}
	}

	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(upstream, client)
		closeWrite(upstream)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, upstream)
		closeWrite(client)
		done <- struct{}{}
	}()
	<-done
}

func (p *egressProxy) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if !allowedProxyPort(port, "http") {
		return nil, fmt.Errorf("destination port %q is not allowed", port)
	}
	return p.dialResolved(ctx, host, port)
}

func (p *egressProxy) dialResolved(ctx context.Context, host, port string) (net.Conn, error) {
	return netguard.DialHostPort(ctx, host, port)
}

func resolvePublicHost(ctx context.Context, host string) ([]net.IP, error) {
	return netguard.ResolvePublicHost(ctx, host)
}

func splitTarget(target, defaultPort string) (string, string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", "", errors.New("empty target")
	}
	if host, port, err := net.SplitHostPort(target); err == nil {
		if host == "" || port == "" {
			return "", "", errors.New("invalid target")
		}
		return host, port, nil
	}
	return target, defaultPort, nil
}

func allowedProxyPort(port, scheme string) bool {
	if port == "" {
		return scheme == "http" || scheme == "https"
	}
	value, err := strconv.Atoi(port)
	if err != nil || value < 1 || value > 65535 {
		return false
	}
	return value == 80 || value == 443
}

func removeHopByHop(header http.Header) {
	for _, name := range []string{
		"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate",
		"Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade",
	} {
		header.Del(name)
	}
}

func copyHeader(dst, src http.Header) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func closeWrite(conn net.Conn) {
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.CloseWrite()
	}
}

// NewAllocator starts the filtering proxy and returns a chromedp allocator
// configured to send all browser traffic through it.
func NewAllocator() (context.Context, context.CancelFunc, error) {
	proxy, err := startEgressProxy()
	if err != nil {
		return nil, nil, err
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(),
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("disable-sync", true),
		chromedp.Flag("disable-background-networking", true),
		chromedp.Flag("disable-default-apps", true),
		chromedp.Flag("mute-audio", true),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("disable-quic", true),
		chromedp.Flag("proxy-server", proxy.URL()),
		chromedp.Flag("proxy-bypass-list", "<-loopback>"),
		chromedp.Flag("host-resolver-rules", "MAP * ~NOTFOUND"),
		chromedp.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"),
	)

	cancel := func() {
		allocCancel()
		proxy.Close()
	}
	return allocCtx, cancel, nil
}

// IsPublicURL reports whether a URL is an http(s) address whose host resolves
// only to public addresses. It is used by the app before handing a URL to the
// system browser.
func IsPublicURL(ctx context.Context, raw string) bool {
	return safePublicURL(ctx, raw)
}
