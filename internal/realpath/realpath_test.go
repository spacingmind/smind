package realpath

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/spacingmind/smind/internal/testutil"
)

// canon resolves dir itself, so expectations compare like for like (the
// temp dir may sit behind a link or an 8.3 name).
func canon(t *testing.T, dir string) string {
	t.Helper()
	r, err := Resolve(dir)
	if err != nil {
		t.Fatalf("Resolve(%s): %v", dir, err)
	}
	return r
}

// A path through a directory link (a junction on Windows without symlink
// privilege) resolves to the link's target -- the case EvalSymlinks fails
// on for junctions.
func TestResolve_FollowsDirectoryLink(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	kind := testutil.Link(t, outside, filepath.Join(root, "evil"))

	got, err := Resolve(filepath.Join(root, "evil", "secret.txt"))
	if err != nil {
		t.Fatalf("Resolve through %s: %v", kind, err)
	}
	if want := filepath.Join(canon(t, outside), "secret.txt"); got != want {
		t.Fatalf("Resolve through %s = %q, want %q", kind, got, want)
	}
}

func TestResolve_PlainPathIsUnchanged(t *testing.T) {
	dir := canon(t, t.TempDir())
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := Resolve(filepath.Join(dir, "a.txt")); err != nil || got != filepath.Join(dir, "a.txt") {
		t.Fatalf("Resolve = %q, %v; want %q", got, err, filepath.Join(dir, "a.txt"))
	}
}

func TestResolve_MissingIsErrNotExist(t *testing.T) {
	_, err := Resolve(filepath.Join(t.TempDir(), "nope", "missing.txt"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Resolve(missing) error = %v, want fs.ErrNotExist", err)
	}
}
