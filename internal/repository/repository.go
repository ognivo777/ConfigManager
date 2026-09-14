// Package repository wraps the system Git executable operating on the files/
// directory as its working tree and repository root.
package repository

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Repository manages the Git repository inside files/.
type Repository struct {
	// Dir is the files/ directory acting as worktree + repo.
	Dir string
}

// New returns a Repository rooted at filesDir.
func New(filesDir string) *Repository {
	return &Repository{Dir: filesDir}
}

// Init initializes the Git repository if it does not already exist.
func (r *Repository) Init(ctx context.Context) error {
	if _, err := os.Stat(filepath.Join(r.Dir, ".git")); err == nil {
		return nil
	}
	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		return err
	}
	_, err := r.run(ctx, nil, "init", "-q")
	if err != nil {
		return err
	}
	// Ensure local identity so automated commits work without a global config.
	_, _ = r.run(ctx, nil, "config", "user.name", "cm")
	_, _ = r.run(ctx, nil, "config", "user.email", "cm@localhost")
	return nil
}

// git runs a git command in the repository worktree, passing args directly
// (no shell).
func (r *Repository) run(ctx context.Context, input []byte, args ...string) ([]byte, error) {
	full := append([]string{"-C", r.Dir}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Dir = r.Dir
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return out.Bytes(), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

// FullPath builds the snapshot file path within files/ for a monitored path.
func (r *Repository) FullPath(rel string) string {
	return filepath.Join(r.Dir, filepath.FromSlash(rel))
}

// WriteSnapshot writes a monitored file's content into files/ at rel path.
func (r *Repository) WriteSnapshot(rel string, content []byte) error {
	p := r.FullPath(rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, content, 0o644)
}

// RemoveSnapshot removes a snapshot at rel path from the worktree.
func (r *Repository) RemoveSnapshot(rel string) error {
	p := r.FullPath(rel)
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// SnapshotExists reports whether a snapshot file exists at rel path.
func (r *Repository) SnapshotExists(rel string) bool {
	_, err := os.Stat(r.FullPath(rel))
	return err == nil
}

// Stage stages the given snapshot rel-paths.
func (r *Repository) Stage(ctx context.Context, rels []string) error {
	if len(rels) == 0 {
		return nil
	}
	args := []string{"add", "--"}
	args = append(args, rels...)
	_, err := r.run(ctx, nil, args...)
	return err
}

// Commit creates a commit with the given message returned via -F. Returns the
// short commit id.
func (r *Repository) Commit(ctx context.Context, message string) (string, error) {
	if _, err := r.run(ctx, nil, "commit", "-q", "-m", message); err != nil {
		return "", err
	}
	out, err := r.run(ctx, nil, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Status reports untracked/modified/deleted staged changes summary. Returns
// whether there is anything to commit.
func (r *Repository) HasChanges(ctx context.Context) (bool, error) {
	out, err := r.run(ctx, nil, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return len(bytes.TrimSpace(out)) > 0, nil
}

// CommitMessageFromState returns an inline commit message (used directly).
func CommitMessageFromState(lines []string, userMsg string) string {
	var b strings.Builder
	for i, l := range lines {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(l)
	}
	if userMsg != "" {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(userMsg)
	}
	return b.String()
}
