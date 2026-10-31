// Package intent stores a single pending action that requires explicit
// confirmation. Actions are never inferred from human-readable output text.
package intent

import (
	"sync"
	"time"
)

// Kind identifies a pending action.
type Kind string

const (
	Shutdown    Kind = "shutdown"
	Restart     Kind = "restart"
	Screenshot  Kind = "screenshot"
	SmartSearch Kind = "smart_search"
	WebSearch   Kind = "web_search"
	OpenApp     Kind = "open_app"
)

// Pending describes an action waiting for confirmation.
type Pending struct {
	Kind Kind
	Arg  string
}

type entry struct {
	pending Pending
	expires time.Time
}

// Store keeps at most one pending action with a time-to-live.
type Store struct {
	mu      sync.Mutex
	ttl     time.Duration
	now     func() time.Time
	pending *entry
}

// NewStore creates a store whose pending action expires after ttl.
func NewStore(ttl time.Duration) *Store {
	if ttl <= 0 {
		ttl = time.Minute
	}
	return &Store{ttl: ttl, now: time.Now}
}

// Set replaces any previous pending action.
func (s *Store) Set(kind Kind, arg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = &entry{
		pending: Pending{Kind: kind, Arg: arg},
		expires: s.now().Add(s.ttl),
	}
}

// Clear drops the pending action.
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = nil
}

// Has reports whether a non-expired action is pending.
func (s *Store) Has() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.validLocked()
}

// TakeConfirmation returns the pending action if input is an explicit
// confirmation. The action is consumed exactly once.
func (s *Store) TakeConfirmation(input string) (Pending, bool) {
	if !IsConfirmation(input) {
		return Pending{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.validLocked() {
		s.pending = nil
		return Pending{}, false
	}
	p := s.pending.pending
	s.pending = nil
	return p, true
}

func (s *Store) validLocked() bool {
	if s.pending == nil {
		return false
	}
	if s.now().After(s.pending.expires) {
		s.pending = nil
		return false
	}
	return true
}

// IsConfirmation reports whether input is an accepted confirmation word.
func IsConfirmation(input string) bool {
	normalized := NormalizeRussian(input)
	switch normalized {
	case "yes", "y", "да", "да сэр", "да сэр пожалуйста", "подтверждаю",
		"подтверждаю сэр", "конечно", "конечно сэр", "выполняй", "выполняй сэр",
		"давай", "ага", "так точно", "разрешаю":
		return true
	default:
		return false
	}
}

// IsCancellation reports whether input explicitly declines a pending action.
func IsCancellation(input string) bool {
	normalized := NormalizeRussian(input)
	switch normalized {
	case "no", "n", "cancel", "нет", "нет сэр", "отмена", "отмени",
		"не надо", "не нужно", "отставить", "не подтверждаю", "забудь":
		return true
	default:
		return false
	}
}
