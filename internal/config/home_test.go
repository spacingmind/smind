package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"
)

// testHome points HOME/USERPROFILE at a temp dir, unsets SMIND_HOME, and
// returns the home plus the legacy and new dir paths. Tests never touch the
// real home. No t.Parallel (Setenv).
func testHome(t *testing.T) (home, legacy, smind string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SMIND_HOME", "")
	return home, filepath.Join(home, legacyDirName), filepath.Join(home, dirName)
}

// writeLegacyTree creates ~/.spacingmind with nested content.
func writeLegacyTree(t *testing.T, legacy string) {
	t.Helper()
	for p, body := range map[string]string{
		"config.yaml": "server:\n  port: 4648\n",
		"token":       "secret-token",
		"nested/db":   "database",
	} {
		full := filepath.Join(legacy, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// captureLogs returns a logf that appends formatted lines to a buffer.
func captureLogs(buf *bytes.Buffer) func(string, ...any) {
	return func(format string, args ...any) {
		fmt.Fprintf(buf, format+"\n", args...)
	}
}

func realDeps(logf func(string, ...any)) migrationDeps {
	return migrationDeps{rename: os.Rename, symlink: os.Symlink, logf: logf, goos: runtime.GOOS}
}

func assertMigratedState(t *testing.T, legacy, smind string) {
	t.Helper()
	for p, want := range map[string]string{
		"config.yaml": "server:\n  port: 4648\n",
		"token":       "secret-token",
		"nested/db":   "database",
	} {
		got, err := os.ReadFile(filepath.Join(smind, filepath.FromSlash(p)))
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		if string(got) != want {
			t.Fatalf("%s = %q, want %q", p, got, want)
		}
	}
	fi, err := os.Lstat(legacy)
	if err != nil {
		t.Fatalf("Lstat legacy: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("legacy %s is not a symlink (mode %v)", legacy, fi.Mode())
	}
	if got, err := os.Readlink(legacy); err != nil || got != ".smind" {
		t.Fatalf("legacy link target = %q, err %v, want %q", got, err, ".smind")
	}
}

func TestHome_DefaultIsDotSmind(t *testing.T) {
	_, _, smind := testHome(t)
	if got := Dir(); got != smind {
		t.Fatalf("Dir() = %q, want %q", got, smind)
	}
	if _, err := os.Lstat(smind); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Dir() created something: Lstat err = %v, want not-exist", err)
	}
}

func TestHome_SmindHomeOverride(t *testing.T) {
	home, legacy, _ := testHome(t)
	writeLegacyTree(t, legacy)
	other := filepath.Join(home, "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SMIND_HOME", other)

	if got := Dir(); got != other {
		t.Fatalf("Dir() = %q, want %q", got, other)
	}
	if _, err := Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if fi, err := os.Stat(legacy); err != nil || !fi.IsDir() {
		t.Fatalf("legacy changed after Load with SMIND_HOME set: stat err %v, isDir %v", err, fi.IsDir())
	}
	if _, err := os.Lstat(filepath.Join(home, dirName)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("~/%s created despite SMIND_HOME override", dirName)
	}
}

func TestHome_MigratesLegacyDirWithSymlink(t *testing.T) {
	_, legacy, smind := testHome(t)
	writeLegacyTree(t, legacy)

	if _, err := Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if runtime.GOOS != "windows" {
		assertMigratedState(t, legacy, smind)
	}
	if got := Dir(); got != smind {
		t.Fatalf("Dir() = %q, want %q", got, smind)
	}

	// Log assertions use migrateHome directly on a fresh tree.
	home2 := t.TempDir()
	legacy2 := filepath.Join(home2, legacyDirName)
	smind2 := filepath.Join(home2, dirName)
	writeLegacyTree(t, legacy2)
	var buf bytes.Buffer
	d := realDeps(captureLogs(&buf))
	migrateHome(home2, d)
	if runtime.GOOS != "windows" {
		assertMigratedState(t, legacy2, smind2)
	}
	if n := bytes.Count(buf.Bytes(), []byte("moved")); n != 1 {
		t.Fatalf("first migrateHome logged %d lines mentioning moved, want 1:\n%s", n, buf.String())
	}
	buf.Reset()
	migrateHome(home2, d)
	if buf.Len() != 0 {
		t.Fatalf("second migrateHome logged %q, want silence", buf.String())
	}
}

func TestHome_BothExistUsesSmindAndWarns(t *testing.T) {
	_, legacy, smind := testHome(t)
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(smind, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "legacy-marker"), []byte("l"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(smind, "smind-marker"), []byte("s"), 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	migrateHome(filepath.Dir(smind), realDeps(captureLogs(&buf)))
	if got := buf.String(); got == "" {
		t.Fatal("both-exists state produced no warning")
	}
	if _, err := os.ReadFile(filepath.Join(legacy, "legacy-marker")); err != nil {
		t.Fatalf("legacy tree changed: %v", err)
	}
	if _, err := os.ReadFile(filepath.Join(smind, "smind-marker")); err != nil {
		t.Fatalf("smind tree changed: %v", err)
	}
	if got := Dir(); got != smind {
		t.Fatalf("Dir() = %q, want %q", got, smind)
	}

	// Normal post-migration state (legacy is the compat symlink) is silent.
	home2 := t.TempDir()
	legacy2 := filepath.Join(home2, legacyDirName)
	writeLegacyTree(t, legacy2)
	if err := migrateTo(t, home2); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	migrateHome(home2, realDeps(captureLogs(&buf)))
	if buf.Len() != 0 {
		t.Fatalf("compat-symlink state logged %q, want silence", buf.String())
	}
}

// migrateTo performs one real migration of home.
func migrateTo(t *testing.T, home string) error {
	t.Helper()
	legacy := filepath.Join(home, legacyDirName)
	smind := filepath.Join(home, dirName)
	if err := os.Rename(legacy, smind); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		return os.Symlink(".smind", legacy)
	}
	return nil
}

func TestHome_RenameFailureKeepsLegacy(t *testing.T) {
	_, legacy, smind := testHome(t)
	writeLegacyTree(t, legacy)

	var buf bytes.Buffer
	d := migrationDeps{
		rename: func(old, new string) error {
			return &os.LinkError{Op: "rename", Old: old, New: new, Err: syscall.EXDEV}
		},
		symlink: os.Symlink,
		logf:    captureLogs(&buf),
		goos:    runtime.GOOS,
	}
	home := filepath.Dir(legacy)
	migrateHome(home, d)
	if buf.Len() == 0 {
		t.Fatal("rename failure produced no warning")
	}
	if _, err := os.ReadFile(filepath.Join(legacy, "token")); err != nil {
		t.Fatalf("legacy content lost: %v", err)
	}
	if _, err := os.Lstat(smind); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("~/%s exists after failed rename", dirName)
	}
	if got := Dir(); got != legacy {
		t.Fatalf("Dir() = %q, want legacy %q", got, legacy)
	}

	t.Setenv("SMIND_HOME", "")
	if _, err := Load(); err != nil {
		t.Fatalf("Load() = %v, want nil despite migration failure", err)
	}
}

func TestHome_ConcurrentMigrationSafe(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, legacyDirName)
	smind := filepath.Join(home, dirName)
	writeLegacyTree(t, legacy)

	var mu sync.Mutex
	var buf bytes.Buffer
	logf := func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		captureLogs(&buf)(format, args...)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			migrateHome(home, realDeps(logf))
		}()
	}
	close(start)
	wg.Wait()

	if runtime.GOOS != "windows" {
		assertMigratedState(t, legacy, smind)
	}
	if n := bytes.Count(buf.Bytes(), []byte("moved")); n != 1 {
		t.Fatalf("concurrent migration logged %d moved lines, want 1:\n%s", n, buf.String())
	}

	// Deterministic variant: winner performs the real rename; loser's
	// rename returns a spurious error and must be treated as success by
	// the re-check.
	home2 := t.TempDir()
	legacy2 := filepath.Join(home2, legacyDirName)
	writeLegacyTree(t, legacy2)
	var done sync.Once
	renamed := make(chan struct{})
	d2 := migrationDeps{
		rename: func(old, new string) error {
			var err error
			var mine bool
			done.Do(func() {
				mine = true
				err = os.Rename(old, new)
				close(renamed)
			})
			if mine {
				return err
			}
			<-renamed
			return &os.LinkError{Op: "rename", Old: old, New: new, Err: syscall.ENOENT}
		},
		symlink: os.Symlink,
		logf:    logf,
		goos:    runtime.GOOS,
	}
	buf.Reset()
	var wg2 sync.WaitGroup
	for range 2 {
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			migrateHome(home2, d2)
		}()
	}
	wg2.Wait()
	if runtime.GOOS != "windows" {
		if _, err := os.ReadFile(filepath.Join(filepath.Join(home2, dirName), "token")); err != nil {
			t.Fatalf("loser clobbered migration: %v", err)
		}
	}
	if n := bytes.Count(buf.Bytes(), []byte("moved")); n != 1 {
		t.Fatalf("deterministic variant logged %d moved lines, want 1:\n%s", n, buf.String())
	}
}

