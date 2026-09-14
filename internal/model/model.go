// Package model defines the shared types and helpers used across cm.
package model

import (
	"path/filepath"
	"strings"
)

// FileState is the logical state of a monitored file relative to the last
// committed snapshot.
type FileState int

const (
	// Clean means the current filesystem content equals the latest committed snapshot.
	Clean FileState = iota
	// Changed means the file exists and its content differs from the latest
	// committed snapshot or the latest pending state.
	Changed
	// Deleted means the monitored path no longer exists.
	Deleted
)

func (s FileState) String() string {
	switch s {
	case Clean:
		return "clean"
	case Changed:
		return "changed"
	case Deleted:
		return "deleted"
	default:
		return "unknown"
	}
}

// MonitoredPath is a registered monitored file together with its in-memory
// pending runtime state.
type MonitoredPath struct {
	// AbsPath is the absolute, normalized monitored filesystem path.
	AbsPath string

	// LastContent is the last content state known to be staged for commit.
	// It is nil when no content has been read yet.
	LastContent []byte

	// Exists records whether the monitored path existed at the last probe.
	Exists bool

	// Pending is the logical transition awaiting commit since last commit.
	Pending FileState

	// SaveCount records logical content transitions detected since the last
	// successful commit.
	SaveCount int
}

// RegistryPath returns the path under links/ (relative, forward slashes) that
// represents AbsPath. The leading slash and any drive prefix are removed so the
// registry tree mirrors the monitored filesystem hierarchy.
func RegistryPath(abs string) string {
	clean := filepath.ToSlash(filepath.Clean(abs))
	parts := strings.Split(strings.TrimPrefix(clean, "/"), "/")
	var out []string
	for _, p := range parts {
		if p == "" {
			continue
		}
		if strings.HasSuffix(p, ":") {
			continue // drive prefix (Windows) — ignored
		}
		out = append(out, p)
	}
	return strings.Join(out, "/")
}

// SnapshotRelPath returns the path under files/ for the monitored AbsPath.
func SnapshotRelPath(abs string) string {
	return RegistryPath(abs)
}
