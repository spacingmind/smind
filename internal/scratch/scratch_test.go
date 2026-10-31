package scratch

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRemove_OnlyTaskDir(t *testing.T) {
	m := New(filepath.Join(t.TempDir(), "scratch"))
	for id := int64(1); id <= 2; id++ {
		if _, err := m.Ensure(id); err != nil {
			t.Fatalf("Ensure(%d) error = %v", id, err)
		}
	}
	if err := os.WriteFile(filepath.Join(m.Root(), "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Remove(1); err != nil {
		t.Fatalf("Remove(1) error = %v", err)
	}
	if _, err := os.Lstat(m.Dir(1)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("scratch/1 still exists after Remove (err = %v)", err)
	}
	for _, p := range []string{m.Dir(2), filepath.Join(m.Root(), "keep.txt")} {
		if _, err := os.Lstat(p); err != nil {
			t.Fatalf("Lstat(%s) error = %v, want entry untouched", p, err)
		}
	}
}

func TestRemove_SymlinkEscapeNotFollowed(t *testing.T) {
	outside := t.TempDir()
	target := filepath.Join(outside, "f.txt")
	if err := os.WriteFile(target, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(filepath.Join(t.TempDir(), "scratch"))
	if _, err := m.Ensure(1); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(m.Dir(1), "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Remove(1); err != nil {
		t.Fatalf("Remove(1) error = %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("symlink target deleted through the link: %v", err)
	}
	if _, err := os.Lstat(m.Dir(1)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("scratch/1 still exists after Remove (err = %v)", err)
	}
}

func TestRemove_SymlinkAtTaskDirRemovesLinkOnly(t *testing.T) {
	outside := t.TempDir()
	target := filepath.Join(outside, "f.txt")
	if err := os.WriteFile(target, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "scratch")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	m := New(root)
	if err := os.Symlink(outside, m.Dir(1)); err != nil {
		t.Fatal(err)
	}
	rem, err := m.Remove(1)
	if err != nil {
		t.Fatalf("Remove(1) error = %v", err)
	}
	if rem.Files != 1 {
		t.Fatalf("Remove(1) Files = %d, want 1 (the link itself)", rem.Files)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("symlink target deleted through the link: %v", err)
	}
	if _, err := os.Lstat(m.Dir(1)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("link still exists after Remove (err = %v)", err)
	}
}

func TestRemove_ReadOnlyFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("read-only dir modes are not enforced on windows")
	}
	m := New(filepath.Join(t.TempDir(), "scratch"))
	dirA := filepath.Join(m.Dir(1), "mod", "a")
	if err := os.MkdirAll(dirA, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dirA, "f.go")
	if err := os.WriteFile(file, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dirA, 0o555); err != nil {
		t.Fatal(err)
	}
	rem, err := m.Remove(1)
	if err != nil {
		t.Fatalf("Remove(1) error = %v", err)
	}
	if _, err := os.Lstat(m.Dir(1)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("scratch/1 still exists after Remove (err = %v)", err)
	}
	if rem.Files != 1 || rem.Bytes != int64(len("package a\n")) {
		t.Fatalf("Remove(1) = %d files, %d bytes; want 1 file, %d bytes", rem.Files, rem.Bytes, len("package a\n"))
	}
}

func TestRemove_BusyRefused(t *testing.T) {
	m := New(filepath.Join(t.TempDir(), "scratch"), WithBusy(func(int64) bool { return true }))
	if _, err := m.Ensure(1); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Remove(1); !errors.Is(err, ErrBusy) {
		t.Fatalf("Remove(1) error = %v, want ErrBusy", err)
	}
	if _, err := os.Lstat(m.Dir(1)); err != nil {
		t.Fatalf("busy task dir was deleted: %v", err)
	}
}

func TestSweep_RemovesClosedKeepsLive(t *testing.T) {
	m := New(filepath.Join(t.TempDir(), "scratch"))
	for id := int64(1); id <= 3; id++ {
		if _, err := m.Ensure(id); err != nil {
			t.Fatalf("Ensure(%d) error = %v", id, err)
		}
	}
	if _, err := m.Sweep(func(id int64) bool { return id == 2 }); err != nil {
		t.Fatalf("Sweep() error = %v", err)
	}
	if _, err := os.Lstat(m.Dir(2)); err != nil {
		t.Fatalf("live task dir 2 deleted by Sweep: %v", err)
	}
	for _, id := range []int64{1, 3} {
		if _, err := os.Lstat(m.Dir(id)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("closed task dir %d still exists after Sweep (err = %v)", id, err)
		}
	}
}

func TestSweep_IgnoresNonNumeric(t *testing.T) {
	m := New(filepath.Join(t.TempDir(), "scratch"))
	if _, err := m.Ensure(1); err != nil {
		t.Fatal(err)
	}
	nonTask := make([]string, 0, 4)
	for _, name := range []string{"notes", "01", "1abc", "-3"} {
		p := filepath.Join(m.Root(), name)
		if err := os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
		nonTask = append(nonTask, p)
	}
	if err := os.WriteFile(filepath.Join(m.Root(), "7"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Sweep(func(int64) bool { return false }); err != nil {
		t.Fatalf("Sweep() error = %v", err)
	}
	for _, p := range nonTask {
		if _, err := os.Lstat(p); err != nil {
			t.Fatalf("non-task entry %s touched by Sweep: %v", p, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(m.Root(), "7")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("numeric file 7 not removed (err = %v)", err)
	}
}

func TestSweep_SkipsBusyThenRetries(t *testing.T) {
	busy := true
	m := New(filepath.Join(t.TempDir(), "scratch"), WithBusy(func(int64) bool { return busy }))
	if _, err := m.Ensure(5); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Sweep(func(int64) bool { return false }); err != nil {
		t.Fatalf("Sweep() error = %v", err)
	}
	if _, err := os.Lstat(m.Dir(5)); err != nil {
		t.Fatalf("busy task dir 5 deleted by Sweep: %v", err)
	}
	busy = false
	if _, err := m.Sweep(func(int64) bool { return false }); err != nil {
		t.Fatalf("second Sweep() error = %v", err)
	}
	if _, err := os.Lstat(m.Dir(5)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("task dir 5 still exists after second Sweep (err = %v)", err)
	}
}

func TestUsage_Counts(t *testing.T) {
	m := New(filepath.Join(t.TempDir(), "scratch"))
	for id := int64(1); id <= 2; id++ {
		if _, err := m.Ensure(id); err != nil {
			t.Fatalf("Ensure(%d) error = %v", id, err)
		}
	}
	if err := os.WriteFile(filepath.Join(m.Dir(1), "a"), make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.Dir(2), "b"), make([]byte, 50), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := m.Usage(func(id int64) bool { return id == 1 })
	if err != nil {
		t.Fatalf("Usage() error = %v", err)
	}
	if u.Total.Files != 2 || u.Total.Bytes != 150 {
		t.Fatalf("Total = %+v, want 2 files / 150 bytes", u.Total)
	}
	if u.Dirs != 2 {
		t.Fatalf("Dirs = %d, want 2", u.Dirs)
	}
	if u.ReclaimableDirs != 1 {
		t.Fatalf("ReclaimableDirs = %d, want 1", u.ReclaimableDirs)
	}
	if u.Reclaimable.Files != 1 || u.Reclaimable.Bytes != 50 {
		t.Fatalf("Reclaimable = %+v, want 1 file / 50 bytes", u.Reclaimable)
	}
}

func TestEnsure_CreatesPrivateDir(t *testing.T) {
	m := New(filepath.Join(t.TempDir(), "scratch"))
	dir, err := m.Ensure(9)
	if err != nil {
		t.Fatalf("Ensure(9) error = %v", err)
	}
	if dir != m.Dir(9) {
		t.Fatalf("Ensure(9) = %q, want %q", dir, m.Dir(9))
	}
	if _, err := m.Ensure(9); err != nil {
		t.Fatalf("second Ensure(9) error = %v", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Lstat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("scratch/9 mode = %o, want 700", info.Mode().Perm())
		}
	}
	outside := t.TempDir()
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Ensure(9); err == nil {
		t.Fatal("Ensure(9) succeeded with a symlink in place, want error")
	}
}
