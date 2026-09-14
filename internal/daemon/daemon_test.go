package daemon

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/example/cm/internal/model"
)

// newTestDaemon creates a daemon rooted in a temp dir with a real monitored
// source file. Returns the daemon, the source file path, and a cleanup func.
func newTestDaemon(t *testing.T) (*Daemon, string) {
	t.Helper()
	root := t.TempDir()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// monitored source file lives outside root
	srcDir := filepath.Join(root, "monitored")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(srcDir, "app.conf")
	if err := os.WriteFile(src, []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	d, err := New(root, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d, src
}

func TestAddCreatesInitialCommit(t *testing.T) {
	d, src := newTestDaemon(t)
	if err := d.Add(AddRequest{AbsPath: src}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if !d.links.IsRegistered(src) {
		t.Fatal("registry symlink not created")
	}
	logs, err := d.repo.History(context.Background(), "", 0)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(logs))
	}
	if want := "Add monitored file: " + src; logs[0].Subject != want {
		t.Fatalf("initial commit subject = %q, want %q", logs[0].Subject, want)
	}
}

func TestReconcileDisabledDetectsChange(t *testing.T) {
	d, src := newTestDaemon(t)
	if err := d.Add(AddRequest{AbsPath: src}); err != nil {
		t.Fatal(err)
	}

	// modify source while "stopped" — the daemon is not running its watcher,
	// so we simulate the startup scan.
	if err := os.WriteFile(src, []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	d.mu.Lock()
	mp := d.monitored[src]
	d.mu.Unlock()
	if mp == nil {
		t.Fatal("monitored path absent")
	}

	// simulate reconcileAll at startup
	d.mu.Lock()
	d.reconcileAll()
	d.mu.Unlock()

	if mp.Pending != model.Changed {
		t.Fatalf("pending = %v, want Changed", mp.Pending)
	}
	if mp.SaveCount != 1 {
		t.Fatalf("save count = %d, want 1", mp.SaveCount)
	}
}

func TestCommitBatchAndMessage(t *testing.T) {
	d, src := newTestDaemon(t)
	if err := d.Add(AddRequest{AbsPath: src}); err != nil {
		t.Fatal(err)
	}
	if err := d.SetMessage("deployed v2"); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(src, []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	d.reconcileAll()
	d.mu.Unlock()
	pending := d.collectPending()
	if len(pending) == 0 {
		t.Fatal("no pending changes")
	}
	if err := d.commit(pending); err != nil {
		t.Fatalf("commit: %v", err)
	}

	logs, _ := d.repo.History(context.Background(), "", 0)
	if len(logs) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(logs))
	}
	full, _ := d.repo.CommitMessage(context.Background(), logs[0].ID)
	if !contains(full, "app.conf: 1 saves") {
		t.Fatalf("commit body missing generated section: %q", full)
	}
	if !contains(full, "deployed v2") {
		t.Fatalf("commit body missing user message: %q", full)
	}
}

func TestDuplicateAddRejected(t *testing.T) {
	d, src := newTestDaemon(t)
	if err := d.Add(AddRequest{AbsPath: src}); err != nil {
		t.Fatal(err)
	}
	if err := d.Add(AddRequest{AbsPath: src}); err == nil {
		t.Fatal("expected duplicate add error")
	}
}

func TestAddDirectoryRejectedByCallerValidation(t *testing.T) {
	d, _ := newTestDaemon(t)
	dir := d.root
	if err := d.Add(AddRequest{AbsPath: dir}); err == nil {
		t.Fatal("expected error adding a directory")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
