// Package registry maintains the links/ symlink-based registry of monitored
// paths.
package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/example/cm/internal/model"
)

// registryEntryMode records whether the platform permits real symlinks.
var registryEntryMode = func() string {
	return symlinkMode()
}()

func symlinkMode() string {
	if runtime.GOOS == "windows" {
		dir, err := os.MkdirTemp("", "cmlinktest")
		if err != nil {
			return "file"
		}
		defer os.RemoveAll(dir)
		link := filepath.Join(dir, "l")
		if err := os.Symlink(filepath.Join(dir, "t"), link); err != nil {
			return "file"
		}
		return "symlink"
	}
	return "symlink"
}

// Registry manages the links/ directory.
type Registry struct {
	// Dir is the links/ directory.
	Dir string
}

// New returns a Registry rooted at linksDir.
func New(linksDir string) *Registry {
	return &Registry{Dir: linksDir}
}

// UsesSymlinks reports whether this platform records entries as real symlinks.
func UsesSymlinks() bool { return registryEntryMode == "symlink" }

// relFor builds the links/ file path for an absolute monitored path.
func (r *Registry) relFor(abs string) string {
	return filepath.FromSlash(model.RegistryPath(abs))
}

// Add registers a monitored path by creating the registry symlink. If the
// platform cannot create symlinks, a marker file is written instead.
func (r *Registry) Add(abs string) error {
	p := r.relFor(abs)
	full := filepath.Join(r.Dir, p)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	if registryEntryMode == "symlink" {
		if err := os.Symlink(abs, full); err != nil {
			return os.WriteFile(full, []byte(abs), 0o644)
		}
		return nil
	}
	return os.WriteFile(full, []byte(abs), 0o644)
}

// IsRegistered reports whether a monitored path is already registered.
func (r *Registry) IsRegistered(abs string) bool {
	_, err := os.Lstat(filepath.Join(r.Dir, r.relFor(abs)))
	return err == nil
}

// Remove unregisters a monitored path by removing its registry entry.
func (r *Registry) Remove(abs string) error {
	full := filepath.Join(r.Dir, r.relFor(abs))
	if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// entryTarget returns the monitored target path represented by a registry file,
// whether it is a symlink or a marker file. ok=false for invalid entries.
func entryTarget(path string, info os.FileInfo) (string, bool) {
	if info.Mode()&os.ModeSymlink != 0 {
		t, err := os.Readlink(path)
		return t, err == nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(b)), true
}

// List enumerates monitored absolute paths by walking links/.
func (r *Registry) List() ([]string, error) {
	var out []string
	err := filepath.Walk(r.Dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == r.Dir {
			return nil
		}
		if info.IsDir() {
			return nil // recurse
		}
		target, ok := entryTarget(path, info)
		if !ok {
			return nil
		}
		out = append(out, target)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// InternallyChecks returns descriptive errors for invalid registry entries.
func (r *Registry) InternallyChecks() error {
	seen := map[string]string{}
	return filepath.Walk(r.Dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || path == r.Dir {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if registryEntryMode == "symlink" && info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("invalid registry state: non-symlink entry at %s", rel(r.Dir, path))
		}
		target, ok := entryTarget(path, info)
		if !ok {
			return fmt.Errorf("invalid registry state: broken entry at %s", rel(r.Dir, path))
		}
		if !filepath.IsAbs(target) {
			return fmt.Errorf("invalid registry state: relative target %q at %s", target, rel(r.Dir, path))
		}
		key := filepath.Clean(target)
		if prev, dup := seen[key]; dup {
			return fmt.Errorf("invalid registry state: duplicate target %q (%s and %s)", target, prev, rel(r.Dir, path))
		}
		seen[key] = rel(r.Dir, path)
		return nil
	})
}

func rel(root, p string) string {
	r, err := filepath.Rel(root, p)
	if err != nil {
		return p
	}
	return r
}
