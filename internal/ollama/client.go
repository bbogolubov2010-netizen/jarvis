// Package ollama is a small client for the local Ollama HTTP API.
package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultBaseURL is Ollama's default local endpoint.
	DefaultBaseURL = "http://localhost:11434"
	// DefaultModel fits an 8 GB machine.
	DefaultModel = "llama3.1:8b"

	maxResponseBytes = 8 << 20
	requestTimeout   = 120 * time.Second
)

// Message is one chat message, optionally with base64-encoded images.
type Message struct {
	Role    string   `json:"role"`
	Content string   `json:"content"`
	Images  []string `json:"images,omitempty"`
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type chatResponse struct {
	Message Message `json:"message"`
	Done    bool    `json:"done"`
}

type chatStreamChunk struct {
	Message Message `json:"message"`
	Done    bool    `json:"done"`
	Error   string  `json:"error"`
}

type listResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

type pullRequest struct {
	Name   string `json:"name"`
	Stream bool   `json:"stream"`
}

type pullEvent struct {
	Status    string `json:"status"`
	Digest    string `json:"digest"`
	Total     int64  `json:"total"`
	Completed int64  `json:"completed"`
	Error     string `json:"error"`
}

// PullProgress is one progress update from Ollama's pull stream.
type PullProgress struct {
	Status    string `json:"status"`
	Digest    string `json:"digest"`
	Total     int64  `json:"total"`
	Completed int64  `json:"completed"`
	Percent   int    `json:"percent"`
}

// Client is safe for concurrent use.
type Client struct {
	mu      sync.RWMutex
	baseURL string
	model   string
	http    *http.Client
}

// NewClient creates a client with sane defaults.
func NewClient(baseURL, model string) *Client {
	validated, err := ValidateBaseURL(baseURL)
	if err != nil {
		validated = DefaultBaseURL
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = DefaultModel
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 10
	transport.IdleConnTimeout = 30 * time.Second
	return &Client{
		baseURL: validated,
		model:   model,
		http: &http.Client{
			Timeout:   requestTimeout,
			Transport: transport,
		},
	}
}

// ValidateBaseURL accepts only absolute http(s) URLs and strips trailing slashes.
func ValidateBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(strings.TrimRight(raw, "/"))
	if raw == "" {
		return "", fmt.Errorf("empty Ollama URL")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid Ollama URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("Ollama URL must use http or https")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("Ollama URL has no host")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("Ollama URL must not contain a query or fragment")
	}
	return raw, nil
}

// SetBaseURL validates and stores the endpoint.
func (c *Client) SetBaseURL(raw string) error {
	validated, err := ValidateBaseURL(raw)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.baseURL = validated
	c.mu.Unlock()
	return nil
}

// BaseURL returns the current endpoint.
func (c *Client) BaseURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURL
}

// SetModel stores the model name.
func (c *Client) SetModel(model string) {
	model = strings.TrimSpace(model)
	if model == "" {
		return
	}
	c.mu.Lock()
	c.model = model
	c.mu.Unlock()
}

// Model returns the current model name.
func (c *Client) Model() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.model
}

func (c *Client) snapshot() (string, string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURL, c.model
}

// Chat sends a non-streaming chat request and returns the assistant reply.
func (c *Client) Chat(ctx context.Context, messages []Message) (string, error) {
	if len(messages) == 0 {
		return "", fmt.Errorf("no messages to send")
	}
	baseURL, model := c.snapshot()

	payload, err := json.Marshal(chatRequest{
		Model:    model,
		Messages: messages,
		Stream:   false,
	})
	if err != nil {
		return "", fmt.Errorf("encode chat request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/chat", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("build chat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("connect to Ollama at %s: %w", baseURL, err)
	}
	defer resp.Body.Close()

	body := io.LimitReader(resp.Body, maxResponseBytes)
	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(body, 4096))
		return "", fmt.Errorf("Ollama error (%d): %s", resp.StatusCode, strings.TrimSpace(string(detail)))
	}

	var parsed chatResponse
	if err := json.NewDecoder(body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("decode Ollama response: %w", err)
	}
	return parsed.Message.Content, nil
}

