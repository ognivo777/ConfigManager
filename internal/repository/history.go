package repository

import (
	"context"
	"strconv"
	"strings"
)

// LogEntry is a single commit relevant to history output.
type LogEntry struct {
	ID      string
	Date    string
	Subject string
}

// History returns commit entries. limit==0 means all. When rel is non-empty,
// only commits touching that snapshot path are returned.
func (r *Repository) History(ctx context.Context, rel string, limit int) ([]LogEntry, error) {
	args := []string{"log", "--format=%H|%aI|%s"}
	if rel != "" {
		args = append(args, "--", rel)
	}
	if limit > 0 {
		args = append(args, "-n", strconv.Itoa(limit))
	}
	out, err := r.run(ctx, nil, args...)
	if err != nil {
		return nil, err
	}
	var entries []LogEntry
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 2 {
			continue
		}
		subject := ""
		if len(parts) == 3 {
			subject = parts[2]
		}
		entries = append(entries, LogEntry{ID: parts[0], Date: parts[1], Subject: subject})
	}
	return entries, nil
}

// DiffStats returns per-file +/- line counts for a commit. rel filters to a
// single snapshot path (may be empty for all files). Returns a map keyed by
// snapshot rel path.
func (r *Repository) DiffStats(ctx context.Context, commit, rel string) (map[string]string, error) {
	args := []string{"show", "--numstat", "--format=", commit}
	if rel != "" {
		args = append(args, "--", rel)
	}
	out, err := r.run(ctx, nil, args...)
	if err != nil {
		return nil, err
	}
	stats := map[string]string{}
	// numstat format: <added>\t<deleted>\t<path>
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 3 {
			continue
		}
		added, deleted, p := fields[0], fields[1], strings.Join(fields[2:], "\t")
		if added == "-" || deleted == "-" {
			stats[p] = "binary"
			continue
		}
		stats[p] = "+" + added + "/-" + deleted
	}
	return stats, nil
}

// CommitSubject returns the subject line of a commit.
func (r *Repository) CommitSubject(ctx context.Context, commit string) (string, error) {
	out, err := r.run(ctx, nil, "log", "-1", "--format=%s", commit)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// CommitMessage returns the full commit message body (subject + body).
func (r *Repository) CommitMessage(ctx context.Context, commit string) (string, error) {
	out, err := r.run(ctx, nil, "log", "-1", "--format=%B", commit)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// FileStatement loads content of a snapshot rel path at a given commit.
// exists=false when the file was absent at that commit.
func (r *Repository) FileStatement(ctx context.Context, commit, rel string) (content []byte, exists bool, err error) {
	out, err := r.run(ctx, nil, "show", commit+":"+rel)
	if err != nil {
		if !strings.Contains(err.Error(), "exists on disk, but not in") &&
			!strings.Contains(err.Error(), "does not exist") {
			return nil, false, err
		}
		return nil, false, nil
	}
	return out, true, nil
}

// DiffBody returns the textual diff for a commit (optionally filtered to a
// single rel path).
func (r *Repository) DiffBody(ctx context.Context, commit, rel string) (string, error) {
	args := []string{"diff", commit + "^.." + commit}
	if rel != "" {
		args = append(args, "--", rel)
	}
	out, err := r.run(ctx, nil, args...)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// AffectedFiles lists snapshot rel paths changed by a commit. Uses numstat so
// it also works for root commits where diff-tree returns nothing.
func (r *Repository) AffectedFiles(ctx context.Context, commit string) ([]string, error) {
	out, err := r.run(ctx, nil, "show", "--numstat", "--format=", commit)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 3 {
			continue
		}
		p := strings.Join(fields[2:], "\t")
		files = append(files, p)
	}
	return files, nil
}
