package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListShowsMonitoredAndDeleted(t *testing.T) {
	d, src := newTestDaemon2(t)
	// second file in a sibling directory
	src2 := filepath.Join(t.TempDir(), "other.conf")
	if err := os.WriteFile(src2, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := d.Add(AddRequest{AbsPath: src2}); err != nil {
		t.Fatal(err)
	}

	entries := d.List()
	if len(entries) != 2 {
		t.Fatalf("expected 2 monitored, got %d", len(entries))
	}
	for _, e := range entries {
		if !e.Exists {
			t.Fatalf("expected %s to exist", e.AbsPath)
		}
		if e.State != "clean" {
			t.Fatalf("expected clean state for %s, got %s", e.AbsPath, e.State)
		}
	}

	// delete one file; List must still include it, flagged and non-existing
	if err := os.Remove(src); err != nil {
		t.Fatal(err)
	}
	entries = d.List()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries after delete, got %d", len(entries))
	}
	deleted := false
	for _, e := range entries {
		if filepath.Clean(e.AbsPath) == filepath.Clean(src) {
			if e.Exists {
				t.Fatalf("deleted file reported as existing")
			}
			deleted = true
		}
	}
	if !deleted {
		t.Fatal("deleted file not present in list")
	}
}

func TestListSorted(t *testing.T) {
	d, _ := newTestDaemon2(t)
	src2 := filepath.Join(t.TempDir(), "aaa.conf")
	_ = os.WriteFile(src2, []byte("x\n"), 0o644)
	_ = d.Add(AddRequest{AbsPath: src2})

	entries := d.List()
	for i := 1; i < len(entries); i++ {
		if entries[i-1].AbsPath > entries[i].AbsPath {
			t.Fatalf("list not sorted: %s > %s", entries[i-1].AbsPath, entries[i].AbsPath)
		}
	}
}
