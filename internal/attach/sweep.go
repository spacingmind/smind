package attach

import (
	"os"
	"time"
)

// Sweep deletes set directories that are not referenced and whose mtime is
// older than olderThan. Only directories directly under root with a 32-hex
// name are considered; anything else is left alone.
func (s *Store) Sweep(referenced map[string]bool, olderThan time.Duration, now time.Time) (removed int, err error) {
	entries, err := os.ReadDir(s.root)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var firstErr error
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || !setIDPattern.MatchString(name) || referenced[name] {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		if now.Sub(info.ModTime()) <= olderThan {
			continue
		}
		if rerr := os.RemoveAll(s.Dir(name)); rerr != nil {
			if firstErr == nil {
				firstErr = rerr
			}
			continue
		}
		removed++
	}
	return removed, firstErr
}
