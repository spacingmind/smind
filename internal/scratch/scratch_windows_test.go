//go:build windows

package scratch

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// junction forces an NTFS junction (mklink /J) at newname -> target. Unlike
// testutil.Link it never creates a symlink, so the junction path is exercised
// even when Developer Mode makes symlinks available.
func junction(t *testing.T, target, newname string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", newname, target).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J %s %s: %v: %s", newname, target, err, out)
	}
}

// outsideTree builds a directory outside the scratch root holding a regular
// file and a read-only file in a subdirectory, and returns its path plus the
// two file paths.
func outsideTree(t *testing.T) (dir, file, roFile string) {
	t.Helper()
	dir = t.TempDir()
	file = filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(file, make([]byte, 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	roFile = filepath.Join(sub, "ro.txt")
	if err := os.WriteFile(roFile, make([]byte, 8192), 0o444); err != nil {
		t.Fatal(err)
	}
	return dir, file, roFile
}

func assertIntact(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("junction target entry %s was damaged: %v", p, err)
		}
	}
}

func TestWindows_Remove_JunctionInsideTaskDirNotFollowed(t *testing.T) {
	outside, file, roFile := outsideTree(t)
	m := New(filepath.Join(t.TempDir(), "scratch"))
	if _, err := m.Ensure(1); err != nil {
		t.Fatal(err)
	}
	junction(t, outside, filepath.Join(m.Dir(1), "escape"))
	if err := os.WriteFile(filepath.Join(m.Dir(1), "own.txt"), make([]byte, 10), 0o644); err != nil {
		t.Fatal(err)
	}

	rem, err := m.Remove(1)
	if err != nil {
		t.Fatalf("Remove(1) error = %v", err)
	}
	assertIntact(t, file, roFile)
	if _, err := os.Lstat(m.Dir(1)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("scratch/1 still exists after Remove (err = %v)", err)
	}
	// own.txt (10 bytes) plus the junction itself as one entry; the target's
	// 12288 bytes must not be counted or removed.
	if rem.Files != 2 || rem.Bytes != 10 {
		t.Fatalf("Remove(1) = %+v, want 2 files / 10 bytes", rem)
	}
}

func TestWindows_Remove_JunctionAtTaskDirRemovesLinkOnly(t *testing.T) {
	outside, file, roFile := outsideTree(t)
	root := filepath.Join(t.TempDir(), "scratch")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	m := New(root)
	junction(t, outside, m.Dir(1))

	rem, err := m.Remove(1)
	if err != nil {
		t.Fatalf("Remove(1) error = %v", err)
	}
	if rem.Files != 1 || rem.Bytes != 0 {
		t.Fatalf("Remove(1) = %+v, want 1 file / 0 bytes (the link itself)", rem)
	}
	assertIntact(t, file, roFile)
	if _, err := os.Lstat(m.Dir(1)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("junction still exists after Remove (err = %v)", err)
	}
}

