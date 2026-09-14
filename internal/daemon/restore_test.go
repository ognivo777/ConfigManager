package daemon

import (
	"context"
	"os"
	"testing"
)

// commitChange makes a content change and commits it through the batch
// mechanism directly (bypassing watchers for reliability). It returns the new
// commit id.
func commitChange(t *testing.T, d *Daemon, src, content, msg string) string {
	t.Helper()
	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	d.reconcileAll()
	d.mu.Unlock()
	pending := d.collectPending()
	if len(pending) == 0 {
		t.Fatal("no pending after change")
	}
	if err := d.commit(pending); err != nil {
		t.Fatalf("commit: %v", err)
	}
	d.clearPending()
	logs, err := d.repo.History(context.Background(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	return logs[0].ID
}

// newTestDaemon2 returns a daemon with one registered file and no watcher.
func newTestDaemon2(t *testing.T) (*Daemon, string) {
	t.Helper()
	d, src := newTestDaemon(t)
	if err := d.Add(AddRequest{AbsPath: src}); err != nil {
		t.Fatal(err)
	}
	return d, src
}

func readSource(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRestoreProducesForwardCommit(t *testing.T) {
	d, src := newTestDaemon2(t)
	first := commitChange(t, d, src, "v2\n", "change1")
	_ = first
	commitChange(t, d, src, "v3\n", "change2")

	// preview should list the file
	prev, err := d.PreviewRestore(context.Background(), first)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(prev.Files) == 0 {
		t.Fatal("preview lists no files")
	}

	// restore "first" (v2) back
	res, err := d.Restore(context.Background(), RestoreRequest{Commit: first, All: true})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if len(res.Restored) != 1 {
		t.Fatalf("expected 1 restored, got %d", len(res.Restored))
	}
	if got := readSource(t, src); got != "v2\n" {
		t.Fatalf("restored content = %q, want v2", got)
	}

	// history must NOT have been rewritten: 3 (init, change1, change2) + 1 restore
	logs, _ := d.repo.History(context.Background(), "", 0)
	if len(logs) != 4 {
		t.Fatalf("expected 4 commits (append-only), got %d", len(logs))
	}
	if shortID7(logs[0].ID) != res.Commit {
		t.Fatalf("HEAD commit = %s, want restore commit %s", logs[0].ID, res.Commit)
	}
}

func shortID7(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

func TestRestoreDeletedFile(t *testing.T) {
	d, src := newTestDaemon2(t)

	// delete the file and commit
	if err := os.Remove(src); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	d.reconcileAll()
	d.mu.Unlock()
	pending := d.collectPending()
	if err := d.commit(pending); err != nil {
		t.Fatal(err)
	}
	d.clearPending()

	// restore from the initial commit (the "Add monitored file" commit) which
	// still has the snapshot, so the file should be recreated with v1.
	previewID := firstCommitID(t, d)
	res, err := d.Restore(context.Background(), RestoreRequest{Commit: previewID, All: true})
	if err != nil {
		t.Fatalf("restore deleted: %v", err)
	}
	if len(res.Restored) != 1 {
		t.Fatalf("expected 1 restored, got %d", len(res.Restored))
	}
	if got := readSource(t, src); got != "v1\n" {
		t.Fatalf("recreated content = %q, want v1", got)
	}
}

func firstCommitID(t *testing.T, d *Daemon) string {
	t.Helper()
	logs, err := d.repo.History(context.Background(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) == 0 {
		t.Fatal("no commits")
	}
	return logs[len(logs)-1].ID
}
