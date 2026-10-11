package config

import (
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
)

const (
	dirName       = ".smind"
	legacyDirName = ".spacingmind"
)

// Dir returns the smind home directory, without creating anything.
// Resolution order: $SMIND_HOME if non-empty; else ~/.smind if anything
// exists there (Lstat, so a user's own symlink counts); else ~/.spacingmind
// if it is a directory (legacy install, not yet migrated or migration
// failed); else ~/.smind.
func Dir() string {
	if v := os.Getenv("SMIND_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return dirName
	}
	smind := filepath.Join(home, dirName)
	if _, err := os.Lstat(smind); err == nil {
		return smind
	}
	legacy := filepath.Join(home, legacyDirName)
	if fi, err := os.Stat(legacy); err == nil && fi.IsDir() {
		return legacy
	}
	return smind
}

// Path returns the config file location inside Dir().
func Path() string {
	return filepath.Join(Dir(), "config.yaml")
}

// migrationDeps carries the injectable operations of migrateHome so tests
// can exercise every branch deterministically.
type migrationDeps struct {
	rename  func(old, new string) error
	symlink func(old, new string) error
	logf    func(format string, args ...any)
	goos    string
}

// migrateHome performs the one-time ~/.spacingmind -> ~/.smind move. It
// never returns an error and never deletes or copies anything: rename
// cannot replace a non-empty directory, so no failure mode loses data.
func migrateHome(home string, d migrationDeps) {
	smind := filepath.Join(home, dirName)
	legacy := filepath.Join(home, legacyDirName)

	if _, err := os.Lstat(smind); err == nil {
		if fi, err := os.Stat(legacy); err == nil && fi.IsDir() {
			if sameDir(legacy, smind) {
				return
			}
			d.logf("home: both %s and %s exist; using %s, leaving %s untouched and ignored", smind, legacy, smind, legacy)
		}
		return
	}

	if fi, err := os.Stat(legacy); err != nil || !fi.IsDir() {
		return
	}

	if err := d.rename(legacy, smind); err != nil {
		if _, lerr := os.Lstat(smind); lerr == nil {
			if _, lerr := os.Lstat(legacy); lerr != nil || sameDir(legacy, smind) {
				return
			}
		}
		if _, lerr := os.Lstat(smind); lerr == nil {
			// Lost a race to a process that created ~/.smind some other way.
			d.logf("home: both %s and %s exist; using %s, leaving %s untouched and ignored", smind, legacy, smind, legacy)
			return
		}
		d.logf("home: could not move %s to %s (%v); %s keeps being used", legacy, smind, err, legacy)
		return
	}

	if d.goos == "windows" {
		d.logf("home: moved %s to %s (no compat symlink on windows; tools using %s will not find it)", legacy, smind, legacy)
		return
	}
	if err := d.symlink(".smind", legacy); err != nil && !errors.Is(err, fs.ErrExist) {
		d.logf("home: moved %s to %s (compat symlink not created: %v; tools using %s will not find it)", legacy, smind, err, legacy)
		return
	}
	d.logf("home: moved %s to %s (compat symlink left at %s)", legacy, smind, legacy)
}

// sameDir reports whether two paths resolve to the same existing directory.
func sameDir(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

// migrateIfDue runs the home migration with the real OS operations. It is
// a no-op when SMIND_HOME is set or the home directory cannot be resolved.
func migrateIfDue() {
	if os.Getenv("SMIND_HOME") != "" {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	migrateHome(home, migrationDeps{
		rename:  os.Rename,
		symlink: os.Symlink,
		logf:    log.Printf,
		goos:    runtime.GOOS,
	})
}
