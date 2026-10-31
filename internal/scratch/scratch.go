// Package scratch manages per-task scratch directories under a single
// root: private (0700) dirs named by numeric task ID, safe recursive
// removal that never follows symlinks out of the task dir, sweeping of
// closed tasks, and usage accounting of reclaimable space.
package scratch

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
)

// ErrBusy is returned by Remove when the busy hook reports that the task
// still has a running run; nothing is deleted in that case.
var ErrBusy = errors.New("scratch: task has a running run")

// Removed counts what a removal actually deleted: Files is the number of
// non-directory entries removed (regular files and symlinks), Bytes the
// sum of regular file sizes.
type Removed struct {
	Files int
	Bytes int64
}

// Usage is a disk-usage snapshot of the scratch root: every numeric task
// entry contributes to Total and Dirs, and entries whose task is no
// longer live additionally contribute to Reclaimable and ReclaimableDirs.
type Usage struct {
	Total           Removed
	Dirs            int
	ReclaimableDirs int
	Reclaimable     Removed
}

// Manager owns the scratch root. Task dirs live directly under root named
// by their numeric task ID; every other entry in root is never touched.
type Manager struct {
	root string
	busy func(int64) bool
}

// Option configures a Manager.
type Option func(*Manager)

// WithBusy sets the hook reporting whether a task has a running run;
// Remove refuses to delete such a task's dir.
func WithBusy(fn func(taskID int64) bool) Option {
	return func(m *Manager) { m.busy = fn }
}

// New returns a Manager for root. The root is created lazily by Ensure.
func New(root string, opts ...Option) *Manager {
	m := &Manager{root: root}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Root returns the scratch root path.
func (m *Manager) Root() string { return m.root }

// Dir returns the scratch dir path for taskID. It is a pure join: no IO,
// and the dir may not exist yet.
func (m *Manager) Dir(taskID int64) string {
	return filepath.Join(m.root, strconv.FormatInt(taskID, 10))
}

// Ensure creates the scratch dir for taskID (and the root if needed) with
// mode 0700 and returns its path. It refuses when the root or the dir
// exists but is not a real directory (e.g. a symlink).
func (m *Manager) Ensure(taskID int64) (string, error) {
	if err := os.MkdirAll(m.root, 0o700); err != nil {
		return "", err
	}
	if rootInfo, err := os.Lstat(m.root); err != nil {
		return "", err
	} else if !rootInfo.IsDir() {
		return "", fmt.Errorf("scratch: root %s is not a directory", m.root)
	}
	p := m.Dir(taskID)
	info, err := os.Lstat(p)
	switch {
	case err == nil:
		if !info.IsDir() {
			return "", fmt.Errorf("scratch: %s exists and is not a directory", p)
		}
	case errors.Is(err, fs.ErrNotExist):
		if err := os.Mkdir(p, 0o700); err != nil {
			return "", err
		}
	default:
		return "", err
	}
	if err := os.Chmod(p, 0o700); err != nil {
		return "", err
	}
	return p, nil
}

// Remove deletes the scratch dir of taskID and returns what it removed.
// A missing dir is a no-op. A symlink or stray file at the task path is
// unlinked itself, never its target. Real task dirs are only deleted
// after verifying they resolve directly under the root.
func (m *Manager) Remove(taskID int64) (Removed, error) {
	if m.busy != nil && m.busy(taskID) {
		return Removed{}, ErrBusy
	}
	p := m.Dir(taskID)
	info, err := os.Lstat(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Removed{}, nil
		}
		return Removed{}, err
	}
	if !info.IsDir() {
		if err := os.Remove(p); err != nil {
			return Removed{}, err
		}
		return Removed{Files: 1}, nil
	}
	realRoot, err := filepath.EvalSymlinks(m.root)
	if err != nil {
		return Removed{}, err
	}
	realP, err := filepath.EvalSymlinks(p)
	if err != nil {
		return Removed{}, err
	}
	if filepath.Dir(realP) != realRoot {
		return Removed{}, fmt.Errorf("scratch: %s resolves outside the scratch root", p)
	}
	rem, walkErr := makeWritable(p)
	if err := os.RemoveAll(p); err != nil {
		return rem, err
	}
	return rem, walkErr
}

// Sweep removes every numeric task entry of the root whose task keep does
// not keep, returning the sum of what it removed. Busy tasks are skipped,
// not an error. Entries with any other name are never touched. A missing
// root is a no-op.
func (m *Manager) Sweep(keep func(taskID int64) bool) (Removed, error) {
	entries, err := os.ReadDir(m.root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Removed{}, nil
		}
		return Removed{}, err
	}
	var total Removed
	for _, e := range entries {
		id, ok := taskID(e.Name())
		if !ok || (keep != nil && keep(id)) {
			continue
		}
		rem, err := m.Remove(id)
		if err != nil {
			if errors.Is(err, ErrBusy) {
				continue
			}
			return total, err
		}
		total.Files += rem.Files
		total.Bytes += rem.Bytes
	}
	return total, nil
}

// Usage sizes every numeric task entry of the root without following
// symlinks. A missing root is a zero Usage.
func (m *Manager) Usage(live func(taskID int64) bool) (Usage, error) {
	entries, err := os.ReadDir(m.root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Usage{}, nil
		}
		return Usage{}, err
	}
	var u Usage
	for _, e := range entries {
		id, ok := taskID(e.Name())
		if !ok {
			continue
		}
		size, err := sizeTree(m.Dir(id))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return Usage{}, err
		}
		u.Dirs++
		u.Total.Files += size.Files
		u.Total.Bytes += size.Bytes
		if live == nil || !live(id) {
			u.ReclaimableDirs++
			u.Reclaimable.Files += size.Files
			u.Reclaimable.Bytes += size.Bytes
		}
	}
	return u, nil
}

// taskID parses a scratch-root entry name as a canonical positive decimal
// task ID ("7", not "07", "7x" or "-3").
func taskID(name string) (int64, bool) {
	id, err := strconv.ParseInt(name, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != name {
		return 0, false
	}
	return id, true
}

// makeWritable walks dir (without following symlinks) re-adding the owner
// bits needed to delete it: rwx on directories, w on regular files.
// WalkDir runs this callback before reading each directory, so chmoding a
// read-only dir here also opens it for walking. Chmod errors are ignored;
// os.RemoveAll surfaces them if they matter.
func makeWritable(dir string) (Removed, error) {
	var rem Removed
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			_ = os.Chmod(path, info.Mode().Perm()|0o700)
		case info.Mode().IsRegular():
			_ = os.Chmod(path, info.Mode().Perm()|0o200)
			rem.Files++
			rem.Bytes += info.Size()
		default:
			rem.Files++
		}
		return nil
	})
	return rem, err
}

// sizeTree totals a path without following symlinks: regular files count
// their size, symlinks and other non-directories count as one entry.
func sizeTree(p string) (Removed, error) {
	var rem Removed
	err := filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rem.Files++
		if d.Type().IsRegular() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			rem.Bytes += info.Size()
		}
		return nil
	})
	return rem, err
}
