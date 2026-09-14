package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/example/cm/internal/model"
)

// AddRequest describes a file registration request.
type AddRequest struct {
	// AbsPath is the already-resolved and validated absolute path.
	AbsPath string
}

// Add registers a new monitored file: registry symlink + snapshot + initial commit.
func (d *Daemon) Add(req AddRequest) error {
	abs := req.AbsPath
	d.mu.Lock()
	if _, exists := d.monitored[abs]; exists {
		d.mu.Unlock()
		return fmt.Errorf("already registered: %s", abs)
	}
	d.mu.Unlock()

	fi, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("file not found: %s", abs)
	}
	if fi.IsDir() {
		return fmt.Errorf("not a regular file: %s", abs)
	}

	rel := d.monitoredRel(abs)

	// 1. registry symlink
	if err := d.links.Add(abs); err != nil {
		return fmt.Errorf("failed to update registry: %w", err)
	}

	// 2. snapshot of current content
	content, exists, err := d.probe(abs)
	if err != nil {
		return fmt.Errorf("failed to read monitored file: %w", err)
	}
	// file was verified to exist by the caller; but guard anyway
	if !exists {
		return fmt.Errorf("file does not exist: %s", abs)
	}
	if err := d.repo.WriteSnapshot(rel, content); err != nil {
		return fmt.Errorf("failed to create snapshot: %w", err)
	}

	// 3. record in-memory state
	d.mu.Lock()
	mp := &model.MonitoredPath{
		AbsPath:     abs,
		LastContent: content,
		Exists:      true,
		Pending:     model.Clean,
		SaveCount:   0,
	}
	d.monitored[abs] = mp
	d.mu.Unlock()

	// watch it
	if d.watch != nil {
		d.watch.Add(abs)
	}

	// 4. stage + initial commit immediately
	if err := d.repo.Stage(context.Background(), []string{rel}); err != nil {
		return fmt.Errorf("failed to stage snapshot: %w", err)
	}
	msg := fmt.Sprintf("Add monitored file: %s", abs)
	commitID, err := d.repo.Commit(context.Background(), msg)
	if err != nil {
		return fmt.Errorf("failed to create initial commit: %w", err)
	}
	d.mu.Lock()
	d.lastCommit = commitID
	d.mu.Unlock()
	d.log.Info("added monitored file", "path", abs, "commit", commitID)
	return nil
}

// ListEntry describes one monitored path for the `ls` command.
type ListEntry struct {
	// AbsPath is the absolute monitored path.
	AbsPath string
	// Exists reports whether the monitored file currently exists.
	Exists bool
	// State is the logical pending state (clean/changed/deleted).
	State string
}

// List returns all currently monitored paths with their current presence and
// pending state.
func (d *Daemon) List() []ListEntry {
	d.mu.Lock()
	defer d.mu.Unlock()

	keys := make([]string, 0, len(d.monitored))
	for abs := range d.monitored {
		keys = append(keys, abs)
	}
	sort.Strings(keys)

	entries := make([]ListEntry, 0, len(keys))
	for _, abs := range keys {
		mp := d.monitored[abs]
		// Reflect the true current filesystem presence so deleted files show up
		// even before a filesystem event has been processed.
		entries = append(entries, ListEntry{
			AbsPath: abs,
			Exists:  fileExists(abs),
			State:   mp.Pending.String(),
		})
	}
	return entries
}

// fileExists reports whether the path exists as a regular file.
func fileExists(abs string) bool {
	fi, err := os.Stat(abs)
	return err == nil && !fi.IsDir()
}

// HistoryOptions configures a history query.
type HistoryOptions struct {
	Limit int    // 0 = all
	File  string // absolute monitored path, empty = all files
}

// HistoryEntry is one row of human-readable history output.
type HistoryEntry struct {
	Date    string
	File    string
	Change  string // e.g. +12/-4, binary, deleted
	Commit  string
	Subject string
}

