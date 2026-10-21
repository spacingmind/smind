package arm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DirPrefix names every directory this package's callers create under the
// arms root; the sweep removes only entries with it.
const DirPrefix = "arm-"

// SweepOrphans removes arm directories under root that are older than minAge
// (a crashed daemon left them): it runs at daemon start, when no probe can be
// live. Only direct children named DirPrefix* that are real directories are
// removed (never a symlink, never anything else), RemoveAll does not follow
// links, and a missing root is not an error. It returns the number removed.
func SweepOrphans(root string, minAge time.Duration, now time.Time) (int, error) {
	if !filepath.IsAbs(root) {
		return 0, fmt.Errorf("arm: sweep root %q is not absolute", root)
	}
	ents, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("arm: sweep: %w", err)
	}
	removed := 0
	var firstErr error
	for _, e := range ents {
		if !strings.HasPrefix(e.Name(), DirPrefix) {
			continue
		}
		p := filepath.Join(root, e.Name())
		fi, err := os.Lstat(p)
		if err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if now.Sub(fi.ModTime()) < minAge {
			continue
		}
		// Arm files may be read-only: make the tree writable first.
		_ = filepath.WalkDir(p, func(q string, d os.DirEntry, werr error) error {
			if werr == nil && d.IsDir() {
				_ = os.Chmod(q, 0o700)
			}
			return nil
		})
		if err := os.RemoveAll(p); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("arm: sweep %s: %w", e.Name(), err)
			}
			continue
		}
		removed++
	}
	return removed, firstErr
}

// Remove deletes one arm directory at arm end (R4). It refuses a path that is
// not a direct DirPrefix* child of root, so a bug elsewhere cannot point it
// at a real directory.
func Remove(root, dir string) error {
	root, dir = filepath.Clean(root), filepath.Clean(dir)
	if filepath.Dir(dir) != root || !strings.HasPrefix(filepath.Base(dir), DirPrefix) {
		return fmt.Errorf("arm: refusing to remove %q: not an arm directory under %q", dir, root)
	}
	_ = filepath.WalkDir(dir, func(q string, d os.DirEntry, werr error) error {
		if werr == nil && d.IsDir() {
			_ = os.Chmod(q, 0o700)
		}
		return nil
	})
	return os.RemoveAll(dir)
}
