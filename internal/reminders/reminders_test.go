package reminders

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreAddListDue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reminders.json")
	store, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	now := time.Now()
	first, err := store.Add("call mom", now.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("Add first: %v", err)
	}
	second, err := store.Add("stretch", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Add second: %v", err)
	}

	items := store.List()
	if len(items) != 2 {
		t.Fatalf("List length = %d, want 2", len(items))
	}
	if items[0].ID != second.ID || items[1].ID != first.ID {
		t.Fatalf("List is not sorted by due time: %#v", items)
	}

	due := store.Due(now.Add(2 * time.Minute))
	if len(due) != 1 || due[0].ID != second.ID {
		t.Fatalf("Due = %#v, want only %q", due, second.ID)
	}
	if remaining := store.List(); len(remaining) != 1 || remaining[0].ID != first.ID {
		t.Fatalf("Remaining = %#v, want only %q", remaining, first.ID)
	}
}

func TestStorePersistsAndRemoves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reminders.json")
	store, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	item, err := store.Add("tea", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	reloaded, err := New(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if items := reloaded.List(); len(items) != 1 || items[0].Text != "tea" {
		t.Fatalf("reloaded items = %#v", items)
	}
	if !reloaded.Remove(item.ID) {
		t.Fatal("Remove returned false")
	}
	if len(reloaded.List()) != 0 {
		t.Fatalf("List after remove = %#v", reloaded.List())
	}
}

func TestStoreRecoversFromCorruptFile(t *testing.T) {
	dir := t.TempDir()
	// Windows can briefly keep the renamed corrupt file open (antivirus,
	// indexer), which makes testing.TempDir cleanup fail intermittently.
	// Remove the directory ourselves with retries; TempDir then sees it gone.
	t.Cleanup(func() { removeAllWithRetry(t, dir) })
	path := filepath.Join(dir, "reminders.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	store, err := New(path)
	if err == nil {
		t.Fatal("New should report a parse error")
	}
	if len(store.List()) != 0 {
		t.Fatalf("corrupt store should be empty, got %#v", store.List())
	}
	matches, globErr := filepath.Glob(path + ".corrupt-*")
	if globErr != nil || len(matches) != 1 {
		t.Fatalf("expected one backup file, got %v (err %v)", matches, globErr)
	}
}

func removeAllWithRetry(t *testing.T, dir string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := os.RemoveAll(dir)
		if err == nil || os.IsNotExist(err) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("remove temp dir %s: %v", dir, err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
