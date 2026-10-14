package selfupdate

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// RenameAside reports whether this platform must move a running executable
// out of the way instead of renaming over it. Windows refuses to overwrite or
// delete a running .exe but does allow renaming it.
var RenameAside = runtime.GOOS == "windows"

// OldPath is where Swap keeps the previous binary.
func OldPath(exe string) string { return exe + ".old" }

// failedPath is where Restore parks a rejected new binary on rename-aside
// platforms, where it may still be running and so cannot be deleted.
func failedPath(exe string) string { return exe + ".failed" }

// Swap installs staged (a complete executable in the same directory as exe)
// as exe and keeps the previous binary at OldPath(exe).
//
// Unix: the old binary is hard-linked (or, on filesystems without links,
// copied) to the .old name, then staged is renamed over exe. rename(2) is
// atomic, so exe is at every instant either the old or the new binary and a
// running process keeps executing its already-open inode.
//
// Rename-aside (Windows): exe is renamed to .old (allowed while it runs) and
// staged is renamed into its place; if the second rename fails the first is
// undone. There is a brief window with no exe, which is why Windows cannot
// offer the unix guarantee.
//
// On any error exe is unchanged and no partial file is left at OldPath.
func Swap(exe, staged string, aside bool) error {
	old := OldPath(exe)
	if err := removeIfExists(old); err != nil {
		return fmt.Errorf("remove the previous %s: %w (is a daemon started before the last update still running? stop it and retry)", filepath.Base(old), err)
	}
	if aside {
		if err := os.Rename(exe, old); err != nil {
			return fmt.Errorf("move %s aside: %w", filepath.Base(exe), err)
		}
		if err := os.Rename(staged, exe); err != nil {
			if rerr := os.Rename(old, exe); rerr != nil {
				return fmt.Errorf("install new %s: %w (and restoring the old one failed: %v; it is at %s)", filepath.Base(exe), err, rerr, old)
			}
			return fmt.Errorf("install new %s: %w", filepath.Base(exe), err)
		}
		return nil
	}
	if err := os.Link(exe, old); err != nil {
		if err := copyFile(exe, old); err != nil {
			_ = os.Remove(old)
			return fmt.Errorf("keep the old binary as %s: %w", filepath.Base(old), err)
		}
	}
	if err := os.Rename(staged, exe); err != nil {
		_ = os.Remove(old)
		return fmt.Errorf("install new %s: %w", filepath.Base(exe), err)
	}
	return nil
}

// Restore undoes a Swap: the .old binary becomes exe again.
func Restore(exe string, aside bool) error {
	old := OldPath(exe)
	if !aside {
		return os.Rename(old, exe)
	}
	failed := failedPath(exe)
	if err := removeIfExists(failed); err != nil {
		return fmt.Errorf("clear %s: %w", filepath.Base(failed), err)
	}
	if err := os.Rename(exe, failed); err != nil {
		return fmt.Errorf("move the new %s aside: %w", filepath.Base(exe), err)
	}
	if err := os.Rename(old, exe); err != nil {
		_ = os.Rename(failed, exe)
		return fmt.Errorf("put the old %s back: %w", filepath.Base(exe), err)
	}
	_ = os.Remove(failed) // best effort: it is locked if the new binary is still running
	return nil
}

// Discard drops the rollback copy after a successful update. Best effort: on
// rename-aside platforms the .old file is the still-running old binary and
// cannot be deleted yet; CleanupStale retries on the next start or update.
func Discard(exe string) { _ = os.Remove(OldPath(exe)) }

// CleanupStale removes leftovers of an earlier update (the .old and .failed
// files) that could not be deleted while that binary was running. Errors are
// ignored: a file still in use simply stays until the next call.
func CleanupStale(exe string) {
	_ = os.Remove(OldPath(exe))
	_ = os.Remove(failedPath(exe))
}

func removeIfExists(p string) error {
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	fi, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fi.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