func TestWindows_Remove_NestedJunctionChainNotFollowed(t *testing.T) {
	outside, file, roFile := outsideTree(t)
	m := New(filepath.Join(t.TempDir(), "scratch"))
	deep := filepath.Join(m.Dir(1), "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	junction(t, outside, filepath.Join(deep, "escape"))
	// A second junction pointing back into scratch/1 itself must not loop.
	junction(t, m.Dir(1), filepath.Join(deep, "loop"))

	if _, err := m.Remove(1); err != nil {
		t.Fatalf("Remove(1) error = %v", err)
	}
	assertIntact(t, file, roFile)
	if _, err := os.Lstat(m.Dir(1)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("scratch/1 still exists after Remove (err = %v)", err)
	}
}

func TestWindows_Remove_ReadOnlyFilesAndDirs(t *testing.T) {
	m := New(filepath.Join(t.TempDir(), "scratch"))
	dir := filepath.Join(m.Dir(1), "mod", "a")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "f.go")
	if err := os.WriteFile(file, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Go maps a missing owner-write bit to FILE_ATTRIBUTE_READONLY.
	for _, p := range []string{file, dir} {
		if err := os.Chmod(p, 0o444); err != nil {
			t.Fatal(err)
		}
	}
	rem, err := m.Remove(1)
	if err != nil {
		t.Fatalf("Remove(1) error = %v", err)
	}
	if _, err := os.Lstat(m.Dir(1)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("scratch/1 still exists after Remove (err = %v)", err)
	}
	if rem.Files != 1 || rem.Bytes != int64(len("package a\n")) {
		t.Fatalf("Remove(1) = %+v, want 1 file / %d bytes", rem, len("package a\n"))
	}
}

// longTree creates scratch/<id>/<deep nested path> whose absolute path is
// well beyond MAX_PATH (260) and writes one file at the bottom.
func longTree(t *testing.T, m *Manager, id int64) (file string) {
	t.Helper()
	dir := m.Dir(id)
	for len(dir) < 400 {
		dir = filepath.Join(dir, strings.Repeat("d", 40))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll long path (%d chars): %v", len(dir), err)
	}
	file = filepath.Join(dir, "deep.txt")
	if err := os.WriteFile(file, make([]byte, 77), 0o444); err != nil {
		t.Fatalf("WriteFile long path (%d chars): %v", len(file), err)
	}
	return file
}

func TestWindows_RemoveAndUsage_LongPaths(t *testing.T) {
	m := New(filepath.Join(t.TempDir(), "scratch"))
	file := longTree(t, m, 1)
	if len(file) <= 260 {
		t.Fatalf("test path is only %d chars", len(file))
	}
	u, err := m.Usage(nil)
	if err != nil {
		t.Fatalf("Usage() error = %v", err)
	}
	if u.Total.Files != 1 || u.Total.Bytes != 77 {
		t.Fatalf("Usage Total = %+v, want 1 file / 77 bytes", u.Total)
	}
	rem, err := m.Remove(1)
	if err != nil {
		t.Fatalf("Remove(1) error = %v", err)
	}
	if rem.Files != 1 || rem.Bytes != 77 {
		t.Fatalf("Remove(1) = %+v, want 1 file / 77 bytes", rem)
	}
	if _, err := os.Lstat(m.Dir(1)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("scratch/1 still exists after Remove (err = %v)", err)
	}
}

func TestWindows_Usage_JunctionTargetNotCounted(t *testing.T) {
	outside, _, _ := outsideTree(t) // 12288 bytes outside
	m := New(filepath.Join(t.TempDir(), "scratch"))
	if _, err := m.Ensure(1); err != nil {
		t.Fatal(err)
	}
	junction(t, outside, filepath.Join(m.Dir(1), "escape"))
	if err := os.WriteFile(filepath.Join(m.Dir(1), "own.txt"), make([]byte, 5), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := m.Usage(nil)
	if err != nil {
		t.Fatalf("Usage() error = %v", err)
	}
	if u.Total.Bytes != 5 || u.Total.Files != 2 {
		t.Fatalf("Usage Total = %+v, want 2 files (own + junction entry) / 5 bytes", u.Total)
	}
	if u.Reclaimable.Bytes != 5 {
		t.Fatalf("Reclaimable.Bytes = %d, want 5", u.Reclaimable.Bytes)
	}
}

func TestWindows_Usage_JunctionAtTaskDir(t *testing.T) {
	outside, _, _ := outsideTree(t)
	root := filepath.Join(t.TempDir(), "scratch")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	m := New(root)
	junction(t, outside, m.Dir(1))
	u, err := m.Usage(nil)
	if err != nil {
		t.Fatalf("Usage() error = %v", err)
	}
	if u.Total.Bytes != 0 {
		t.Fatalf("Usage Total.Bytes = %d, want 0 (junction target must not be counted)", u.Total.Bytes)
	}
}

func TestWindows_Sweep_JunctionTaskDirsKeepTargets(t *testing.T) {
	outside, file, roFile := outsideTree(t)
	root := filepath.Join(t.TempDir(), "scratch")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	m := New(root)
	junction(t, outside, m.Dir(1))
	if _, err := m.Ensure(2); err != nil {
		t.Fatal(err)
	}
	junction(t, outside, filepath.Join(m.Dir(2), "escape"))
	if _, err := m.Sweep(func(int64) bool { return false }); err != nil {
		t.Fatalf("Sweep() error = %v", err)
	}
	assertIntact(t, file, roFile)
	for _, id := range []int64{1, 2} {
		if _, err := os.Lstat(m.Dir(id)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("scratch/%d still exists after Sweep (err = %v)", id, err)
		}
	}
}

// A symlink to a file needs a privilege this account may lack; the test is
// skipped (never silently passed) in that case.
func TestWindows_Remove_FileSymlinkNotFollowed(t *testing.T) {
	outside, file, _ := outsideTree(t)
	_ = outside
	m := New(filepath.Join(t.TempDir(), "scratch"))
	if _, err := m.Ensure(1); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(m.Dir(1), "filelink")
	if err := os.Symlink(file, link); err != nil {
		var errno syscall.Errno
		if errors.As(err, &errno) && errno == syscall.Errno(1314) {
			t.Skipf("file symlink needs SeCreateSymbolicLinkPrivilege: %v", err)
		}
		t.Fatal(err)
	}
	if _, err := m.Remove(1); err != nil {
		t.Fatalf("Remove(1) error = %v", err)
	}
	assertIntact(t, file)
}
