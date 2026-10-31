package permpolicy

import (
	"path/filepath"
	"testing"

	"github.com/spacingmind/smind/internal/realpath"
)

// segCanon resolves a temp dir once, so expectations compare like for like
// (on macOS a temp dir sits behind the /var -> /private/var link).
func segCanon(t *testing.T, dir string) string {
	t.Helper()
	r, err := realpath.Resolve(dir)
	if err != nil {
		t.Fatalf("realpath %s: %v", dir, err)
	}
	return r
}

func TestSegmentPaths_Basics(t *testing.T) {
	dir := segCanon(t, t.TempDir())
	// A real split, not a hand-built segment, so the paths this extracts
	// are the paths the engine would actually judge.
	p := SplitCommand("cp src/a.txt dst/b.txt > out.log 2>&1")
	if len(p.Segments) != 1 {
		t.Fatalf("segments = %d, want 1 (%+v)", len(p.Segments), p.Segments)
	}
	got, err := SegmentPaths(p.Segments[0], dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(dir, "src", "a.txt"),
		filepath.Join(dir, "dst", "b.txt"),
		filepath.Join(dir, "out.log"),
	}
	if len(got) != len(want) {
		t.Fatalf("paths = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("paths[%d] = %q, want %q (all: %q)", i, got[i], want[i], got)
		}
	}

	// Repeated words collapse; order is kept.
	dup, err := SegmentPaths(Segment{Argv: []string{"cat", "a.txt", "a.txt", "b.txt"}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(dup) != 2 || dup[0] != filepath.Join(dir, "a.txt") || dup[1] != filepath.Join(dir, "b.txt") {
		t.Fatalf("dedup = %q", dup)
	}
}

func TestSegmentPaths_FlagValueAndRedirect(t *testing.T) {
	dir := segCanon(t, t.TempDir())
	out := filepath.Join(dir, "out.txt")
	seg := Segment{
		Argv: []string{"tool", "-v", "--log=" + filepath.Join(dir, "run.log"), "--count=3", "note.txt"},
		Redirects: []Redirect{
			{Op: ">", Target: out},
			{Op: "2>&1"},
			{Op: "2>", Target: "&2"},
			{Op: "1>", Target: "-"},
			{Op: ">>", Target: "/dev/null"},
		},
	}
	got, err := SegmentPaths(seg, dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "run.log"), filepath.Join(dir, "3"), filepath.Join(dir, "note.txt"), out}
	if len(got) != len(want) {
		t.Fatalf("paths = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("paths[%d] = %q, want %q (all: %q)", i, got[i], want[i], got)
		}
	}
}

func TestSegmentPaths_UnresolvableIsError(t *testing.T) {
	if _, err := SegmentPaths(Segment{Argv: []string{"cat", "x/y.txt"}}, ""); err == nil {
		t.Fatal("an unresolvable word must fail the whole call, not be skipped")
	}
}

func TestSegmentPaths_DynamicSkipped(t *testing.T) {
	dir := segCanon(t, t.TempDir())
	seg := Segment{
		Argv: []string{"cp", "$DST/out.txt", "a`id`.txt", "*.log", "f?.txt", "x[0-9].txt", "keep.txt"},
		Redirects: []Redirect{
			{Op: ">", Target: "$TMP/out.log"},
			{Op: "2>", Target: "err.txt"},
		},
	}
	got, err := SegmentPaths(seg, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("paths = %q, want keep.txt and the static redirect target only", got)
	}
	for _, p := range got {
		if dynamicWord(p) {
			t.Fatalf("dynamic word %q resolved as a path", p)
		}
	}
	want := []string{filepath.Join(dir, "keep.txt"), filepath.Join(dir, "err.txt")}
	if got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("paths = %q, want %q first", got, want)
	}
}