// History returns fully expanded history rows for CLI display.
func (d *Daemon) History(ctx context.Context, opts HistoryOptions) ([]HistoryEntry, error) {
	rel := ""
	if opts.File != "" {
		rel = d.monitoredRel(opts.File)
	}
	logs, err := d.repo.History(ctx, rel, opts.Limit)
	if err != nil {
		return nil, err
	}

	var entries []HistoryEntry
	for _, e := range logs {
		stats, err := d.repo.DiffStats(ctx, e.ID, rel)
		if err != nil {
			return nil, err
		}
		fullMsg, _ := d.repo.CommitMessage(ctx, e.ID)
		for f, change := range stats {
			display := f
			if opts.File != "" {
				display = opts.File
			}
			entries = append(entries, HistoryEntry{
				Date:    e.Date,
				File:    display,
				Change:  change,
				Commit:  e.ID,
				Subject: fullMsg,
			})
		}
	}
	return entries, nil
}

// DiffOptions configures a diff query.
type DiffOptions struct {
	Commit string // empty = most recent relevant commit
	File   string
	All    bool // system-wide (ignore file) - for "latest commit" mode
}

// Diff returns the diff body for the requested commit/file combination.
func (d *Daemon) Diff(ctx context.Context, opts DiffOptions) (string, error) {
	commit := opts.Commit
	if commit == "" {
		// resolve most recent relevant commit
		rel := ""
		if opts.File != "" {
			rel = d.monitoredRel(opts.File)
		}
		logs, err := d.repo.History(ctx, rel, 1)
		if err != nil {
			return "", err
		}
		if len(logs) == 0 {
			return "", fmt.Errorf("no commits found")
		}
		commit = logs[0].ID
	}
	rel := ""
	// If a specific commit given without file, show everything.
	if opts.File != "" {
		rel = d.monitoredRel(opts.File)
	}
	return d.repo.DiffBody(ctx, commit, rel)
}

// RestoreRequest describes a restore operation.
type RestoreRequest struct {
	Commit string
	All    bool
}

// RestoreFile is one file that a restore would affect.
type RestoreFile struct {
	AbsPath string
	Rel     string
}

// RestorePreview describes files a restore would affect, for confirmation.
type RestorePreview struct {
	Commit      string
	Message     string
	Files       []RestoreFile
	CurrentDiff string
}

// PreviewRestore returns the affected files and commit message for a commit,
// WITHOUT performing any restore or commit. Used by the CLI for confirmation.
func (d *Daemon) PreviewRestore(ctx context.Context, commit string) (*RestorePreview, error) {
	affected, err := d.repo.AffectedFiles(ctx, commit)
	if err != nil {
		return nil, fmt.Errorf("failed to identify affected files: %w", err)
	}
	msg, err := d.repo.CommitMessage(ctx, commit)
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	absByRel := map[string]string{}
	for abs := range d.monitored {
		absByRel[d.monitoredRel(abs)] = abs
	}
	d.mu.Unlock()

	prev := &RestorePreview{Commit: commit, Message: msg}
	for _, rel := range affected {
		if abs, ok := absByRel[rel]; ok {
			prev.Files = append(prev.Files, RestoreFile{AbsPath: abs, Rel: rel})
		}
	}
	return prev, nil
}

// RestoreResult describes the outcome of a restore.
type RestoreResult struct {
	Restored []string
	Commit   string
}

// PendingMaster is exposed for restore: returns whether uncommitted changes exist.
func (d *Daemon) PendingExists() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.hasPending()
}

// ForcedCommit commits pending changes immediately (used before restore).
func (d *Daemon) ForcedCommit() error {
	pending := d.collectPending()
	if len(pending) == 0 {
		return nil
	}
	return d.commit(pending)
}