func TestHome_SymlinkFailureStillMigrates(t *testing.T) {
	_, legacy, smind := testHome(t)
	writeLegacyTree(t, legacy)

	var buf bytes.Buffer
	d := realDeps(captureLogs(&buf))
	d.symlink = func(old, new string) error {
		return &os.LinkError{Op: "symlink", Old: old, New: new, Err: syscall.EPERM}
	}
	migrateHome(filepath.Dir(legacy), d)

	if got, err := os.ReadFile(filepath.Join(smind, "token")); err != nil || string(got) != "secret-token" {
		t.Fatalf("content not migrated: %q, %v", got, err)
	}
	if n := bytes.Count(buf.Bytes(), []byte("compat symlink not created")); n != 1 {
		t.Fatalf("want one symlink warning, got:\n%s", buf.String())
	}
	if got := Dir(); got != smind {
		t.Fatalf("Dir() = %q, want %q", got, smind)
	}
}

func TestHome_WindowsSkipsSymlink(t *testing.T) {
	_, legacy, smind := testHome(t)
	writeLegacyTree(t, legacy)

	var buf bytes.Buffer
	d := realDeps(captureLogs(&buf))
	d.goos = "windows"
	d.symlink = func(old, new string) error { t.Fatal("symlink attempted on windows"); return nil }
	migrateHome(filepath.Dir(legacy), d)

	if _, err := os.Stat(filepath.Join(smind, "token")); err != nil {
		t.Fatalf("content not migrated: %v", err)
	}
	if _, err := os.Lstat(legacy); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("legacy path should be gone without a symlink, Lstat err = %v", err)
	}
	if n := bytes.Count(buf.Bytes(), []byte("no compat symlink on windows")); n != 1 {
		t.Fatalf("want one windows warning, got:\n%s", buf.String())
	}
}
