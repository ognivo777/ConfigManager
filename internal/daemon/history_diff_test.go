package daemon

import (
	"context"
	"testing"
)

func TestHistoryRows(t *testing.T) {
	d, src := newTestDaemon2(t)
	commitChange(t, d, src, "v2\n", "change1")

	entries, err := d.History(context.Background(), HistoryOptions{Limit: 0})
	if err != nil {
		t.Fatal(err)
	}
	// init commit + change commit, each touches the file => at least 2 rows
	if len(entries) < 2 {
		t.Fatalf("expected >=2 rows, got %d", len(entries))
	}
	for _, e := range entries {
		if e.Commit == "" {
			t.Fatalf("history row missing commit id: %+v", e)
		}
	}
}

func TestDiffLatestAndFile(t *testing.T) {
	d, src := newTestDaemon2(t)
	commitChange(t, d, src, "v2 changed\n", "change1")

	diff, err := d.Diff(context.Background(), DiffOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if diff == "" {
		t.Fatal("expected non-empty diff")
	}

	diff, err = d.Diff(context.Background(), DiffOptions{File: src})
	if err != nil {
		t.Fatal(err)
	}
	if diff == "" {
		t.Fatal("expected non-empty file diff")
	}
}

func TestDiffSpecificCommit(t *testing.T) {
	d, src := newTestDaemon2(t)
	commitChange(t, d, src, "v2\n", "change1")
	id := commitChange(t, d, src, "v3\n", "change2")

	diff, err := d.Diff(context.Background(), DiffOptions{Commit: id})
	if err != nil {
		t.Fatal(err)
	}
	if diff == "" {
		t.Fatal("expected non-empty commit diff")
	}

	diff, err = d.Diff(context.Background(), DiffOptions{Commit: id, File: src})
	if err != nil {
		t.Fatal(err)
	}
	if diff == "" {
		t.Fatal("expected non-empty file+commit diff")
	}
}
