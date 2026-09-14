// Package daemon implements the background cm daemon: filesystem monitoring,
// change detection, batching, git operations, and restoration.
package daemon

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/example/cm/internal/config"
	"github.com/example/cm/internal/model"
	"github.com/example/cm/internal/registry"
	"github.com/example/cm/internal/repository"
	"github.com/example/cm/internal/state"
	"github.com/example/cm/internal/watcher"
)

const batchWindow = 60 * time.Second

// Daemon coordinates the monitoring pipeline.
//
// Locking: d.mu guards the monitored map, batch timer state, and lastCommit.
// Methods documented as "locked" require the caller to hold d.mu.
type Daemon struct {
	log  *slog.Logger
	root string

	links *registry.Registry
	repo  *repository.Repository
	state *state.State
	watch *watcher.Watcher

	mu         sync.Mutex
	monitored  map[string]*model.MonitoredPath
	lastCommit string

	batchTimer  *time.Timer
	batchActive bool
	batchWindow time.Duration
	stopNotify  chan struct{}
	work        chan func()
	done        chan struct{}
}

// New creates a Daemon for a repository root.
func New(root string, log *slog.Logger) (*Daemon, error) {
	filesDir := config.FilesDir(root)
	linksDir := config.LinksDir(root)

	rep := repository.New(filesDir)
	if err := os.MkdirAll(linksDir, 0o755); err != nil {
		return nil, err
	}
	if err := rep.Init(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to initialize repository: %w", err)
	}

	return &Daemon{
		log:         log,
		root:        root,
		links:       registry.New(linksDir),
		repo:        rep,
		state:       state.New(filepath.Join(root, "state", "message")),
		monitored:   map[string]*model.MonitoredPath{},
		batchWindow: batchWindow,
	}, nil
}

func (d *Daemon) monitoredRel(abs string) string { return model.SnapshotRelPath(abs) }
func (d *Daemon) registryPath() string           { return d.links.Dir }

// Start begins monitoring: scans the registry and starts the watcher.
func (d *Daemon) Start() error {
	if err := d.validateRegistry(); err != nil {
		d.log.Error("invalid registry state", "error", err)
	}

	w, err := watcher.New(d.log)
	if err != nil {
		return fmt.Errorf("failed to start filesystem watcher: %w", err)
	}

	if err := d.loadRegistry(w); err != nil {
		_ = w.Close()
		return fmt.Errorf("failed to load registry: %w", err)
	}

	d.watch = w
	// Single worker goroutine serializes all event handling and batch commits,
	// eliminating races between reconcile and commit.
	d.work = make(chan func(), 64)
	d.stopNotify = make(chan struct{})
	d.done = make(chan struct{})
	go d.loop()
	return nil
}

// loop is the single serialized event-processing goroutine.
func (d *Daemon) loop() {
	defer close(d.done)
	for {
		select {
		case <-d.stopNotify:
			return
		case fn := <-d.work:
			fn()
		case ev, ok := <-d.watch.Events:
			if !ok {
				return
			}
			d.handleFileEvent(ev)
		case err, ok := <-d.watch.Errors:
			if ok && err != nil {
				d.log.Error("watcher error", "error", err)
			}
		}
	}
}

// loadRegistry scans links/, watches each path, and reconciles initial state.
func (d *Daemon) loadRegistry(w *watcher.Watcher) error {
	absPaths, err := d.links.List()
	if err != nil {
		return err
	}
	d.mu.Lock()
	for _, abs := range absPaths {
		mp := &model.MonitoredPath{AbsPath: abs}
		rel := d.monitoredRel(abs)
		if content, ok, err := d.readSnapshot(rel); err == nil && ok {
			mp.LastContent = content
			mp.Exists = true
		}
		d.monitored[abs] = mp
		w.Add(abs)
	}
	// initial reconciliation
	d.reconcileAll()
	hasPending := d.hasPending()
	d.mu.Unlock()

	if hasPending {
		d.startBatch()
	}
	return nil
}

