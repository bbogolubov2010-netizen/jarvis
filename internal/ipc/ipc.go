// Package ipc provides authenticated loopback command delivery between the
// background voice agent and the foreground JARVIS window.
package ipc

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"Jarvis/internal/dpapi"
)

const (
	endpointName     = "voice-ipc.json"
	maxEndpointBytes = 8 << 10
	maxRequestBytes  = 32 << 10
	maxCommandBytes  = 16 << 10
	maxNonceBytes    = 128
	maxClockSkew     = 30 * time.Second
	replayWindow     = 2 * time.Minute
)

type endpointFile struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
}

type request struct {
	Version   int    `json:"version"`
	Command   string `json:"command"`
	Timestamp int64  `json:"timestamp"`
	Nonce     string `json:"nonce"`
	MAC       string `json:"mac"`
}

type response struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// Server accepts authenticated commands on a random loopback port.
type Server struct {
	listener net.Listener
	path     string
	token    []byte
	callback func(string)

	done      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup

	seenMu sync.Mutex
	seen   map[string]time.Time
}

// Start creates the loopback server and writes an encrypted endpoint file in
// configDir. The endpoint is readable only by the current Windows user because
// the token is protected with DPAPI.
func Start(configDir string, callback func(string)) (*Server, error) {
	if callback == nil {
		return nil, errors.New("ipc callback is required")
	}
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return nil, fmt.Errorf("create ipc directory: %w", err)
	}

	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return nil, fmt.Errorf("generate ipc token: %w", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start ipc listener: %w", err)
	}
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		_ = listener.Close()
		return nil, errors.New("ipc listener is not TCP")
	}

	server := &Server{
		listener: listener,
		path:     filepath.Join(configDir, endpointName),
		token:    token,
		callback: callback,
		done:     make(chan struct{}),
		seen:     make(map[string]time.Time),
	}
	if err := writeEndpoint(server.path, endpointFile{Port: address.Port, Token: hex.EncodeToString(token)}); err != nil {
		_ = listener.Close()
		return nil, err
	}

	server.wg.Add(1)
	go server.serve()
	return server, nil
}

// Close stops the listener and removes the endpoint file.
func (s *Server) Close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		close(s.done)
		_ = s.listener.Close()
		s.wg.Wait()
		_ = os.Remove(s.path)
	})
}

func (s *Server) serve() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
			}
			if temporary, ok := err.(interface{ Temporary() bool }); ok && temporary.Temporary() {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			return
		}

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handle(conn)
		}()
	}
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	reader := bufio.NewReader(io.LimitReader(conn, maxRequestBytes+1))
	line, err := reader.ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return
	}
	if len(line) == 0 || len(line) > maxRequestBytes {
		writeResponse(conn, response{Error: "request too large"})
		return
	}

	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		writeResponse(conn, response{Error: "invalid request"})
		return
	}
	if err := s.verify(req); err != nil {
		writeResponse(conn, response{Error: err.Error()})
		return
	}
	if !s.acceptNonce(req.Nonce) {
		writeResponse(conn, response{Error: "replayed request"})
		return
	}

	writeResponse(conn, response{OK: true})
	go func() {
		defer func() { _ = recover() }()
		s.callback(req.Command)
	}()
}

func (s *Server) verify(req request) error {
	if req.Version != 1 {
		return errors.New("unsupported request version")
	}
	if strings.TrimSpace(req.Command) == "" {
		return errors.New("empty command")
	}
	if len(req.Command) > maxCommandBytes {
		return errors.New("command too large")
	}
	if len(req.Nonce) == 0 || len(req.Nonce) > maxNonceBytes {
		return errors.New("invalid nonce")
	}
	now := time.Now()
	sent := time.Unix(req.Timestamp, 0)
	if sent.Before(now.Add(-maxClockSkew)) || sent.After(now.Add(maxClockSkew)) {
		return errors.New("request timestamp is outside the allowed window")
	}

	expected := sign(s.token, req.Command, req.Timestamp, req.Nonce)
	actual, err := base64.StdEncoding.DecodeString(req.MAC)
	if err != nil {
		return errors.New("invalid signature")
	}
	if subtle.ConstantTimeCompare(expected, actual) != 1 {
		return errors.New("invalid signature")
	}
	return nil
}

