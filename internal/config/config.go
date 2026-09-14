// Package config holds configuration defaults and paths.
package config

import (
	"os"
	"path/filepath"
)

// DefaultRepository is the default cm root directory.
const DefaultRepository = "/var/lib/cm"

// SocketPath returns the default Unix domain socket location.
func SocketPath(root string) string {
	return filepath.Join(runDir(), "cm.sock")
}

func runDir() string {
	if d := os.Getenv("CM_RUN_DIR"); d != "" {
		return d
	}
	return "/run"
}

// LinksDir returns the path to the registry directory for a repository root.
func LinksDir(root string) string {
	return filepath.Join(root, "links")
}

// FilesDir returns the path to the Git snapshot directory for a repository root.
func FilesDir(root string) string {
	return filepath.Join(root, "files")
}
