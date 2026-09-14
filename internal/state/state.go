// Package state maintains pending runtime state for the daemon: save
// counters, pending file states, and the pending user commit message. It is
// distinct from the links/ monitored-file registry.
package state

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// State holds pending runtime information for one repository root.
type State struct {
	mu sync.Mutex

	// pendingMessage is the user text to attach to the next commit.
	pendingMessage string

	// statePath, when non-empty, is where pendingMessage is persisted so a
	// daemon restart does not silently lose it.
	statePath string
}

// New returns a State with the given optional persistence file path.
func New(statePath string) *State {
	s := &State{statePath: statePath}
	s.load()
	return s
}

func (s *State) load() {
	if s.statePath == "" {
		return
	}
	b, err := os.ReadFile(s.statePath)
	if err != nil {
		return
	}
	s.pendingMessage = strings.TrimSpace(string(b))
}

func (s *State) persist() error {
	if s.statePath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.statePath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.statePath, []byte(s.pendingMessage), 0o600)
}

// SetMessage sets the pending user message for the next commit.
func (s *State) SetMessage(text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingMessage = strings.TrimSpace(text)
	return s.persist()
}

// Message returns the pending user message (empty if none).
func (s *State) Message() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pendingMessage
}

// TakeAndClear atomically returns and clears the pending message.
func (s *State) TakeAndClear() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	msg := s.pendingMessage
	s.pendingMessage = ""
	if err := s.persist(); err != nil {
		return msg, fmt.Errorf("failed to clear pending message: %w", err)
	}
	return msg, nil
}
