package files

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spacingmind/smind/internal/testutil"
)

func TestResolve_InsideRoot(t *testing.T) {
	root := newTestRoot(t)
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Resolve(root, "sub/nested.txt")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := filepath.Join(realRoot, "sub", "nested.txt"); got != want {
		t.Fatalf("Resolve = %q, want %q", got, want)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("resolved path does not exist: %v", err)
	}
}

func TestResolve_RejectsDotDotAndAbsoluteOutside(t *testing.T) {
	root := newTestRoot(t)
	outside := t.TempDir()
	for _, p := range []string{"../x", "sub/../../x", outside} {
		if got, err := Resolve(root, p); err == nil {
			t.Fatalf("Resolve(%q) = %q, want rejection", p, got)
		}
	}
}

func TestResolve_RejectsSymlinkEscape(t *testing.T) {
	root := newTestRoot(t)
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	testutil.Link(t, secret, filepath.Join(root, "evil"))
	testutil.Link(t, outside, filepath.Join(root, "evildir"))
	for _, p := range []string{"evil", "evildir/secret.txt"} {
		if got, err := Resolve(root, p); err == nil {
			t.Fatalf("Resolve(%q) = %q, want rejection", p, got)
		}
	}
}
