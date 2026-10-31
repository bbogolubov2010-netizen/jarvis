package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"Jarvis/internal/dpapi"
)

const (
	maxNotes     = 500
	maxNoteRunes = 4000
)

type notesStore struct {
	mu      sync.RWMutex
	notes   []string
	path    string
	loadErr error
}

var store = &notesStore{}

// RegisterNotesCommands registers note commands.
func RegisterNotesCommands(m *Manager) {
	m.Register(Command{
		Name:        "note",
		Aliases:     []string{"remember"},
		Description: "saves a note to disk",
		Handler:     noteAdd,
	})
	m.Register(Command{
		Name:        "notes",
		Aliases:     []string{"recall"},
		Description: "lists saved notes",
		Handler:     noteList,
	})
}

// InitNotes loads persisted notes from path.
func InitNotes(path string) error {
	store.mu.Lock()
	defer store.mu.Unlock()

	store.path = path
	store.notes = nil
	store.loadErr = nil

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		store.loadErr = fmt.Errorf("read notes: %w", err)
		return store.loadErr
	}
	var loaded []string
	if err := json.Unmarshal(data, &loaded); err != nil {
		decrypted, decryptErr := dpapi.Decrypt(data)
		if decryptErr != nil {
			store.loadErr = fmt.Errorf("parse notes: %w", err)
			return store.loadErr
		}
		if err := json.Unmarshal(decrypted, &loaded); err != nil {
			store.loadErr = fmt.Errorf("parse notes: %w", err)
			return store.loadErr
		}
	}
	store.notes = loaded
	return nil
}

func noteAdd(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "Usage: /note <text> - saves a note."
	}
	if len([]rune(text)) > maxNoteRunes {
		return fmt.Sprintf("Note is too long (maximum %d characters).", maxNoteRunes)
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	if len(store.notes) >= maxNotes {
		return fmt.Sprintf("Note limit reached (%d). Remove the notes file to start over.", maxNotes)
	}

	store.notes = append(store.notes, text)
	if err := store.saveLocked(); err != nil {
		store.notes = store.notes[:len(store.notes)-1]
		return fmt.Sprintf("Could not save the note: %s", err)
	}
	return fmt.Sprintf("Note saved. You have %d note(s).", len(store.notes))
}

func noteList(args string) string {
	store.mu.RLock()
	defer store.mu.RUnlock()

	if len(store.notes) == 0 {
		return "No notes saved."
	}
	var out strings.Builder
	out.WriteString("Your notes:\n")
	for i, note := range store.notes {
		out.WriteString(fmt.Sprintf("%d. %s\n", i+1, note))
	}
	return out.String()
}

func (s *notesStore) saveLocked() error {
	if s.path == "" {
		return nil
	}
	if s.loadErr != nil {
		// Do not overwrite a notes file we could not read; that would destroy
		// the user's existing notes.
		return s.loadErr
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.notes, "", "  ")
	if err != nil {
		return err
	}
	data, err = dpapi.Encrypt(data)
	if err != nil {
		return fmt.Errorf("encrypt notes: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(s.path), "."+filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(tempName)
		}
	}()

	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempName, s.path); err != nil {
		return err
	}
	renamed = true
	return nil
}
