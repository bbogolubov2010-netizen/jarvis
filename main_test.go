package main

import (
	"os"
	"testing"
	"time"
)

// TestMain isolates the package from the real per-user config directory.
// NewApp reads and may rewrite settings, so letting tests share
// %AppData%\Jarvis would corrupt a developer's data and make results depend on
// whether JARVIS is installed. os.UserConfigDir honours APPDATA on Windows.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "jarvis-test-appdata-")
	if err != nil {
		os.Exit(m.Run())
	}
	if err := os.Setenv("APPDATA", dir); err != nil {
		os.RemoveAll(dir)
		os.Exit(m.Run())
	}

	code := m.Run()

	// Antivirus and the search indexer can keep freshly written files open for
	// a moment on Windows, so retry the cleanup instead of failing the run.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := os.RemoveAll(dir); err == nil || os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	os.Exit(code)
}
