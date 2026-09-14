package repository

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func newTestRepo(t *testing.T) *Repository {
	t.Helper()
	dir := t.TempDir()
	filesDir := filepath.Join(dir, "files")
	r := New(filesDir)
	if err := r.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	// ensure git identity so commits succeed headlessly
	setIdentity(t, r)
	return r
}

func setIdentity(t *testing.T, r *Repository) {
	t.Helper()
	_, err := r.run(context.Background(), nil, "config", "user.name", "cm-test")
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.run(context.Background(), nil, "config", "user.email", "cm@test")
	if err != nil {
		t.Fatal(err)
	}
}

func TestInitAndCommit(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	if err := r.WriteSnapshot("etc/app.conf", []byte("a\n")); err != nil {
		t.Fatal(err)
	}
	if err := r.Stage(ctx, []string{"etc/app.conf"}); err != nil {
		t.Fatal(err)
	}
	id, err := r.Commit(ctx, "Add monitored file: /etc/app.conf")
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("empty commit id")
	}
	// snapshot file should exist
	if _, err := os.Stat(r.FullPath("etc/app.conf")); err != nil {
		t.Fatal(err)
	}
}

func TestHistory(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	_ = r.WriteSnapshot("a.txt", []byte("1\n"))
	_ = r.Stage(ctx, []string{"a.txt"})
	id1, _ := r.Commit(ctx, "add a")
	_ = r.WriteSnapshot("a.txt", []byte("2\n"))
	_ = r.Stage(ctx, []string{"a.txt"})
	id2, _ := r.Commit(ctx, "change a")

	logs, err := r.History(ctx, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(logs))
	}
	if !hasPrefix(logs[0].ID, id2) || !hasPrefix(logs[1].ID, id1) {
		t.Fatalf("order wrong: %s then %s, want %s then %s", logs[0].ID, logs[1].ID, id2, id1)
	}

	// file-filtered history (rel path)
	logs, err = r.History(ctx, "a.txt", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 {
		t.Fatalf("expected 2 for file, got %d", len(logs))
	}

	// limit
	logs, err = r.History(ctx, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 with limit, got %d", len(logs))
	}
}

func TestDiffStatsAndBody(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	_ = r.WriteSnapshot("a.txt", []byte("one\ntwo\n"))
	_ = r.Stage(ctx, []string{"a.txt"})
	id1, _ := r.Commit(ctx, "add")
	_ = r.WriteSnapshot("a.txt", []byte("one\nTHREE\n"))
	_ = r.Stage(ctx, []string{"a.txt"})
	id2, _ := r.Commit(ctx, "change")

	stats, err := r.DiffStats(ctx, id2, "")
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := stats["a.txt"]; !ok || v != "+1/-1" {
		t.Fatalf("stats = %v", stats)
	}
	body, err := r.DiffBody(ctx, id2, "")
	if err != nil {
		t.Fatal(err)
	}
	if !contains(body, "THREE") {
		t.Fatalf("diff body missing change: %q", body)
	}
	_ = id1
}

func TestFileStatementExistsAndMissing(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	_ = r.WriteSnapshot("x/y.txt", []byte("c\n"))
	_ = r.Stage(ctx, []string{"x/y.txt"})
	id, _ := r.Commit(ctx, "add")

	content, exists, err := r.FileStatement(ctx, id, "x/y.txt")
	if err != nil || !exists {
		t.Fatalf("statement: exists=%v err=%v", exists, err)
	}
	if string(content) != "c\n" {
		t.Fatalf("content=%q", content)
	}

	_, exists, err = r.FileStatement(ctx, id, "nope/nope.txt")
	if err != nil || exists {
		t.Fatalf("missing: exists=%v err=%v", exists, err)
	}
}

func TestRemoveSnapshot(t *testing.T) {
	r := newTestRepo(t)
	_ = r.WriteSnapshot("d.txt", []byte("x\n"))
	if !r.SnapshotExists("d.txt") {
		t.Fatal("snapshot should exist")
	}
	if err := r.RemoveSnapshot("d.txt"); err != nil {
		t.Fatal(err)
	}
	if r.SnapshotExists("d.txt") {
		t.Fatal("snapshot should be removed")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
