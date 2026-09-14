package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/example/cm/internal/model"
)

func newStartedDaemon(t *testing.T) (*Daemon, string) {
	t.Helper()
	d, src := newTestDaemon(t)
	d.batchWindow = 400 * time.Millisecond
	if err := d.Add(AddRequest{AbsPath: src}); err != nil {
		t.Fatal(err)
	}
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	return d, src
}

func waitCommits(t *testing.T, d *Daemon, want int) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		logs, _ := d.repo.History(context.Background(), "", 0)
		if len(logs) >= want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	logs, _ := d.repo.History(context.Background(), "", 0)
	t.Fatalf("timed out waiting for %d commits, have %d", want, len(logs))
}

func snapshotContent(t *testing.T, d *Daemon, src string) []byte {
	t.Helper()
	p := d.repo.FullPath(model.SnapshotRelPath(src))
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read snapshot %s: %v", p, err)
	}
	return b
}

func TestWatchDetectsWriteAndCommits(t *testing.T) {
	d, src := newStartedDaemon(t)
	if err := os.WriteFile(src, []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitCommits(t, d, 2)

	if got := string(snapshotContent(t, d, src)); got != "v2\n" {
		t.Fatalf("snapshot content = %q, want v2", got)
	}
	logs, _ := d.repo.History(context.Background(), "", 0)
	full, _ := d.repo.CommitMessage(context.Background(), logs[0].ID)
	if !contains(full, "app.conf") {
		t.Fatalf("batch commit missing file line: %q", full)
	}
}

func TestWatchDetectsAtomicReplace(t *testing.T) {
	d, src := newStartedDaemon(t)
	dir := filepath.Dir(src)
	tmp := filepath.Join(dir, "app.conf.tmp")
	if err := os.WriteFile(tmp, []byte("v3 replaced\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, src); err != nil {
		t.Fatal(err)
	}
	waitCommits(t, d, 2)

	if got := string(snapshotContent(t, d, src)); got != "v3 replaced\n" {
		t.Fatalf("snapshot content = %q, want replaced content", got)
	}
}

func TestWatchDetectsDeletion(t *testing.T) {
	d, src := newStartedDaemon(t)
	if err := os.Remove(src); err != nil {
		t.Fatal(err)
	}
	waitCommits(t, d, 2)

	logs, _ := d.repo.History(context.Background(), "", 0)
	full, _ := d.repo.CommitMessage(context.Background(), logs[0].ID)
	if !contains(full, "deleted") {
		t.Fatalf("deletion commit missing 'deleted': %q", full)
	}
	d.mu.Lock()
	mp := d.monitored[src]
	d.mu.Unlock()
	if mp == nil || mp.Exists {
		t.Fatalf("expected deleted state, got %+v", mp)
	}
}

func TestWatchDetectsRecreation(t *testing.T) {
	d, src := newStartedDaemon(t)
	if err := os.Remove(src); err != nil {
		t.Fatal(err)
	}
	waitCommits(t, d, 2)

	if err := os.WriteFile(src, []byte("recreated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// recreation may batch with the deletion or produce a new commit; wait for
	// pending state to be committed (snapshot reflects recreated content).
	waitSnapshot(t, d, src, "recreated\n")
}

// waitSnapshot waits until the committed snapshot content for src equals want,
// tolerating a transiently missing snapshot (e.g. during recreation).
func waitSnapshot(t *testing.T, d *Daemon, src, want string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		p := d.repo.FullPath(model.SnapshotRelPath(src))
		b, err := os.ReadFile(p)
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if string(b) == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	b, _ := os.ReadFile(d.repo.FullPath(model.SnapshotRelPath(src)))
	t.Fatalf("timed out waiting for snapshot content %q, got %q", want, string(b))
}
