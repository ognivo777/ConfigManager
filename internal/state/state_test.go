package state

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSetAndMessage(t *testing.T) {
	s := New("")
	if got := s.Message(); got != "" {
		t.Fatalf("initial message = %q, want empty", got)
	}
	if err := s.SetMessage("hello"); err != nil {
		t.Fatal(err)
	}
	if got := s.Message(); got != "hello" {
		t.Fatalf("message = %q", got)
	}
}

func TestTakeAndClear(t *testing.T) {
	s := New("")
	_ = s.SetMessage("pending")
	msg, err := s.TakeAndClear()
	if err != nil {
		t.Fatal(err)
	}
	if msg != "pending" {
		t.Fatalf("took %q", msg)
	}
	if s.Message() != "" {
		t.Fatal("message not cleared")
	}
}

func TestPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "msg")
	s := New(path)
	if err := s.SetMessage("persisted"); err != nil {
		t.Fatal(err)
	}
	// reload
	s2 := New(path)
	if got := s2.Message(); got != "persisted" {
		t.Fatalf("reloaded message = %q", got)
	}
}

func TestTrimsWhitespace(t *testing.T) {
	s := New("")
	_ = s.SetMessage("  hi  ")
	if got := strings.TrimSpace(s.Message()); got != "hi" {
		t.Fatalf("message = %q", got)
	}
}