func (s *Server) acceptNonce(nonce string) bool {
	now := time.Now()
	s.seenMu.Lock()
	defer s.seenMu.Unlock()
	for value, expiry := range s.seen {
		if now.After(expiry) {
			delete(s.seen, value)
		}
	}
	if expiry, exists := s.seen[nonce]; exists && now.Before(expiry) {
		return false
	}
	s.seen[nonce] = now.Add(replayWindow)
	return true
}

func writeResponse(conn net.Conn, resp response) {
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	data = append(data, '\n')
	_, _ = conn.Write(data)
}

func sign(token []byte, command string, timestamp int64, nonce string) []byte {
	mac := hmac.New(sha256.New, token)
	_, _ = fmt.Fprintf(mac, "jarvis-ipc-v1\n%s\n%d\n%s", command, timestamp, nonce)
	return mac.Sum(nil)
}

func endpointPath(configDir string) string {
	return filepath.Join(configDir, endpointName)
}

func writeEndpoint(path string, endpoint endpointFile) error {
	data, err := json.Marshal(endpoint)
	if err != nil {
		return fmt.Errorf("encode ipc endpoint: %w", err)
	}
	encrypted, err := dpapi.Encrypt(data)
	if err != nil {
		return fmt.Errorf("encrypt ipc endpoint: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create ipc directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create ipc endpoint: %w", err)
	}
	tempName := temp.Name()
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(tempName)
		}
	}()

	if _, err := temp.Write(encrypted); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write ipc endpoint: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync ipc endpoint: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close ipc endpoint: %w", err)
	}
	if err := os.Rename(tempName, path); err != nil {
		return fmt.Errorf("replace ipc endpoint: %w", err)
	}
	renamed = true
	return nil
}

func readEndpoint(path string) (endpointFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return endpointFile{}, err
	}
	if len(data) == 0 || len(data) > maxEndpointBytes {
		return endpointFile{}, errors.New("invalid ipc endpoint size")
	}
	decrypted, err := dpapi.Decrypt(data)
	if err != nil {
		return endpointFile{}, fmt.Errorf("decrypt ipc endpoint: %w", err)
	}
	var endpoint endpointFile
	if err := json.Unmarshal(decrypted, &endpoint); err != nil {
		return endpointFile{}, fmt.Errorf("parse ipc endpoint: %w", err)
	}
	if endpoint.Port < 1 || endpoint.Port > 65535 || endpoint.Token == "" {
		return endpointFile{}, errors.New("invalid ipc endpoint")
	}
	return endpoint, nil
}

// SendCommand waits for the foreground app endpoint and delivers one
// authenticated command. The command is never placed on a process command
// line.
func SendCommand(ctx context.Context, configDir, command string) error {
	command = strings.TrimSpace(command)
	if command == "" {
		return errors.New("empty command")
	}
	if len(command) > maxCommandBytes {
		return errors.New("command too large")
	}

	var lastErr error
	for {
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return fmt.Errorf("send ipc command: %w (last error: %v)", err, lastErr)
			}
			return fmt.Errorf("send ipc command: %w", err)
		}

		endpoint, err := readEndpoint(endpointPath(configDir))
		if err == nil {
			err = sendOnce(ctx, endpoint, command)
			if err == nil {
				return nil
			}
			lastErr = err
		} else {
			lastErr = err
		}

		select {
		case <-ctx.Done():
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func sendOnce(ctx context.Context, endpoint endpointFile, command string) error {
	token, err := hex.DecodeString(endpoint.Token)
	if err != nil || len(token) < 16 {
		return errors.New("invalid ipc token")
	}
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return fmt.Errorf("generate nonce: %w", err)
	}
	nonce := hex.EncodeToString(nonceBytes)
	timestamp := time.Now().Unix()
	req := request{
		Version:   1,
		Command:   command,
		Timestamp: timestamp,
		Nonce:     nonce,
		MAC:       base64.StdEncoding.EncodeToString(sign(token, command, timestamp, nonce)),
	}
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("encode ipc request: %w", err)
	}
	data = append(data, '\n')

	dialer := &net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", endpoint.Port))
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write(data); err != nil {
		return err
	}

	line, err := bufio.NewReader(io.LimitReader(conn, maxRequestBytes)).ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	var resp response
	if err := json.Unmarshal(line, &resp); err != nil {
		return fmt.Errorf("decode ipc response: %w", err)
	}
	if !resp.OK {
		if resp.Error == "" {
			resp.Error = "command rejected"
		}
		return errors.New(resp.Error)
	}
	return nil
}
