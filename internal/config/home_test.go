package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// testHome points HOME/USERPROFILE at a temp dir and unsets SMIND_HOME.
// Tests never touch the real home. No t.Parallel (Setenv).
func testHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SMIND_HOME", "")
	return home
}

func TestHome_DefaultIsDotSmind(t *testing.T) {
	smind := filepath.Join(testHome(t), dirName)
	if got := Dir(); got != smind {
		t.Fatalf("Dir() = %q, want %q", got, smind)
	}
	if _, err := os.Lstat(smind); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Dir() created something: Lstat err = %v, want not-exist", err)
	}
}

func TestHome_SmindHomeOverride(t *testing.T) {
	home := testHome(t)
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
	if _, err := os.Lstat(filepath.Join(home, dirName)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("~/%s created despite SMIND_HOME override", dirName)
	}
}

// The pre-rename ~/.spacingmind is not read, moved or linked: smind never
// shipped to users under that name.
func TestHome_IgnoresOldSpacingmindDir(t *testing.T) {
	home := testHome(t)
	old := filepath.Join(home, ".spacingmind")
	if err := os.Mkdir(old, 0o700); err != nil {
		t.Fatal(err)
	}
	if got, want := Dir(), filepath.Join(home, dirName); got != want {
		t.Fatalf("Dir() = %q, want %q", got, want)
	}
	if _, err := Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if fi, err := os.Lstat(old); err != nil || !fi.IsDir() {
		t.Fatalf("~/.spacingmind changed by Load: %v", err)
	}
}