// Restore performs the restore workflow. Confirmation is done by the caller
// (CLI) before invoking this.
func (d *Daemon) Restore(ctx context.Context, req RestoreRequest) (*RestoreResult, error) {
	// 1. identify target commit and its affected files (snapshot rel paths)
	affected, err := d.repo.AffectedFiles(ctx, req.Commit)
	if err != nil {
		return nil, fmt.Errorf("failed to identify affected files: %w", err)
	}
	if len(affected) == 0 {
		return nil, fmt.Errorf("no files affected by commit %s", req.Commit)
	}

	// map snapshot rel -> abs monitored path (guard that they are registered)
	d.mu.Lock()
	absByRel := map[string]string{}
	registered := map[string]bool{}
	for abs := range d.monitored {
		registered[abs] = true
		absByRel[d.monitoredRel(abs)] = abs
	}
	d.mu.Unlock()

	var selected []string
	for _, rel := range affected {
		abs, ok := absByRel[rel]
		if !ok {
			continue
		}
		if !registered[abs] {
			continue
		}
		selected = append(selected, abs)
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("commit %s affects no registered monitored files", req.Commit)
	}

	// 2. forced pre-restore commit of current changes
	if d.PendingExists() {
		if err := d.ForcedCommit(); err != nil {
			return nil, fmt.Errorf("failed to commit current changes before restore: %w", err)
		}
	}

	// 3. restore each selected file
	var restored []string
	for _, abs := range selected {
		rel := d.monitoredRel(abs)
		content, exists, err := d.repo.FileStatement(ctx, req.Commit, rel)
		if err != nil {
			return nil, fmt.Errorf("failed to read historical snapshot: %w", err)
		}
		if err := d.restoreOne(abs, content, exists); err != nil {
			return nil, fmt.Errorf("failed to restore file %s: %w", abs, err)
		}
		// update the in-sync snapshot under files/ so the restore commit is valid
		if exists {
			if err := d.repo.WriteSnapshot(rel, content); err != nil {
				return nil, fmt.Errorf("failed to update snapshot for %s: %w", abs, err)
			}
		} else {
			if err := d.repo.RemoveSnapshot(rel); err != nil {
				return nil, fmt.Errorf("failed to remove snapshot for %s: %w", abs, err)
			}
		}
		restored = append(restored, abs)
	}

	// 4. create restore commit (stage + commit all restored files)
	var rels []string
	for _, abs := range selected {
		rels = append(rels, d.monitoredRel(abs))
	}
	if err := d.repo.Stage(ctx, rels); err != nil {
		return nil, fmt.Errorf("failed to stage restored files: %w", err)
	}
	msg := fmt.Sprintf("Restore from commit %s", req.Commit)
	commitID, err := d.repo.Commit(ctx, msg)
	if err != nil {
		return nil, fmt.Errorf("failed to create restore commit: %w", err)
	}
	d.mu.Lock()
	d.lastCommit = commitID
	d.mu.Unlock()

	// update in-memory LastContent to match restored content
	d.mu.Lock()
	for _, abs := range restored {
		mp := d.monitored[abs]
		rel := d.monitoredRel(abs)
		content, ok, _ := d.repo.FileStatement(ctx, commitID, rel)
		if ok {
			mp.LastContent = content
		}
		mp.Pending = model.Clean
		mp.SaveCount = 0
		mp.Exists = d.monitored[abs].Exists
	}
	d.mu.Unlock()

	d.log.Info("restore committed", "commit", commitID)
	return &RestoreResult{Restored: restored, Commit: commitID}, nil
}

// restoreOne atomically writes content to abs path, or removes it if the
// historical state was deleted.
func (d *Daemon) restoreOne(abs string, content []byte, exists bool) error {
	if !exists {
		if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
			return err
		}
		// update in-memory existence
		d.mu.Lock()
		if mp, ok := d.monitored[abs]; ok {
			mp.Exists = false
		}
		d.mu.Unlock()
		return nil
	}

	// atomic write via temp file + rename
	dir := filepath.Dir(abs)
	tmp, err := os.CreateTemp(dir, ".cm-restore-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, abs); err != nil {
		return err
	}

	// verify resulting contents
	verify, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("failed to verify restored contents: %w", err)
	}
	if !bytesEqual(verify, content) {
		return fmt.Errorf("restored file verification failed: %s", abs)
	}
	return nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// SetMessage stores the pending user message.
func (d *Daemon) SetMessage(text string) error { return d.state.SetMessage(text) }
