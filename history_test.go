package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"Jarvis/internal/config"
	"Jarvis/internal/ollama"
)

func historyTempDir(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "jarvis-history-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// Windows antivirus/indexers can briefly keep fresh files open.
		for attempt := 0; attempt < 10; attempt++ {
			if err := os.RemoveAll(dir); err == nil {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		_ = os.RemoveAll(dir)
	})
	return dir
}

func conversationCount(messages []ollama.Message) int {
	count := 0
	for _, message := range messages {
		if message.Role == "user" || message.Role == "assistant" {
			count++
		}
	}
	return count
}

func newHistoryTestApp(t *testing.T) *App {
	t.Helper()

	app := NewApp(false)
	app.settings = config.DefaultSettings()
	app.historyPath = filepath.Join(historyTempDir(t), "history.json")
	app.resetHistory()
	return app
}

func readStoredHistory(t *testing.T, path string) []HistoryMessage {
	t.Helper()

	messages, _, err := readHistoryFile(path)
	if err != nil {
		t.Fatalf("decode history: %v", err)
	}
	return messages
}

func TestHistoryEncryptsLegacyPlaintextOnSave(t *testing.T) {
	app := newHistoryTestApp(t)
	legacy := []byte(`[{"role":"user","content":"old"},{"role":"assistant","content":"reply"}]`)
	if err := os.WriteFile(app.historyPath, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeHistoryFile(app.historyPath, legacy); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{app.historyPath, app.historyPath + ".bak"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if json.Valid(data) {
			t.Fatalf("%s still contains plaintext history", path)
		}
		messages, _, err := readHistoryFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if len(messages) != 2 || messages[0].Content != "old" {
			t.Fatalf("unexpected history in %s: %+v", path, messages)
		}
	}
}

func TestHistoryRoundTripAndBackup(t *testing.T) {
	app := newHistoryTestApp(t)
	app.settings.SystemPrompt = "system prompt v1"
	app.resetHistory()

	app.appendHistory(ollama.Message{Role: "user", Content: "hello"})
	app.appendHistory(ollama.Message{Role: "assistant", Content: "hi"})

	stored := readStoredHistory(t, app.historyPath)
	if len(stored) != 2 {
		t.Fatalf("stored messages = %d, want 2: %+v", len(stored), stored)
	}
	if stored[0].Role != "user" || stored[0].Content != "hello" {
		t.Fatalf("unexpected stored user message: %+v", stored[0])
	}
	if stored[1].Role != "assistant" || stored[1].Content != "hi" {
		t.Fatalf("unexpected stored assistant message: %+v", stored[1])
	}
	if _, err := os.Stat(app.historyPath + ".bak"); err != nil {
		t.Fatalf("backup file is missing: %v", err)
	}

	restored := NewApp(false)
	restored.settings = config.DefaultSettings()
	restored.settings.SystemPrompt = "system prompt v2"
	restored.historyPath = app.historyPath
	restored.loadHistory()

	history := restored.snapshotHistory()
	if len(history) != 3 {
		t.Fatalf("restored history length = %d, want 3: %+v", len(history), history)
	}
	if history[0].Role != "system" || history[0].Content != "system prompt v2" {
		t.Fatalf("system prompt was not restored from settings: %+v", history[0])
	}
	if history[1].Content != "hello" || history[2].Content != "hi" {
		t.Fatalf("unexpected restored history: %+v", history)
	}

	ui := restored.GetHistory()
	if len(ui) != 2 || ui[0].Role != "user" || ui[1].Role != "assistant" {
		t.Fatalf("unexpected UI history: %+v", ui)
	}
}

func TestHistoryFallsBackToBackupWithoutOverwritingIt(t *testing.T) {
	app := newHistoryTestApp(t)
	app.appendHistory(ollama.Message{Role: "user", Content: "first"})
	app.appendHistory(ollama.Message{Role: "user", Content: "second"})

	if err := os.WriteFile(app.historyPath, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	restored := NewApp(false)
	restored.settings = config.DefaultSettings()
	restored.historyPath = app.historyPath
	restored.loadHistory()

	ui := restored.GetHistory()
	if len(ui) != 1 || ui[0].Content != "first" {
		t.Fatalf("backup was not restored: %+v", ui)
	}

	// The repaired primary must keep the good backup intact.
	backup := readStoredHistory(t, app.historyPath+".bak")
	if len(backup) != 1 || backup[0].Content != "first" {
		t.Fatalf("backup was overwritten by corrupt primary: %+v", backup)
	}
	if primary := readStoredHistory(t, app.historyPath); len(primary) != 1 || primary[0].Content != "first" {
		t.Fatalf("primary was not repaired: %+v", primary)
	}
}

func TestHistoryStartsFreshWhenBothFilesAreInvalid(t *testing.T) {
	app := newHistoryTestApp(t)
	if err := os.MkdirAll(filepath.Dir(app.historyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(app.historyPath, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(app.historyPath+".bak", []byte("{also broken"), 0o600); err != nil {
		t.Fatal(err)
	}

	app.loadHistory()
	history := app.snapshotHistory()
	if len(history) != 1 || history[0].Role != "system" {
		t.Fatalf("invalid history was not discarded: %+v", history)
	}
	if app.historyError == "" {
		t.Fatal("corrupt history did not produce a visible recovery error")
	}
	backups, err := filepath.Glob(app.historyPath + ".corrupt-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) < 1 {
		t.Fatalf("corrupt history was not backed up: %v", backups)
	}
}

func TestHistoryTrimsLoadedConversationToLimit(t *testing.T) {
	app := newHistoryTestApp(t)
	messages := make([]HistoryMessage, 0, 60)
	for index := 0; index < 60; index++ {
		role := "user"
		if index%2 == 1 {
			role = "assistant"
		}
		messages = append(messages, HistoryMessage{
			Role:    role,
			Content: fmt.Sprintf("message-%d", index),
		})
	}

	data, err := json.Marshal(messages)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(app.historyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(app.historyPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	app.loadHistory()
	history := app.snapshotHistory()
	if len(history) != maxConversationMessages+1 {
		t.Fatalf("history length = %d, want %d", len(history), maxConversationMessages+1)
	}
	if history[0].Role != "system" {
		t.Fatalf("first message must be system: %+v", history[0])
	}
	if history[1].Content != "message-20" {
		t.Fatalf("first kept message = %q, want message-20", history[1].Content)
	}
	if history[len(history)-1].Content != "message-59" {
		t.Fatalf("last kept message = %q, want message-59", history[len(history)-1].Content)
	}

	if stored := readStoredHistory(t, app.historyPath); len(stored) != maxConversationMessages {
		t.Fatalf("file was not trimmed: %d", len(stored))
	}
}

func TestClearChatPersistsEmptyHistory(t *testing.T) {
	app := newHistoryTestApp(t)
	app.appendHistory(ollama.Message{Role: "user", Content: "old message"})

	start, err := app.ChatStream("/clear")
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if !start.ClearHistory {
		t.Fatalf("clear command did not report a UI reset: %+v", start)
	}
	if history := app.snapshotHistory(); len(history) != 1 || history[0].Role != "system" {
		t.Fatalf("in-memory history was not cleared: %+v", history)
	}
	if stored := readStoredHistory(t, app.historyPath); len(stored) != 0 {
		t.Fatalf("cleared history was not persisted: %+v", stored)
	}
	if backup := readStoredHistory(t, app.historyPath+".bak"); len(backup) != 0 {
		t.Fatalf("cleared history is still in the backup: %+v", backup)
	}
}

func TestHistoryRejectsInvalidRoles(t *testing.T) {
	app := newHistoryTestApp(t)
	if err := os.MkdirAll(filepath.Dir(app.historyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(app.historyPath, []byte(`[{"role":"system","content":"evil"}]`), 0o600); err != nil {
		t.Fatal(err)
	}

	app.loadHistory()
	history := app.snapshotHistory()
	if len(history) != 1 || history[0].Role != "system" || history[0].Content != app.settings.SystemPrompt {
		t.Fatalf("invalid role was accepted: %+v", history)
	}
}

func TestHistoryPathUsesJarvisConfigDirectory(t *testing.T) {
	path := defaultHistoryPath()
	if filepath.Base(path) != "history.json" {
		t.Fatalf("unexpected history filename: %s", path)
	}
	if filepath.Base(filepath.Dir(path)) != "Jarvis" {
		t.Fatalf("history is not inside the Jarvis config directory: %s", path)
	}
	if strings.Contains(path, string(filepath.Separator)+string(filepath.Separator)) {
		t.Fatalf("history path contains an empty segment: %s", path)
	}
}
