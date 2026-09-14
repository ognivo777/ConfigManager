package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveValidate(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.conf")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	abs, err := resolveValidate(f)
	if err != nil {
		t.Fatalf("resolveValidate file: %v", err)
	}
	if !filepath.IsAbs(abs) {
		t.Fatalf("expected absolute, got %q", abs)
	}

	// directory rejected
	if _, err := resolveValidate(dir); err == nil {
		t.Fatal("expected error for directory")
	}
	// nonexistent rejected
	if _, err := resolveValidate(filepath.Join(dir, "nope")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestShortDate(t *testing.T) {
	if got := shortDate("2026-09-13T12:01:10+03:00"); strings.Contains(got, "T") {
		t.Fatalf("shortDate kept T: %q", got)
	}
}

func TestShortID(t *testing.T) {
	if got := shortID("0123456789abcdef"); got != "0123456" {
		t.Fatalf("shortID = %q", got)
	}
}

func TestUsageOutput(t *testing.T) {
	var b strings.Builder
	usage(&b)
	if !strings.Contains(b.String(), "cm add") {
		t.Fatal("usage missing commands")
	}
	if !strings.Contains(b.String(), "cm ls") {
		t.Fatal("usage missing ls command")
	}
}

func TestIsUnder(t *testing.T) {
	dir := filepath.Join("a", "b")
	if !isUnder(filepath.Join("a", "b"), dir) {
		t.Fatal("same dir should be under")
	}
	if !isUnder(filepath.Join("a", "b", "c", "f.conf"), dir) {
		t.Fatal("subdir file should be under")
	}
	if isUnder(filepath.Join("a", "x.conf"), dir) {
		t.Fatal("parent file should not be under")
	}
	if isUnder(filepath.Join("a", "b2-and-bash", "f"), dir) {
		t.Fatal("sibling with similar prefix must not be under")
	}
}
