package files

import (
	"os"
	"path/filepath"
	"testing"
)

// newTestRoot creates a real temp directory to use as a sandbox root, with
// a small tree inside it: a file at the root, a subdirectory with a file,
// and an empty subdirectory. Returns the root path.
func newTestRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write hello.txt: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "nested.txt"), []byte("nested\n"), 0o644); err != nil {
		t.Fatalf("write sub/nested.txt: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatalf("mkdir empty: %v", err)
	}
	return root
}

func TestList_Root(t *testing.T) {
	root := newTestRoot(t)

	entries, err := List(root, "")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("List() = %+v, want 3 entries", entries)
	}
	// Sorted by name: empty, hello.txt, sub.
	if entries[0].Name != "empty" || !entries[0].IsDir {
		t.Errorf("entries[0] = %+v, want dir %q", entries[0], "empty")
	}
	if entries[1].Name != "hello.txt" || entries[1].IsDir || entries[1].Size != 6 {
		t.Errorf("entries[1] = %+v, want file %q size 6", entries[1], "hello.txt")
	}
	if entries[2].Name != "sub" || !entries[2].IsDir {
		t.Errorf("entries[2] = %+v, want dir %q", entries[2], "sub")
	}
}

func TestList_Subdirectory(t *testing.T) {
	root := newTestRoot(t)

	entries, err := List(root, "sub")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "nested.txt" {
		t.Fatalf("List(sub) = %+v, want [nested.txt]", entries)
	}
}

func TestList_PathIsAFile(t *testing.T) {
	root := newTestRoot(t)

	if _, err := List(root, "hello.txt"); err == nil {
		t.Fatal("List(hello.txt) error = nil, want an error (not a directory)")
	}
}

func TestRead_ExistingFile(t *testing.T) {
	root := newTestRoot(t)

	content, err := Read(root, "sub/nested.txt")
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if content != "nested\n" {
		t.Fatalf("Read() = %q, want %q", content, "nested\n")
	}
}

func TestRead_Directory(t *testing.T) {
	root := newTestRoot(t)

	if _, err := Read(root, "sub"); err == nil {
		t.Fatal("Read(sub) error = nil, want an error (is a directory)")
	}
}

func TestRead_NonUTF8_Rejected(t *testing.T) {
	root := newTestRoot(t)
	binPath := filepath.Join(root, "bin.dat")
	if err := os.WriteFile(binPath, []byte{0xff, 0xfe, 0x00, 0x01}, 0o644); err != nil {
		t.Fatalf("write bin.dat: %v", err)
	}

	if _, err := Read(root, "bin.dat"); err == nil {
		t.Fatal("Read(bin.dat) error = nil, want an error (not valid UTF-8)")
	}
}

func TestWrite_CreatesNewFile(t *testing.T) {
	root := newTestRoot(t)

	if err := Write(root, "new.txt", "fresh content"); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	content, err := Read(root, "new.txt")
	if err != nil {
		t.Fatalf("Read() after Write() error = %v", err)
	}
	if content != "fresh content" {
		t.Fatalf("content = %q, want %q", content, "fresh content")
	}
}

func TestWrite_OverwritesExistingFile(t *testing.T) {
	root := newTestRoot(t)

	if err := Write(root, "hello.txt", "overwritten"); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	content, err := Read(root, "hello.txt")
	if err != nil {
		t.Fatalf("Read() after Write() error = %v", err)
	}
	if content != "overwritten" {
		t.Fatalf("content = %q, want %q", content, "overwritten")
	}
}

func TestWrite_TargetIsDirectory(t *testing.T) {
	root := newTestRoot(t)

	if err := Write(root, "sub", "oops"); err == nil {
		t.Fatal("Write(sub) error = nil, want an error (is a directory)")
	}
}

func TestWrite_NestedInMissingParentDir_Errors(t *testing.T) {
	root := newTestRoot(t)

	if err := Write(root, "nosuchdir/file.txt", "x"); err == nil {
		t.Fatal("Write(nosuchdir/file.txt) error = nil, want an error (parent directory doesn't exist)")
	}
}

// --- Adversarial path-sandboxing tests ---

func TestSandbox_DotDotTraversal_Rejected(t *testing.T) {
	root := newTestRoot(t)

	// A sibling file outside root, at the same level as root itself.
	outsideDir := filepath.Dir(root)
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("top secret"), 0o644); err != nil {
		t.Fatalf("write secret.txt: %v", err)
	}
	t.Cleanup(func() { os.Remove(outsideFile) })

	traversal := "../" + filepath.Base(outsideFile)
	if _, err := Read(root, traversal); err == nil {
		t.Fatalf("Read(%q) error = nil, want a rejection (escapes worktree root)", traversal)
	}
	if _, err := List(root, "../"); err == nil {
		t.Fatal(`List("../") error = nil, want a rejection (escapes worktree root)`)
	}
	if err := Write(root, traversal, "pwned"); err == nil {
		t.Fatalf("Write(%q) error = nil, want a rejection (escapes worktree root)", traversal)
	}
	// Confirm the traversal write didn't actually land.
	data, err := os.ReadFile(outsideFile)
	if err != nil {
		t.Fatalf("re-read outside file: %v", err)
	}
	if string(data) != "top secret" {
		t.Fatalf("outside file content = %q, want unchanged %q (a sandbox-escaping write must not succeed)", data, "top secret")
	}
}

