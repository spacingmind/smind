package permpolicy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spacingmind/smind/internal/realpath"
	"github.com/spacingmind/smind/internal/testutil"
)

// canon resolves dir itself, so expectations compare like for like (on macOS
// a temp dir sits behind the /var -> /private/var link).
func canon(t *testing.T, dir string) string {
	t.Helper()
	r, err := realpath.Resolve(dir)
	if err != nil {
		t.Fatalf("realpath %s: %v", dir, err)
	}
	return r
}

func TestResolvePath_SymlinkResolved(t *testing.T) {
	root := canon(t, t.TempDir())
	outside := canon(t, t.TempDir())
	kind := testutil.Link(t, outside, filepath.Join(root, "link"))

	got, err := ResolvePath(filepath.Join(root, "link"), root)
	if err != nil {
		t.Fatalf("ResolvePath through %s: %v", kind, err)
	}
	if got != outside {
		t.Fatalf("ResolvePath through %s = %q, want %q", kind, got, outside)
	}
}

func TestResolvePath_DotDotCollapsed(t *testing.T) {
	dir := canon(t, t.TempDir())
	got, err := ResolvePath(filepath.Join(dir, "sub", "..", "file.txt"), dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "file.txt"); got != want {
		t.Fatalf("ResolvePath = %q, want %q", got, want)
	}
}

func TestResolvePath_MissingByDeepestParent(t *testing.T) {
	base := t.TempDir()
	a := filepath.Join(base, "a")
	if err := os.Mkdir(a, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := ResolvePath(filepath.Join(a, "b", "c", "new.txt"), base)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(canon(t, a), "b", "c", "new.txt"); got != want {
		t.Fatalf("ResolvePath = %q, want %q", got, want)
	}
}

func TestResolvePath_RelativeNeedsCwd(t *testing.T) {
	if _, err := ResolvePath("x/y.txt", ""); err == nil {
		t.Fatal("ResolvePath of a relative path with no cwd: want error")
	}
	dir := canon(t, t.TempDir())
	got, err := ResolvePath("x/y.txt", dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "x", "y.txt"); got != want {
		t.Fatalf("ResolvePath = %q, want %q", got, want)
	}
}

func TestResolvePath_HomeExpansion(t *testing.T) {
	skipPOSIXPathsOnWindows(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, p := range []string{"~/f.txt", "$HOME/f.txt", "${HOME}/f.txt"} {
		got, err := ResolvePath(p, "")
		if err != nil {
			t.Fatalf("ResolvePath(%q): %v", p, err)
		}
		if want := filepath.Join(canon(t, home), "f.txt"); got != want {
			t.Fatalf("ResolvePath(%q) = %q, want %q", p, got, want)
		}
	}
}

// A dangling link must resolve to its target, not to the link's own path:
// walking up from wt/link would place it inside wt while a write through it
// lands in the target directory.
func TestResolvePath_DanglingSymlinkFollowed(t *testing.T) {
	wt, outside := t.TempDir(), t.TempDir()
	kind := testutil.Link(t, filepath.Join(outside, "new.txt"), filepath.Join(wt, "link"))

	got, err := ResolvePath(filepath.Join(wt, "link"), wt)
	if err != nil {
		t.Fatalf("ResolvePath through dangling %s: %v", kind, err)
	}
	if want := filepath.Join(canon(t, outside), "new.txt"); got != want {
		t.Fatalf("ResolvePath through dangling %s = %q, want %q", kind, got, want)
	}
	if Within(got, []string{wt}) {
		t.Fatalf("a %s pointing outside wt resolved within wt", kind)
	}
}

func TestResolvePath_SymlinkLoopIsError(t *testing.T) {
	dir := t.TempDir()
	testutil.Link(t, "loop-b", filepath.Join(dir, "loop-a"))
	testutil.Link(t, "loop-a", filepath.Join(dir, "loop-b"))
	if _, err := ResolvePath(filepath.Join(dir, "loop-a"), dir); err == nil {
		t.Fatal("ResolvePath of a link loop: want error")
	}
}

// The pending tail is stacked deepest-first; a link with two or more missing
// components after it is where a wrong rejoin order would show.
func TestResolvePath_DanglingSymlinkWithDeepTail(t *testing.T) {
	wt, other := t.TempDir(), t.TempDir()
	kind := testutil.Link(t, filepath.Join(other, "missing"), filepath.Join(wt, "link"))

	got, err := ResolvePath(filepath.Join(wt, "link", "a", "b.txt"), wt)
	if err != nil {
		t.Fatalf("ResolvePath through dangling %s: %v", kind, err)
	}
	if want := filepath.Join(canon(t, other), "missing", "a", "b.txt"); got != want {
		t.Fatalf("ResolvePath through dangling %s = %q, want %q", kind, got, want)
	}
}

func TestWithin_SiblingPrefixIsNotWithin(t *testing.T) {
	dir := canon(t, t.TempDir())
	root := filepath.Join(dir, "b")
	sibling := filepath.Join(dir, "bc")
	if Within(sibling, []string{root}) {
		t.Fatalf("%q reported within %q on a bare string prefix", sibling, root)
	}
	if !Within(filepath.Join(root, "f.txt"), []string{root}) {
		t.Fatalf("a file under the root is not within it")
	}
}

func TestWithin_SymlinkEscapeNotWithin(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	kind := testutil.Link(t, outside, filepath.Join(root, "evil"))

	resolved, err := ResolvePath(filepath.Join(root, "evil"), root)
	if err != nil {
		t.Fatalf("ResolvePath through %s: %v", kind, err)
	}
	if Within(resolved, []string{root}) {
		t.Fatalf("a %s under the root pointing outside is within the root", kind)
	}
}

func TestWithin_RootItselfIsWithin(t *testing.T) {
	root := canon(t, t.TempDir())
	if !Within(root, []string{root}) {
		t.Fatalf("the root itself is not within itself")
	}
}

func TestWithin_EmptyRootsFalse(t *testing.T) {
	dir := canon(t, t.TempDir())
	if Within(dir, nil) {
		t.Fatal("Within with no roots = true, want false")
	}
}
