package hostfs

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// newTestRoot creates a real temp directory with a small tree inside it: a
// regular file, a plain subdirectory, a subdirectory that's a git repo (has
// a .git entry), a dot-prefixed subdirectory (must be excluded), and a
// dot-prefixed file (must also be excluded).
func newTestRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write hello.txt: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "plain"), 0o755); err != nil {
		t.Fatalf("mkdir plain: %v", err)
	}
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir repo/.git: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, ".hidden"), 0o755); err != nil {
		t.Fatalf("mkdir .hidden: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".hidden.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write .hidden.txt: %v", err)
	}
	return root
}

func TestList_OnlySubdirectoriesExcludingFiles(t *testing.T) {
	root := newTestRoot(t)

	result, err := List(root)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	names := entryNames(result.Entries)
	if len(names) != 2 || names[0] != "plain" || names[1] != "repo" {
		t.Fatalf("entries = %+v, want [plain repo]", names)
	}
}

func TestList_ExcludesDotPrefixedEntries(t *testing.T) {
	root := newTestRoot(t)

	result, err := List(root)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	for _, e := range result.Entries {
		if e.Name == ".hidden" {
			t.Fatalf("entries = %+v, want .hidden excluded", result.Entries)
		}
	}
}

func TestList_FlagsGitRepo(t *testing.T) {
	root := newTestRoot(t)

	result, err := List(root)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	byName := entryByName(t, result.Entries)
	if !byName["repo"].IsGitRepo {
		t.Errorf("repo.IsGitRepo = false, want true")
	}
	if byName["plain"].IsGitRepo {
		t.Errorf("plain.IsGitRepo = true, want false")
	}
}

func TestList_FollowsSymlinkedDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require elevated privileges on windows")
	}
	root := newTestRoot(t)
	target := filepath.Join(root, "plain")
	link := filepath.Join(root, "linked")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	result, err := List(root)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	byName := entryByName(t, result.Entries)
	if _, ok := byName["linked"]; !ok {
		t.Fatalf("entries = %+v, want a %q entry (symlinked directory)", result.Entries, "linked")
	}
}

func TestList_EmptyPathDefaultsToHomeDir(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	t.Setenv("HOME", home)

	result, err := List("")
	if err != nil {
		t.Fatalf("List(\"\") error = %v", err)
	}
	wantHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatalf("EvalSymlinks(home) error = %v", err)
	}
	gotPath, err := filepath.EvalSymlinks(result.Path)
	if err != nil {
		t.Fatalf("EvalSymlinks(result.Path) error = %v", err)
	}
	if gotPath != wantHome {
		t.Fatalf("List(\"\").Path = %q, want %q", result.Path, home)
	}
	names := entryNames(result.Entries)
	if len(names) != 1 || names[0] != "sub" {
		t.Fatalf("entries = %+v, want [sub]", names)
	}
}

func TestList_ParentPath(t *testing.T) {
	root := newTestRoot(t)
	sub := filepath.Join(root, "plain")

	result, err := List(sub)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	wantParent, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("Abs() error = %v", err)
	}
	if result.Parent != filepath.Clean(wantParent) {
		t.Fatalf("Parent = %q, want %q", result.Parent, wantParent)
	}
}

func TestList_ParentIsEmptyAtFilesystemRoot(t *testing.T) {
	result, err := List("/")
	if err != nil {
		t.Fatalf("List(\"/\") error = %v", err)
	}
	if result.Parent != "" {
		t.Fatalf("Parent = %q, want empty at the filesystem root", result.Parent)
	}
}

func TestList_DistinguishableErrors(t *testing.T) {
	root := newTestRoot(t)

	_, notExistErr := List(filepath.Join(root, "does-not-exist"))
	if notExistErr == nil {
		t.Fatal("List(nonexistent) error = nil, want an error")
	}
	if !errors.Is(notExistErr, fs.ErrNotExist) {
		t.Errorf("List(nonexistent) error = %v, want fs.ErrNotExist", notExistErr)
	}

	_, notDirErr := List(filepath.Join(root, "hello.txt"))
	if notDirErr == nil {
		t.Fatal("List(file) error = nil, want an error")
	}
	if !errors.Is(notDirErr, ErrNotDirectory) {
		t.Errorf("List(file) error = %v, want ErrNotDirectory", notDirErr)
	}

	if errors.Is(notExistErr, ErrNotDirectory) || errors.Is(notDirErr, fs.ErrNotExist) {
		t.Fatalf("nonexistent-path and not-a-directory errors must be distinguishable: %v vs %v", notExistErr, notDirErr)
	}

	if os.Geteuid() == 0 {
		t.Skip("running as root: permission checks are bypassed, skipping the permission-denied case")
	}
	if runtime.GOOS == "windows" {
		t.Skip("permission bits don't work the same way on windows")
	}
	denied := filepath.Join(root, "denied")
	if err := os.Mkdir(denied, 0o000); err != nil {
		t.Fatalf("mkdir denied: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(denied, 0o755) })

	_, permErr := List(denied)
	if permErr == nil {
		t.Fatal("List(permission-denied dir) error = nil, want an error")
	}
	if !errors.Is(permErr, fs.ErrPermission) {
		t.Errorf("List(permission-denied dir) error = %v, want fs.ErrPermission", permErr)
	}
	if errors.Is(permErr, fs.ErrNotExist) || errors.Is(permErr, ErrNotDirectory) {
		t.Fatalf("permission-denied error must be distinguishable from the other two: %v", permErr)
	}
}

func entryNames(entries []Entry) []string {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	return names
}

func entryByName(t *testing.T, entries []Entry) map[string]Entry {
	t.Helper()
	m := make(map[string]Entry, len(entries))
	for _, e := range entries {
		m[e.Name] = e
	}
	return m
}
