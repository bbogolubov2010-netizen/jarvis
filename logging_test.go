package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestWriteLogFileRotatesAndBoundsBackups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "app.log")

	writeLogFile(path, "first", 1<<20, 2)
	writeLogFile(path, "second", 1, 2)
	writeLogFile(path, "third", 1, 2)

	if got := readLog(t, path); !strings.Contains(got, "third") {
		t.Fatalf("current log = %q, want third entry", got)
	}
	if got := readLog(t, path+".1"); !strings.Contains(got, "second") {
		t.Fatalf("first backup = %q, want second entry", got)
	}
	if got := readLog(t, path+".2"); !strings.Contains(got, "first") {
		t.Fatalf("second backup = %q, want first entry", got)
	}

	// A fourth rotation must drop the oldest backup instead of growing forever.
	writeLogFile(path, "fourth", 1, 2)
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Fatalf("unexpected backup beyond the configured count: %v", err)
	}
	if got := readLog(t, path+".1"); !strings.Contains(got, "third") {
		t.Fatalf("first backup after fourth = %q, want third entry", got)
	}
}
