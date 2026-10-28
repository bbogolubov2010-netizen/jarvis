package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"Jarvis/internal/commands"
	"Jarvis/internal/config"
	"Jarvis/internal/intent"
	"Jarvis/internal/ollama"
	"Jarvis/internal/voice"
)

func TestCommandArgs(t *testing.T) {
	lower := "/smart how do black holes work"
	original := "/smart how do black holes work"

	arg, matched := commandArgs(lower, original, "/smart")
	if !matched || arg != "how do black holes work" {
		t.Fatalf("unexpected match: arg=%q matched=%v", arg, matched)
	}

	if _, matched := commandArgs("smart search", "smart search", "/smart"); matched {
		t.Fatal("bare words must not be treated as commands")
	}

	arg, matched = commandArgs("/screen", "/screen", "/screen")
	if !matched || arg != "" {
		t.Fatalf("unexpected match: arg=%q matched=%v", arg, matched)
	}
}

func TestVoiceForCommand(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		result string
		want   voice.Key
	}{
		{name: "system", input: "/system", result: "CPU: ok", want: voice.KeyCheckDone},
		{name: "list", input: "/ls", result: "file.txt", want: voice.KeyCheckDone},
		{name: "no notes", input: "/notes", result: "No notes saved.", want: voice.KeyNoInfo},
		{name: "save note", input: "/note buy milk", result: "Note saved.", want: voice.KeyCreated},
		{name: "volume", input: "/volume 50", result: "Volume set to 50.", want: voice.KeyAsYouWish},
		{name: "volume failed", input: "/volume mute", result: "Mute failed: no device", want: voice.KeyError},
		{name: "access denied", input: "/mkdir x", result: "Access denied: outside allowed folders", want: voice.KeyError},
		{name: "help", input: "/help", result: "Available commands:", want: voice.KeyAtYourService},
		{name: "error", input: "/unknown", result: "Error: nope", want: voice.KeyError},
		{name: "image failed", input: "/imagine cat", result: "Image generation failed: timeout", want: voice.KeyImageFail},
		{name: "image saved", input: "/imagine cat", result: commands.ResultImageSavedPrefix + "cat.png:512x512", want: voice.KeyImageDone},
		{name: "fallback", input: "/unknown", result: "Done.", want: voice.KeyAck},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := voiceForCommand(test.input, test.result); got != test.want {
				t.Fatalf("voiceForCommand(%q, %q) = %q, want %q", test.input, test.result, got, test.want)
			}
		})
	}
}

func TestAppendHistoryKeepsSystemPromptAndLimit(t *testing.T) {
	app := NewApp(false)
	app.settings = config.DefaultSettings()
	app.historyPath = ""
	app.resetHistory()

	for index := 0; index < 100; index++ {
		app.appendHistory(ollama.Message{Role: "user", Content: fmt.Sprintf("message-%d", index)})
	}

	history := app.snapshotHistory()
	if len(history) != maxConversationMessages+1 {
		t.Fatalf("history length = %d, want %d", len(history), maxConversationMessages+1)
	}
	if history[0].Role != "system" {
		t.Fatalf("first message must be the system prompt, got %q", history[0].Role)
	}
	if history[len(history)-1].Content != "message-99" {
		t.Fatalf("last message = %q", history[len(history)-1].Content)
	}
	if conversationCount(history) != maxConversationMessages {
		t.Fatalf("conversation length = %d, want %d", conversationCount(history), maxConversationMessages)
	}
}

func TestProcessTextRoutesLocally(t *testing.T) {
	app := NewApp(false)
	app.settings = config.DefaultSettings()
	app.historyPath = ""

	if got := app.ProcessText("   "); got != "Yes, Sir?" {
		t.Fatalf("empty input returned %q", got)
	}

	if got := app.ProcessText("/help"); !strings.Contains(got, "Available commands") {
		t.Fatalf("/help returned %q", got)
	}

	if got := app.ProcessText("/screen"); !strings.Contains(got, "screenshot") {
		t.Fatalf("/screen returned %q", got)
	}
	if !app.intents.Has() {
		t.Fatal("/screen must arm a pending screenshot intent")
	}

	if got := app.ProcessText("/shutdown"); !strings.Contains(got, "shut down") {
		t.Fatalf("/shutdown returned %q", got)
	}
	if pending, ok := app.intents.TakeConfirmation("YES"); !ok || pending.Kind != intent.Shutdown {
		t.Fatalf("pending intent = %+v, ok=%v", pending, ok)
	}
}

