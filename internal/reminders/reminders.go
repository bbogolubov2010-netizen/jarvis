// Package reminders stores user reminders and timers on disk.
package reminders

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Reminder is one scheduled message.
type Reminder struct {
	ID      string    `json:"id"`
	Text    string    `json:"text"`
	Due     time.Time `json:"due"`
	Created time.Time `json:"created"`
}

// Store is a concurrency-safe reminder list persisted as JSON.
type Store struct {
	mu    sync.Mutex
	path  string
	items []Reminder
}

// New loads the store. A corrupt file is preserved as a sibling backup and an
// empty store is returned so reminders never prevent the assistant from
// starting.
func New(path string) (*Store, error) {
	store := &Store{path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return store, fmt.Errorf("read reminders: %w", err)
	}
	if err := json.Unmarshal(data, &store.items); err != nil {
		backup := path + ".corrupt-" + time.Now().Format("20060102-150405")
		_ = os.Rename(path, backup)
		return store, fmt.Errorf("parse reminders: %w", err)
	}
	store.sortLocked()
	return store, nil
}

// Add schedules text at due and returns the created reminder.
func (s *Store) Add(text string, due time.Time) (Reminder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item := Reminder{
		ID:      fmt.Sprintf("%d", time.Now().UnixNano()),
		Text:    text,
		Due:     due,
		Created: time.Now(),
	}
	s.items = append(s.items, item)
	s.sortLocked()
	if err := s.saveLocked(); err != nil {
		s.items = s.items[:len(s.items)-1]
		return Reminder{}, err
	}
	return item, nil
}

// List returns a copy of all reminders sorted by due time.
func (s *Store) List() []Reminder {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Reminder(nil), s.items...)
}

// Due returns reminders that are due at now and removes them from the store.
func (s *Store) Due(now time.Time) []Reminder {
	s.mu.Lock()
	defer s.mu.Unlock()

	var due []Reminder
	var remaining []Reminder
	for _, item := range s.items {
		if !item.Due.After(now) {
			due = append(due, item)
			continue
		}
		remaining = append(remaining, item)
	}
	if len(due) == 0 {
		return nil
	}
	s.items = remaining
	_ = s.saveLocked()
	return due
}

// Remove deletes one reminder by ID.
func (s *Store) Remove(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for index, item := range s.items {
		if item.ID == id {
			s.items = append(s.items[:index], s.items[index+1:]...)
			_ = s.saveLocked()
			return true
		}
	}
	return false
}

func (s *Store) sortLocked() {
	sort.SliceStable(s.items, func(i, j int) bool {
		return s.items[i].Due.Before(s.items[j].Due)
	})
}

func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(s.items, "", "  ")
	if err != nil {
		return fmt.Errorf("encode reminders: %w", err)
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create reminders directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, ".reminders.tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary reminders file: %w", err)
	}
	name := temp.Name()
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(name)
		}
	}()
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write reminders: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync reminders: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close reminders: %w", err)
	}
	if err := os.Rename(name, s.path); err != nil {
		return fmt.Errorf("replace reminders: %w", err)
	}
	renamed = true
	return nil
}