func TestSandbox_DeepDotDotTraversal_Rejected(t *testing.T) {
	root := newTestRoot(t)

	if _, err := Read(root, "../../../../../../../../etc/passwd"); err == nil {
		t.Fatal("Read(deep ../ traversal to /etc/passwd) error = nil, want a rejection")
	}
}

func TestSandbox_AbsolutePathOutsideRoot_Rejected(t *testing.T) {
	root := newTestRoot(t)

	if _, err := Read(root, "/etc/passwd"); err == nil {
		t.Fatal("Read(/etc/passwd) error = nil, want a rejection (absolute path escapes root)")
	}
	if _, err := List(root, "/etc"); err == nil {
		t.Fatal("List(/etc) error = nil, want a rejection (absolute path escapes root)")
	}
	if err := Write(root, "/tmp/smind-sandbox-escape-test.txt", "pwned"); err == nil {
		t.Fatal("Write(/tmp/...) error = nil, want a rejection (absolute path escapes root)")
	}
}

func TestSandbox_AbsolutePathInsideRoot_Allowed(t *testing.T) {
	root := newTestRoot(t)

	abs := filepath.Join(root, "hello.txt")
	content, err := Read(root, abs)
	if err != nil {
		t.Fatalf("Read(absolute path inside root) error = %v, want success", err)
	}
	if content != "hello\n" {
		t.Fatalf("content = %q, want %q", content, "hello\n")
	}
}

// TestSandbox_SymlinkToOutsideFile_Rejected plants a symlink inside root
// that points at a file outside root, and proves Read follows it and
// rejects the result rather than trusting the textual (pre-resolution)
// in-root-looking path.
func TestSandbox_SymlinkToOutsideFile_Rejected(t *testing.T) {
	root := newTestRoot(t)

	outsideDir := t.TempDir()
	secretPath := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(secretPath, []byte("top secret"), 0o644); err != nil {
		t.Fatalf("write secret: %v", err)
	}

	linkPath := filepath.Join(root, "evil-link")
	if err := os.Symlink(secretPath, linkPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if content, err := Read(root, "evil-link"); err == nil {
		t.Fatalf("Read(evil-link) = %q, err = nil, want a rejection (symlink escapes root)", content)
	}
}

// TestSandbox_SymlinkToOutsideDirectory_Rejected plants a symlinked
// directory inside root pointing outside it, and proves both List (an
// existing target) and Write (a new file *through* the symlinked dir, whose
// leaf doesn't exist yet) are rejected.
func TestSandbox_SymlinkToOutsideDirectory_Rejected(t *testing.T) {
	root := newTestRoot(t)

	outsideDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outsideDir, "existing.txt"), []byte("secret dir listing"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	linkDir := filepath.Join(root, "evil-dir")
	if err := os.Symlink(outsideDir, linkDir); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if _, err := List(root, "evil-dir"); err == nil {
		t.Fatal("List(evil-dir) error = nil, want a rejection (symlinked directory escapes root)")
	}

	// The new file's leaf ("newfile.txt") doesn't exist yet -- this
	// exercises resolveExistingPrefix's missing-leaf path, proving the
	// symlinked *ancestor* is still caught even though the full candidate
	// path itself doesn't exist for EvalSymlinks to reject outright.
	if err := Write(root, "evil-dir/newfile.txt", "pwned"); err == nil {
		t.Fatal("Write(evil-dir/newfile.txt) error = nil, want a rejection (symlinked ancestor escapes root)")
	}
	if _, err := os.Stat(filepath.Join(outsideDir, "newfile.txt")); err == nil {
		t.Fatal("newfile.txt was created outside root -- sandbox escape actually succeeded")
	}
}

// TestSandbox_SiblingDirectorySharingPrefix_NotTreatedAsContained proves
// withinRoot's containment check is separator-aware: a sibling directory
// whose name merely has root's name as a string prefix (e.g. root "foo" and
// sibling "foo-evil") must not be treated as being inside root.
func TestSandbox_SiblingDirectorySharingPrefix_NotTreatedAsContained(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "foo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	sibling := filepath.Join(parent, "foo-evil")
	if err := os.Mkdir(sibling, 0o755); err != nil {
		t.Fatalf("mkdir sibling: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sibling, "secret.txt"), []byte("nope"), 0o644); err != nil {
		t.Fatalf("write sibling file: %v", err)
	}

	// Absolute path straight at the sibling: must be rejected, not allowed
	// just because its string form starts with root's string form.
	if _, err := Read(root, filepath.Join(sibling, "secret.txt")); err == nil {
		t.Fatal("Read(sibling absolute path) error = nil, want a rejection")
	}
}

func TestSandbox_RootItself_Allowed(t *testing.T) {
	root := newTestRoot(t)

	if _, err := List(root, ""); err != nil {
		t.Fatalf(`List("") error = %v, want success (root itself)`, err)
	}
	if _, err := List(root, "."); err != nil {
		t.Fatalf(`List(".") error = %v, want success (root itself)`, err)
	}
}
