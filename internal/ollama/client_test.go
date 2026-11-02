package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestValidateBaseURL(t *testing.T) {
	valid := map[string]string{
		"http://localhost:11434/":      "http://localhost:11434",
		"http://127.0.0.1:11434":       "http://127.0.0.1:11434",
		"http://[::1]:11434":           "http://[::1]:11434",
		"https://localhost:11434":      "https://localhost:11434",
		" https://ollama.example.com ": "https://ollama.example.com",
	}
	for input, want := range valid {
		got, err := ValidateBaseURL(input)
		if err != nil {
			t.Fatalf("ValidateBaseURL(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("ValidateBaseURL(%q) = %q, want %q", input, got, want)
		}
	}

	for _, input := range []string{
		"", "localhost:11434", "ftp://example.com", "http://",
		"http://localhost:11434/?token=secret", "http://localhost:11434/#fragment",
		"http://ollama.example.com:11434", "http://192.168.1.10:11434",
		"https://user:pass@ollama.example.com",
	} {
		if _, err := ValidateBaseURL(input); err == nil {
			t.Fatalf("ValidateBaseURL(%q) should fail", input)
		}
	}
}

func TestValidateBaseURLRequiresHTTPSForRemoteHosts(t *testing.T) {
	_, err := ValidateBaseURL("http://ollama.example.com")
	if err == nil {
		t.Fatal("remote plain HTTP endpoint was accepted")
	}
	if !strings.Contains(err.Error(), "https") {
		t.Fatalf("error %q does not explain the HTTPS requirement", err)
	}
}

func TestPullModelUsesConfiguredEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/pull" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var request pullRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if request.Name != "test:1b" || !request.Stream {
			t.Errorf("unexpected pull request: %+v", request)
		}
		_, _ = w.Write([]byte("{\"status\":\"pulling manifest\"}\n{\"status\":\"success\"}\n"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test:1b")
	if err := client.PullModel(context.Background(), "test:1b"); err != nil {
		t.Fatalf("PullModel: %v", err)
	}
}

func TestPullModelReportsStreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{\"error\":\"model not found\"}\n"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test:1b")
	if err := client.PullModel(context.Background(), "missing:1b"); err == nil {
		t.Fatal("expected an error from the pull stream")
	}
}

func TestPullModelReportsProgress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte("{\"status\":\"pulling manifest\"}\n"))
		_, _ = w.Write([]byte("{\"status\":\"downloading\",\"digest\":\"sha256:abc\",\"total\":100,\"completed\":50}\n"))
		_, _ = w.Write([]byte("{\"status\":\"downloading\",\"digest\":\"sha256:abc\",\"total\":100,\"completed\":100}\n"))
		_, _ = w.Write([]byte("{\"status\":\"success\"}\n"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test:1b")
	var progress []PullProgress
	if err := client.PullModelWithProgress(context.Background(), "test:1b", func(event PullProgress) error {
		progress = append(progress, event)
		return nil
	}); err != nil {
		t.Fatalf("PullModelWithProgress: %v", err)
	}
	if len(progress) != 4 {
		t.Fatalf("progress events = %d, want 4: %+v", len(progress), progress)
	}
	if progress[1].Percent != 50 || progress[1].Completed != 50 || progress[1].Total != 100 {
		t.Fatalf("unexpected half-way progress: %+v", progress[1])
	}
	if progress[2].Percent != 100 {
		t.Fatalf("completed progress = %+v, want 100%%", progress[2])
	}
	if progress[3].Status != "success" {
		t.Fatalf("last status = %q, want success", progress[3].Status)
	}
}

func TestPullModelProgressCallbackErrorStopsDownload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{\"status\":\"downloading\",\"total\":100,\"completed\":10}\n"))
		_, _ = w.Write([]byte("{\"status\":\"success\"}\n"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test:1b")
	err := client.PullModelWithProgress(context.Background(), "test:1b", func(PullProgress) error {
		return context.Canceled
	})
	if err == nil {
		t.Fatal("expected the callback error to stop the pull")
	}
}

func TestChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var request chatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if request.Stream {
			t.Error("stream must be disabled")
		}
		if request.Model != "test-model" {
			t.Errorf("model = %q", request.Model)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"hello"},"done":true}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-model")
	reply, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if reply != "hello" {
		t.Fatalf("reply = %q", reply)
	}
}

func TestChatReportsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not found", http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.URL, "missing")
	if _, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}); err == nil {
		t.Fatal("expected an error")
	}
}

func TestChatStreamSendsChunks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var request chatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if !request.Stream {
			t.Error("stream must be enabled")
		}
		if request.Model != "test-model" {
			t.Errorf("model = %q", request.Model)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte("{\"message\":{\"role\":\"assistant\",\"content\":\"Hel\"},\"done\":false}\n"))
		_, _ = w.Write([]byte("{\"message\":{\"role\":\"assistant\",\"content\":\"lo\"},\"done\":false}\n"))
		_, _ = w.Write([]byte("{\"message\":{\"role\":\"assistant\",\"content\":\"\"},\"done\":true}\n"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-model")
	var deltas []string
	reply, err := client.ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(delta string) error {
		deltas = append(deltas, delta)
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if reply != "Hello" {
		t.Fatalf("reply = %q", reply)
	}
	if want := []string{"Hel", "lo"}; !reflect.DeepEqual(deltas, want) {
		t.Fatalf("deltas = %v, want %v", deltas, want)
	}
}

func TestChatStreamRequiresDone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte("{\"message\":{\"role\":\"assistant\",\"content\":\"partial\"},\"done\":false}\n"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-model")
	if _, err := client.ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil); err == nil {
		t.Fatal("expected an error for an incomplete stream")
	}
}

func TestChatStreamReportsErrorChunk(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte("{\"error\":\"model not found\"}\n"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "missing")
	if _, err := client.ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil); err == nil {
		t.Fatal("expected an error")
	}
}

func TestListModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"llama3.2:8b"},{"name":""},{"name":"llava:7b"}]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-model")
	models, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	want := []string{"llama3.2:8b", "llava:7b"}
	if !reflect.DeepEqual(models, want) {
		t.Fatalf("models = %v, want %v", models, want)
	}
}

func TestHealthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-model")
	if err := client.Healthy(context.Background()); err != nil {
		t.Fatalf("Healthy: %v", err)
	}
}