// validateRegistry reports invalid manual registry modifications.
func (d *Daemon) validateRegistry() error { return d.links.InternallyChecks() }

// readSnapshot returns committed snapshot content in files/.
func (d *Daemon) readSnapshot(rel string) ([]byte, bool, error) {
	b, err := os.ReadFile(d.repo.FullPath(rel))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return b, true, nil
}

func (d *Daemon) handleFileEvent(ev watcher.Event) {
	d.mu.Lock()
	mp, ok := d.monitored[ev.Path]
	if !ok {
		// Atomic replacement may yield events for sibling temp files; match by
		// parent directory to the exact monitored path name.
		base := filepath.Base(ev.Path)
		for a, m := range d.monitored {
			if filepath.Base(a) == base && filepath.Dir(a) == filepath.Dir(ev.Path) {
				mp, ok = m, true
				break
			}
		}
	}
	if ok && d.reconcile(mp) {
		d.startBatchLocked()
	}
	d.mu.Unlock()
}

// reconcile updates pending state by comparing current fs content with
// LastContent. Caller must hold d.mu. Returns true if a transition occurred.
func (d *Daemon) reconcile(mp *model.MonitoredPath) bool {
	content, exists, err := d.probe(mp.AbsPath)
	if err != nil {
		d.log.Error("failed to read monitored file", "path", mp.AbsPath, "error", err)
		return false
	}

	if !exists {
		prevExists := mp.Exists
		mp.Exists = false
		if mp.Pending != model.Deleted {
			mp.Pending = model.Deleted
			mp.SaveCount++
		}
		return mp.Exists != prevExists
	}

	changed := mp.Exists && !bytes.Equal(content, mp.LastContent)
	if !mp.Exists {
		// recreation
		mp.LastContent = content
		mp.Exists = true
		if mp.Pending != model.Changed {
			mp.Pending = model.Changed
			mp.SaveCount++
		}
		return true
	}

	if changed {
		if mp.Pending != model.Changed {
			mp.Pending = model.Changed
			mp.SaveCount++
		}
		mp.LastContent = content
		return true
	}
	return false
}

func (d *Daemon) probe(abs string) ([]byte, bool, error) {
	b, err := os.ReadFile(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return b, true, nil
}

// reconcileAll recomputes state for all monitored paths. Caller must hold d.mu.
func (d *Daemon) reconcileAll() {
	for _, mp := range d.monitored {
		d.reconcile(mp)
	}
}

// hasPending reports whether any path has a non-clean state. Caller holds d.mu.
func (d *Daemon) hasPending() bool {
	for _, mp := range d.monitored {
		if mp.Pending != model.Clean {
			return true
		}
	}
	return false
}

// startBatch starts the global window if not active. Safe to call without lock.
func (d *Daemon) startBatch() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.startBatchLocked()
}

// startBatchLocked starts the global window. Caller must hold d.mu.
func (d *Daemon) startBatchLocked() {
	if d.batchActive || d.batchTimer != nil {
		return
	}
	d.batchActive = true
	d.batchTimer = time.AfterFunc(d.batchWindow, func() {
		// schedule commit on the serialized worker loop
		select {
		case d.work <- func() { d.commitBatch() }:
		case <-d.stopNotify:
		}
	})
	d.log.Info("batch window started")
}

func (d *Daemon) commitBatch() {
	pending := d.collectPending()
	if len(pending) == 0 {
		d.resetBatch()
		return
	}
	if err := d.commit(pending); err != nil {
		d.log.Error("failed to commit Git changes; retaining pending state", "error", err)
		d.mu.Lock()
		select {
		case <-d.stopNotify:
			d.batchActive = false
		default:
			d.batchTimer = time.AfterFunc(d.batchWindow, func() {
				select {
				case d.work <- func() { d.commitBatch() }:
				case <-d.stopNotify:
				}
			})
		}
		d.mu.Unlock()
		return
	}
	d.clearPending()
	d.resetBatch()

	// A change may have occurred while the batch window/commit was in flight.
	// Reconcile and re-arm the batch if there is new pending work.
	d.mu.Lock()
	d.reconcileAll()
	again := d.hasPending()
	if again {
		d.startBatchLocked()
	}
	d.mu.Unlock()
}