func TestProcessTextCancellationClearsPendingIntent(t *testing.T) {
	app := NewApp(false)
	app.settings = config.DefaultSettings()
	app.historyPath = ""

	if got := app.ProcessText("/shutdown"); !strings.Contains(got, "Type YES") {
		t.Fatalf("/shutdown returned %q", got)
	}
	if got := app.ProcessText("NO"); got != "Cancelled, Sir." {
		t.Fatalf("cancellation returned %q", got)
	}
	if app.intents.Has() {
		t.Fatal("cancellation must clear the pending action")
	}
	if routed := app.routeInput("YES"); routed.handled {
		t.Fatal("a later YES must not execute a cancelled action")
	}
}

func TestChatStreamRoutesCommandsInline(t *testing.T) {
	app := NewApp(false)
	app.settings = config.DefaultSettings()
	app.historyPath = ""

	start, err := app.ChatStream("/help")
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if start.Streaming {
		t.Fatal("commands must not start a stream")
	}
	if start.ClearHistory {
		t.Fatal("only /clear should request a UI history reset")
	}
	if !strings.Contains(start.Content, "Available commands") {
		t.Fatalf("unexpected command response %q", start.Content)
	}
}

func TestChatStreamReleasesReservedSlotForCommands(t *testing.T) {
	app := NewApp(false)
	app.settings = config.DefaultSettings()
	app.historyPath = ""

	for i := 0; i < 2; i++ {
		start, err := app.ChatStream("/time")
		if err != nil {
			t.Fatalf("command %d: %v", i, err)
		}
		if start.Streaming || !strings.Contains(start.Content, "Current time:") {
			t.Fatalf("unexpected command response: %+v", start)
		}
	}
}

func TestChatStreamClearCommandReportsUIReset(t *testing.T) {
	app := NewApp(false)
	app.settings = config.DefaultSettings()
	app.historyPath = ""
	app.resetHistory()
	app.appendHistory(ollama.Message{Role: "user", Content: "old message"})

	start, err := app.ChatStream("/clear")
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if start.Streaming || !start.ClearHistory {
		t.Fatalf("unexpected clear response: %+v", start)
	}
	if history := app.snapshotHistory(); len(history) != 1 || history[0].Role != "system" {
		t.Fatalf("history was not cleared: %+v", history)
	}
}

func TestChatStreamEmitsDeltasAndStoresReply(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte("{\"message\":{\"role\":\"assistant\",\"content\":\"Hel\"},\"done\":false}\n"))
		_, _ = w.Write([]byte("{\"message\":{\"role\":\"assistant\",\"content\":\"lo\"},\"done\":true}\n"))
	}))
	defer server.Close()

	app := NewApp(false)
	app.settings = config.DefaultSettings()
	app.historyPath = ""
	app.resetHistory()
	app.ollama = ollama.NewClient(server.URL, "test-model")

	var deltas []string
	done := make(chan string, 1)
	app.emit = func(event string, payload any) {
		switch event {
		case eventChatDelta:
			deltas = append(deltas, payload.(chatDeltaEvent).Delta)
		case eventChatDone:
			done <- payload.(chatDoneEvent).Content
		case eventChatError:
			t.Errorf("unexpected error event: %+v", payload)
		}
	}

	start, err := app.ChatStream("hello")
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if !start.Streaming || start.RequestID == "" {
		t.Fatalf("unexpected stream start: %+v", start)
	}

	select {
	case content := <-done:
		if content != "Hello" {
			t.Fatalf("done content = %q", content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the stream")
	}

	if got := strings.Join(deltas, ""); got != "Hello" {
		t.Fatalf("deltas = %q", got)
	}
	history := app.snapshotHistory()
	if len(history) < 3 {
		t.Fatalf("history too short: %+v", history)
	}
	if history[len(history)-2].Role != "user" || history[len(history)-2].Content != "hello" {
		t.Fatalf("unexpected user history: %+v", history[len(history)-2])
	}
	if history[len(history)-1].Role != "assistant" || history[len(history)-1].Content != "Hello" {
		t.Fatalf("unexpected assistant history: %+v", history[len(history)-1])
	}
}

func TestCancelChatStreamEmitsCanceled(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte("{\"message\":{\"role\":\"assistant\",\"content\":\"partial\"},\"done\":false}\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()

	app := NewApp(false)
	app.settings = config.DefaultSettings()
	app.historyPath = ""
	app.resetHistory()
	app.ollama = ollama.NewClient(server.URL, "test-model")

	canceled := make(chan struct{}, 1)
	app.emit = func(event string, payload any) {
		switch event {
		case eventChatCanceled:
			canceled <- struct{}{}
		case eventChatError:
			t.Errorf("unexpected error event: %+v", payload)
		}
	}

	start, err := app.ChatStream("hello")
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the first chunk")
	}

	app.CancelChatStream(start.RequestID)

	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the canceled event")
	}

	history := app.snapshotHistory()
	if len(history) != 1 || history[0].Role != "system" {
		t.Fatalf("canceled turn must be rolled back: %+v", history)
	}
}

func TestChatStreamErrorRollsBackUserMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model unavailable", http.StatusInternalServerError)
	}))
	defer server.Close()

	app := NewApp(false)
	app.settings = config.DefaultSettings()
	app.historyPath = ""
	app.resetHistory()
	app.ollama = ollama.NewClient(server.URL, "test-model")

	failed := make(chan struct{}, 1)
	app.emit = func(event string, payload any) {
		switch event {
		case eventChatError:
			failed <- struct{}{}
		case eventChatDone, eventChatCanceled:
			t.Errorf("unexpected terminal event: %s %+v", event, payload)
		}
	}

	if _, err := app.ChatStream("hello"); err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	select {
	case <-failed:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the error event")
	}

	history := app.snapshotHistory()
	if len(history) != 1 || history[0].Role != "system" {
		t.Fatalf("failed turn must be rolled back: %+v", history)
	}
}

func TestChatStreamEmptyReplyRollsBackUserMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte("{\"done\":true}\n"))
	}))
	defer server.Close()

	app := NewApp(false)
	app.settings = config.DefaultSettings()
	app.historyPath = ""
	app.resetHistory()
	app.ollama = ollama.NewClient(server.URL, "test-model")

	failed := make(chan struct{}, 1)
	app.emit = func(event string, payload any) {
		switch event {
		case eventChatError:
			failed <- struct{}{}
		case eventChatDone, eventChatCanceled:
			t.Errorf("unexpected terminal event: %s %+v", event, payload)
		}
	}

	if _, err := app.ChatStream("hello"); err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	select {
	case <-failed:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the error event")
	}

	history := app.snapshotHistory()
	if len(history) != 1 || history[0].Role != "system" {
		t.Fatalf("empty turn must be rolled back: %+v", history)
	}
}

func TestSearchCommandsRequireConfirmation(t *testing.T) {
	app := NewApp(false)
	app.settings = config.DefaultSettings()
	app.historyPath = ""
	app.settings.WebSearch = true

	if got := app.ProcessText("/search golang concurrency"); !strings.Contains(got, "Type YES to confirm") {
		t.Fatalf("/search must ask for confirmation, got %q", got)
	}
	if pending, ok := app.intents.TakeConfirmation("YES"); !ok || pending.Kind != intent.WebSearch {
		t.Fatalf("pending intent = %+v, ok=%v", pending, ok)
	}

	if got := app.ProcessText("/smart golang concurrency"); !strings.Contains(got, "Type YES to confirm") {
		t.Fatalf("/smart must ask for confirmation, got %q", got)
	}
	if pending, ok := app.intents.TakeConfirmation("YES"); !ok || pending.Kind != intent.SmartSearch {
		t.Fatalf("pending intent = %+v, ok=%v", pending, ok)
	}
}

func TestSmartSearchRespectsWebSearchSetting(t *testing.T) {
	app := NewApp(false)
	app.settings.WebSearch = false

	got := app.ProcessText("/smart latest news")
	if !strings.Contains(got, "disabled") {
		t.Fatalf("expected a disabled message, got %q", got)
	}
	if app.intents.Has() {
		t.Fatal("no intent should be armed when web search is disabled")
	}
}

func TestOpenExternalRejectsUnsafeSchemes(t *testing.T) {
	app := NewApp(false)
	app.ctx = context.Background()

	for _, url := range []string{"file:///C:/Windows/System32", "javascript:alert(1)", "not-a-url"} {
		if err := app.OpenExternal(url); err == nil {
			t.Fatalf("OpenExternal(%q) should fail", url)
		}
	}
}

func TestTruncateMessageKeepsUTF8(t *testing.T) {
	got := truncateMessage("привет мир", 6)
	if !strings.HasPrefix(got, "привет") {
		t.Fatalf("unexpected truncation: %q", got)
	}
}
