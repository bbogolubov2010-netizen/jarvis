package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestGetLogsDoesNotPanicWithoutInit(t *testing.T) {
	Close()
	if logs := GetLogs(); logs == "" {
		t.Fatal("GetLogs returned an empty string")
	}
}

func TestLoggerKeepsWorkingAfterGetLogs(t *testing.T) {
	Close()
	if err := Init(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(Close)

	Log("TEST", "first entry")
	if logs := GetLogs(); !strings.Contains(logs, "first entry") {
		t.Fatalf("first entry missing from logs: %q", logs)
	}

	Log("TEST", "second entry")
	logs := GetLogs()
	if !strings.Contains(logs, "second entry") {
		t.Fatalf("logger stopped writing after GetLogs: %q", logs)
	}
}

func TestLogSanitizesNewlines(t *testing.T) {
	Close()
	if err := Init(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(Close)

	Log("TEST", "line one\nline two")
	logs := GetLogs()
	if strings.Contains(logs, "line one\nline two") {
		t.Fatalf("newlines were not sanitized: %q", logs)
	}
	if !strings.Contains(logs, "line one line two") {
		t.Fatalf("values were not written on one line: %q", logs)
	}
}

func TestSanitizeValueKeepsValidUTF8WhenTruncated(t *testing.T) {
	value := strings.Repeat("中", maxLogValueLength)
	got := sanitizeValue(value)
	if !utf8.ValidString(got) {
		t.Fatalf("sanitizeValue produced invalid UTF-8: %q", got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected truncation marker: %q", got)
	}
	if len(got) > maxLogValueLength+3 {
		t.Fatalf("truncated value is too long: %d bytes", len(got))
	}
}

func TestTailReadsEndOfLargeFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.log")
	var content strings.Builder
	for content.Len() < maxTailBytes+1024 {
		content.WriteString("old entry filler filler filler\n")
	}
	content.WriteString("latest entry\n")
	if err := os.WriteFile(path, []byte(content.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	lines, err := tail(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0] != "latest entry" {
		t.Fatalf("tail did not return the end of the file: %q", lines)
	}
}
