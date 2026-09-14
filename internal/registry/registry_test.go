package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "links")
	return New(dir)
}

func TestAddAndList(t *testing.T) {
	r := newTestRegistry(t)
	target := filepath.Join("var", "lib", "app.conf")
	if err := r.Add(target); err != nil {
		t.Fatal(err)
	}
	if !r.IsRegistered(target) {
		t.Fatal("target not registered")
	}
	list, err := r.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0] != target {
		t.Fatalf("list = %v", list)
	}
}

func TestDuplicateDetection(t *testing.T) {
	r := newTestRegistry(t)
	base := t.TempDir()
	a := filepath.Join(base, "nginx", "a.conf")
	b := filepath.Join(base, "nginx", "b.conf")
	if err := r.Add(a); err != nil {
		t.Fatal(err)
	}
	if err := r.Add(b); err != nil {
		t.Fatal(err)
	}
	if err := r.InternallyChecks(); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

func TestRegistryValidates(t *testing.T) {
	r := newTestRegistry(t)
	_ = os.MkdirAll(r.Dir, 0o755)
	// non-symlink regular file entry (in file mode this is a marker, valid)
	if UsesSymlinks() {
		// create a plain file that is not a link and not a marker
		p := filepath.Join(r.Dir, "bad.txt")
		_ = os.WriteFile(p, []byte("junk"), 0o644)
		err := r.InternallyChecks()
		if err == nil {
			t.Fatal("expected error for unexpected non-link file")
		}
	}
}

func TestNestedTargets(t *testing.T) {
	r := newTestRegistry(t)
	base := t.TempDir()
	targets := []string{
		filepath.Join(base, "etc", "nginx", "nginx.conf"),
		filepath.Join(base, "opt", "app", "app.yml"),
	}
	for _, abs := range targets {
		if err := r.Add(abs); err != nil {
			t.Fatal(err)
		}
	}
	list, err := r.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2, got %d: %v", len(list), list)
	}
}