func (d *Daemon) collectPending() []*model.MonitoredPath {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []*model.MonitoredPath
	for _, mp := range d.monitored {
		if mp.Pending != model.Clean {
			out = append(out, mp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AbsPath < out[j].AbsPath })
	return out
}

// commit performs snapshot updates, staging, and one git commit.
func (d *Daemon) commit(pending []*model.MonitoredPath) error {
	ctx := context.Background()

	var stagedRels []string
	var lines []string

	d.mu.Lock()
	for _, mp := range pending {
		rel := d.monitoredRel(mp.AbsPath)
		switch mp.Pending {
		case model.Deleted:
			// Re-probe: the path may have been recreated during the batch window.
			content, exists, err := d.probe(mp.AbsPath)
			if err != nil {
				d.mu.Unlock()
				return err
			}
			if exists {
				if err := d.repo.WriteSnapshot(rel, content); err != nil {
					d.mu.Unlock()
					return err
				}
				mp.LastContent = content
				mp.Exists = true
				mp.Pending = model.Changed
				lines = append(lines, fmt.Sprintf("%s: %d saves", base(mp.AbsPath), mp.SaveCount))
				stagedRels = append(stagedRels, rel)
			} else {
				if err := d.repo.RemoveSnapshot(rel); err != nil {
					d.mu.Unlock()
					return err
				}
				lines = append(lines, base(mp.AbsPath)+": deleted")
				stagedRels = append(stagedRels, rel)
			}
		case model.Changed:
			content, exists, err := d.probe(mp.AbsPath)
			if err != nil {
				d.mu.Unlock()
				return err
			}
			if exists {
				if err := d.repo.WriteSnapshot(rel, content); err != nil {
					d.mu.Unlock()
					return err
				}
				mp.LastContent = content
				lines = append(lines, fmt.Sprintf("%s: %d saves", base(mp.AbsPath), mp.SaveCount))
				stagedRels = append(stagedRels, rel)
			} else {
				if err := d.repo.RemoveSnapshot(rel); err != nil {
					d.mu.Unlock()
					return err
				}
				lines = append(lines, base(mp.AbsPath)+": deleted")
				stagedRels = append(stagedRels, rel)
			}
		}
	}
	d.mu.Unlock()

	if err := d.repo.Stage(ctx, stagedRels); err != nil {
		return err
	}

	userMsg, err := d.state.TakeAndClear()
	if err != nil {
		d.log.Warn("failed to clear pending message", "error", err)
		userMsg = d.state.Message()
	}

	msg := repository.CommitMessageFromState(lines, userMsg)
	commitID, err := d.repo.Commit(ctx, msg)
	if err != nil {
		if userMsg != "" {
			_ = d.state.SetMessage(userMsg)
		}
		return err
	}
	d.mu.Lock()
	d.lastCommit = commitID
	d.mu.Unlock()
	d.log.Info("committed batch", "commit", commitID)
	return nil
}

func (d *Daemon) clearPending() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, mp := range d.monitored {
		mp.Pending = model.Clean
		mp.SaveCount = 0
	}
}

func (d *Daemon) resetBatch() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.batchActive = false
	if d.batchTimer != nil {
		d.batchTimer.Stop()
		d.batchTimer = nil
	}
}

// Close stops the watcher and pending timer.
func (d *Daemon) Close() error {
	d.mu.Lock()
	if d.batchTimer != nil {
		d.batchTimer.Stop()
		d.batchTimer = nil
	}
	if d.stopNotify != nil {
		close(d.stopNotify)
	}
	d.mu.Unlock()
	if d.watch != nil {
		_ = d.watch.Close()
	}
	if d.done != nil {
		<-d.done
	}
	return nil
}

func base(p string) string { return filepath.Base(p) }
