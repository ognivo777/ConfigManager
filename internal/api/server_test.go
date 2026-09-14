package api

import (
	"context"
	"log/slog"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/example/cm/internal/daemon"
)

// fakeHandler implements Handler for round-trip testing.
type fakeHandler struct{}

func (f *fakeHandler) Add(req daemon.AddRequest) error { return nil }
func (f *fakeHandler) History(ctx context.Context, opts daemon.HistoryOptions) ([]daemon.HistoryEntry, error) {
	return []daemon.HistoryEntry{{Date: "d", File: "f", Change: "+1/-1", Commit: "abc", Subject: "s"}}, nil
}
func (f *fakeHandler) Diff(ctx context.Context, opts daemon.DiffOptions) (string, error) {
	return "DIFF", nil
}
func (f *fakeHandler) PreviewRestore(ctx context.Context, commit string) (*daemon.RestorePreview, error) {
	return &daemon.RestorePreview{Commit: commit, Files: []daemon.RestoreFile{{AbsPath: "/x"}}}, nil
}
func (f *fakeHandler) Restore(ctx context.Context, req daemon.RestoreRequest) (*daemon.RestoreResult, error) {
	return &daemon.RestoreResult{Restored: []string{"/x"}, Commit: "abc"}, nil
}
func (f *fakeHandler) SetMessage(text string) error { return nil }

func (f *fakeHandler) List() []daemon.ListEntry {
	return []daemon.ListEntry{{AbsPath: "/etc/app.conf", Exists: true, State: "clean"}}
}

func TestServerClientRoundTrip(t *testing.T) {
	// Unix sockets are only reliably supported on Linux/macOS.
	if runtime.GOOS == "windows" {
		t.Skip("unix sockets not supported on windows")
	}
	sock := filepath.Join(t.TempDir(), "cm.sock")
	log := slog.New(slog.NewTextHandler(stderr{}, &slog.HandlerOptions{Level: slog.LevelError}))
	srv := NewServer(sock, &fakeHandler{}, log)
	if err := srv.Start(); err != nil {
		t.Skipf("unix socket unsupported on this platform: %v", err)
	}
	defer srv.Close()
	time.Sleep(50 * time.Millisecond)

	c := NewClient(sock)
	if err := c.Call(MHistory, daemon.HistoryOptions{Limit: 20}, &[]daemon.HistoryEntry{}); err != nil {
		t.Fatalf("history call: %v", err)
	}
	if err := c.Call(MMessage, "hello", nil); err != nil {
		t.Fatalf("message call: %v", err)
	}
	// unknown method should return a remote error
	if err := c.Call("bogus", nil, nil); err == nil {
		t.Fatal("expected error for unknown method")
	}
}

// stderr is a minimal writer satisfying io.Writer for slog (discard).
type stderr struct{}

func (stderr) Write(p []byte) (int, error) { return len(p), nil }