// ChatStream sends a streaming chat request and invokes onDelta for every chunk.
// Ollama streams newline-delimited JSON objects, not server-sent events.
func (c *Client) ChatStream(ctx context.Context, messages []Message, onDelta func(string) error) (string, error) {
	if len(messages) == 0 {
		return "", fmt.Errorf("no messages to send")
	}
	baseURL, model := c.snapshot()

	payload, err := json.Marshal(chatRequest{
		Model:    model,
		Messages: messages,
		Stream:   true,
	})
	if err != nil {
		return "", fmt.Errorf("encode chat request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/chat", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("build chat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// The regular client timeout covers slow model loading and long generations.
	// A streaming request is bounded by its context instead.
	streamClient := *c.http
	streamClient.Timeout = 0
	resp, err := streamClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("connect to Ollama at %s: %w", baseURL, err)
	}
	defer resp.Body.Close()

	body := io.LimitReader(resp.Body, maxResponseBytes)
	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(body, 4096))
		return "", fmt.Errorf("Ollama error (%d): %s", resp.StatusCode, strings.TrimSpace(string(detail)))
	}

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), maxResponseBytes)

	var full strings.Builder
	sawDone := false
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		var chunk chatStreamChunk
		if err := json.Unmarshal(line, &chunk); err != nil {
			return "", fmt.Errorf("decode Ollama stream chunk: %w", err)
		}
		if chunk.Error != "" {
			return "", fmt.Errorf("Ollama stream error: %s", chunk.Error)
		}
		if chunk.Message.Content != "" {
			full.WriteString(chunk.Message.Content)
			if onDelta != nil {
				if err := onDelta(chunk.Message.Content); err != nil {
					return "", fmt.Errorf("stream callback: %w", err)
				}
			}
		}
		if chunk.Done {
			sawDone = true
			break
		}
	}
	if err := scanner.Err(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", fmt.Errorf("read Ollama stream: %w", err)
	}
	if !sawDone {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", fmt.Errorf("Ollama stream ended before done")
	}
	return full.String(), nil
}

// ListModels returns locally installed models.
func (c *Client) ListModels(ctx context.Context) ([]string, error) {
	baseURL, _ := c.snapshot()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("build models request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connect to Ollama: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Ollama returned %d", resp.StatusCode)
	}

	var parsed listResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode models: %w", err)
	}
	models := make([]string, 0, len(parsed.Models))
	for _, item := range parsed.Models {
		if name := strings.TrimSpace(item.Name); name != "" {
			models = append(models, name)
		}
	}
	return models, nil
}

// PullModel downloads a model through the configured Ollama endpoint.
func (c *Client) PullModel(ctx context.Context, name string) error {
	return c.PullModelWithProgress(ctx, name, nil)
}

// PullModelWithProgress downloads a model and reports Ollama's download
// progress. The callback is invoked for every stream event; callers that emit
// UI events should throttle it.
func (c *Client) PullModelWithProgress(ctx context.Context, name string, onProgress func(PullProgress) error) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("model name is empty")
	}

	payload, err := json.Marshal(pullRequest{Name: name, Stream: true})
	if err != nil {
		return fmt.Errorf("encode pull request: %w", err)
	}
	baseURL, _ := c.snapshot()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/pull", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build pull request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// A model download can take much longer than the normal request timeout.
	streamClient := *c.http
	streamClient.Timeout = 0
	resp, err := streamClient.Do(req)
	if err != nil {
		return fmt.Errorf("connect to Ollama: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("Ollama pull returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	sawSuccess := false
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var event pullEvent
		if err := json.Unmarshal(line, &event); err != nil {
			return fmt.Errorf("decode pull response: %w", err)
		}
		if event.Error != "" {
			return fmt.Errorf("Ollama pull failed: %s", event.Error)
		}
		if event.Status == "success" {
			sawSuccess = true
		}
		if onProgress != nil {
			percent := 0
			if event.Total > 0 {
				percent = int(event.Completed * 100 / event.Total)
				if percent < 0 {
					percent = 0
				}
				if percent > 100 {
					percent = 100
				}
			}
			if err := onProgress(PullProgress{
				Status:    event.Status,
				Digest:    event.Digest,
				Total:     event.Total,
				Completed: event.Completed,
				Percent:   percent,
			}); err != nil {
				return fmt.Errorf("pull progress callback: %w", err)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read pull response: %w", err)
	}
	if !sawSuccess {
		return fmt.Errorf("Ollama pull ended before completion")
	}
	return nil
}

// Healthy reports whether the Ollama API answers.
func (c *Client) Healthy(ctx context.Context) error {
	baseURL, _ := c.snapshot()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/tags", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Ollama returned %d", resp.StatusCode)
	}
	return nil
}
