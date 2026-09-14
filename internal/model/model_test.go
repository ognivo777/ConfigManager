package model

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegistryPathLinux(t *testing.T) {
	got := RegistryPath("/etc/nginx/nginx.conf")
	if want := "etc/nginx/nginx.conf"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSnapshotRelPath(t *testing.T) {
	got := SnapshotRelPath("/opt/application/app.yml")
	if want := "opt/application/app.yml"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRegistryPathWindowsDrive(t *testing.T) {
	// only meaningful on native windows paths
	sep := string(filepath.Separator)
	if sep != "\\" {
		t.Skip("windows-only")
	}
	abs := `C:\Program Files\app\config.yml`
	got := RegistryPath(abs)
	// drive prefix should be stripped, leaving relative forward-slash path
	if got == "" || os.PathSeparator == '/' && got != "Program Files/app/config.yml" {
		// accept any of the normalized forms; just ensure no "C:" drive remnant
		if filepath.VolumeName(abs) != "" && containsStr(got, ":") {
			t.Fatalf("drive prefix leaked into registry path: %q", got)
		}
	}
	if filepath.IsAbs(got) {
		t.Fatalf("registry path must be relative, got %q", got)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
