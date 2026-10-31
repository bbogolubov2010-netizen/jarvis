package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFirstExistingIgnoresRelativePaths(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(workingDir, executable)
	if err != nil || filepath.IsAbs(relative) {
		t.Skip("cannot construct a relative path to the test executable")
	}
	if got := firstExisting([]string{relative}); got != "" {
		t.Fatalf("firstExisting accepted a relative path: %q", got)
	}
}

func TestNoDuplicateCommandTriggers(t *testing.T) {
	manager := NewManager()
	seen := make(map[string]string)

	check := func(trigger, owner string) {
		if previous, exists := seen[trigger]; exists {
			t.Fatalf("trigger %q is registered by both %q and %q", trigger, previous, owner)
		}
		seen[trigger] = owner
	}

	for _, command := range manager.commands {
		check("/"+command.Name, command.Name)
		for _, alias := range command.Aliases {
			check("/"+alias, command.Name)
		}
	}
}

func TestDangerousCommandsReturnSentinels(t *testing.T) {
	manager := NewManager()

	cases := map[string]string{
		"/screenshot": ResultScreenshotConfirm,
		"/screen":     ResultScreenshotConfirm,
		"/shot":       ResultScreenshotConfirm,
		"/shutdown":   ResultShutdownConfirm,
		"/restart":    ResultRestartConfirm,
		"/clear":      ResultClearChat,
	}
	for input, want := range cases {
		got, handled := manager.Process(input)
		if !handled {
			t.Fatalf("%s was not handled", input)
		}
		if got != want {
			t.Fatalf("%s returned %q, want %q", input, got, want)
		}
	}
}

func TestCommandAliasesRouteToDistinctHandlers(t *testing.T) {
	manager := NewManager()

	if _, handled := manager.Process("/notes"); !handled {
		t.Fatal("/notes is not routed")
	}
	if got, handled := manager.Process("/list"); !handled || !strings.Contains(got, "Contents of") {
		t.Fatalf("/list should list files, got %q handled=%v", got, handled)
	}
	if got, handled := manager.Process("/help"); !handled || !strings.Contains(got, "restart") {
		t.Fatalf("/help returned %q", got)
	}
}

func TestWriteFileSupportsQuotedPathsWithSpaces(t *testing.T) {
	path := filepath.Join(os.TempDir(), "jarvis_images", "quoted path test.txt")
	_ = os.Remove(path)
	t.Cleanup(func() { _ = os.Remove(path) })

	result := writeFile(`"` + path + `" hello world`)
	if !strings.Contains(result, "File written") {
		t.Fatalf("write failed: %s", result)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello world" {
		t.Fatalf("unexpected content: %q", string(data))
	}
}

func TestListDirCapsLargeDirectories(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "jarvis_images", "list-cap-test")
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	for i := 0; i < maxListEntries+5; i++ {
		path := filepath.Join(dir, fmt.Sprintf("entry-%03d.txt", i))
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result := listDir(dir)
	if !strings.Contains(result, "and 5 more entries") {
		t.Fatalf("large directory listing was not capped: %s", result)
	}
}

func TestTouchRefusesExistingFile(t *testing.T) {
	path := filepath.Join(os.TempDir(), "jarvis_images", "touch test.txt")
	_ = os.Remove(path)
	t.Cleanup(func() { _ = os.Remove(path) })

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	if result := touchFile(path); !strings.Contains(result, "already exists") {
		t.Fatalf("touch should refuse an existing file: %s", result)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "keep me" {
		t.Fatalf("touch changed the file: %q", string(data))
	}
}

func TestWriteFileRefusesOverwrite(t *testing.T) {
	path := filepath.Join(os.TempDir(), "jarvis_images", "write test.txt")
	_ = os.Remove(path)
	t.Cleanup(func() { _ = os.Remove(path) })

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	if result := writeFile(`"` + path + `" replacement`); !strings.Contains(result, "already exists") {
		t.Fatalf("write should refuse an existing file: %s", result)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "keep me" {
		t.Fatalf("write overwrote the file: %q", string(data))
	}
}

func TestReadFileLimitsLargeFiles(t *testing.T) {
	path := filepath.Join(os.TempDir(), "jarvis_images", "large read test.txt")
	_ = os.Remove(path)
	t.Cleanup(func() { _ = os.Remove(path) })

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("a", maxReadBytes+100)), 0o644); err != nil {
		t.Fatal(err)
	}

	result := readFile(path)
	if !strings.Contains(result, "... (truncated)") {
		t.Fatalf("large file should be truncated: %s", result)
	}
}

func TestListDirDoesNotCreateMissingDirectory(t *testing.T) {
	path := filepath.Join(os.TempDir(), "jarvis_images", "missing list test")
	_ = os.RemoveAll(path)
	t.Cleanup(func() { _ = os.RemoveAll(path) })

	result := listDir(path)
	if !strings.Contains(result, "Error reading directory") {
		t.Fatalf("missing directory should be reported: %s", result)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("listDir created a missing directory: %v", err)
	}
}

func TestNotesDoNotOverwriteCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.json")
	original := []byte("{not valid json")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := InitNotes(path); err == nil {
		t.Fatal("InitNotes accepted a corrupt file")
	}
	t.Cleanup(func() { _ = InitNotes("") })

	if result := noteAdd("new note"); !strings.Contains(result, "Could not save the note") {
		t.Fatalf("noteAdd should refuse to overwrite a corrupt file: %s", result)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(original) {
		t.Fatalf("corrupt notes file was overwritten: %q", data)
	}
}

func TestNotesPersistToDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.json")
	if err := InitNotes(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = InitNotes("") })

	if result := noteAdd("remember the milk"); !strings.Contains(result, "Note saved") {
		t.Fatalf("noteAdd failed: %s", result)
	}

	store.mu.Lock()
	store.notes = nil
	store.mu.Unlock()

	if err := InitNotes(path); err != nil {
		t.Fatal(err)
	}
	if got := noteList(""); !strings.Contains(got, "remember the milk") {
		t.Fatalf("note was not persisted: %q", got)
	}
}

func TestTruncateRunesKeepsUTF8(t *testing.T) {
	got := truncateRunes("привет мир", 6)
	if !strings.HasPrefix(got, "привет") {
		t.Fatalf("unexpected truncation: %q", got)
	}
}
