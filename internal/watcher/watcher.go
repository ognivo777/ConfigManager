// Package watcher wraps github.com/fsnotify/fsnotify to produce filesystem
// event notifications for monitored paths.
package watcher

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

// Event is a normalized filesystem notification for a path.
type Event struct {
	Path string
	Op   fsnotify.Op
}

// Watcher watches a set of paths.
type Watcher struct {
	fw     *fsnotify.Watcher
	Events chan Event
	Errors chan error
	log    *slog.Logger
}

// New creates a Watcher.
func New(log *slog.Logger) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{
		fw:     fw,
		Events: make(chan Event),
		Errors: make(chan error),
		log:    log,
	}
	go w.run()
	return w, nil
}

func (w *Watcher) run() {
	for {
		select {
		case ev, ok := <-w.fw.Events:
			if !ok {
				close(w.Events)
				return
			}
			w.Events <- Event{Path: ev.Name, Op: ev.Op}
		case err, ok := <-w.fw.Errors:
			if !ok {
				close(w.Errors)
				return
			}
			w.Errors <- err
		}
	}
}

// Add starts watching a path. To detect atomic replacement and rename of the
// target, the parent directory is also watched.
func (w *Watcher) Add(path string) error {
	// Watch the file itself.
	if err := w.fw.Add(path); err != nil {
		// If the file is absent (e.g. registered-then-deleted), watching the
		// parent directory still lets us observe recreation.
		if !os.IsNotExist(err) {
			return err
		}
	}
	// Watch the parent directory for rename/replacement of the target name.
	parent := filepath.Dir(path)
	if err := w.fw.Add(parent); err != nil {
		return err
	}
	return nil
}

// Remove stops watching the path. The parent watch is shared with any other
// monitored paths, so it is left in place until Close.
func (w *Watcher) Remove(path string) error {
	if err := w.fw.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Close shuts the watcher down.
func (w *Watcher) Close() error {
	// fsnotify Close also closes the event/error channels.
	return w.fw.Close()
}
