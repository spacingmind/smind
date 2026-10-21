package arm

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// armWithBase materialises realRepo's first commit and returns its dir.
func armWithBase(t *testing.T) string {
	t.Helper()
	repo, base := realRepo(t)
	dir := filepath.Join(t.TempDir(), "arm")
	if _, err := Materialise(context.Background(), Spec{Repo: repo, BaseSHA: base, Dir: dir}); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDiff_ModifiedAddedDeletedUntracked(t *testing.T) {
	dir := armWithBase(t)
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("README.md", "HELLO\n")
	write("new.txt", "fresh\nfile\n")
	if err := os.Remove(filepath.Join(dir, "cmd", "run.sh")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	// An added-and-staged file counts as a change against HEAD too.
	write("staged.txt", "s\n")
	if out, err := exec.Command("git", "-C", dir, "add", "staged.txt").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	before := statusOf(t, dir)

	d, n, err := Diff(context.Background(), dir, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"-hello", "+HELLO", "deleted file mode", "a/cmd/run.sh", "diff --git a/new.txt b/new.txt", "new file mode", "+fresh", "+file", "staged.txt"} {
		if !strings.Contains(d, want) {
			t.Errorf("diff lacks %q:\n%s", want, d)
		}
	}
	if strings.Contains(d, "passwd") || strings.Contains(d, "link") {
		t.Errorf("symlink rendered:\n%s", d)
	}
	if n != len(d) {
		t.Errorf("n = %d, len = %d (no clipping expected)", n, len(d))
	}
	if after := statusOf(t, dir); after != before {
		t.Errorf("Diff mutated the arm repo: %q -> %q", before, after)
	}
}

func TestDiff_ClipsAndReportsFullSize(t *testing.T) {
	dir := armWithBase(t)
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(strings.Repeat("x\n", 5000)), 0o644); err != nil {
		t.Fatal(err)
	}
	d, n, err := Diff(context.Background(), dir, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(d, "[truncated]\n") || len(d) > 1000+len(truncatedMarker) {
		t.Fatalf("len %d, tail %q", len(d), d[max(0, len(d)-30):])
	}
	if n <= 1000 {
		t.Fatalf("n = %d, want the pre-clip size > 1000", n)
	}
}

func TestDiff_NoChangesAndBinaryAndArgs(t *testing.T) {
	dir := armWithBase(t)
	d, n, err := Diff(context.Background(), dir, 1000)
	if err != nil || d != "" || n != 0 {
		t.Fatalf("clean arm: %q %d %v", d, n, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.bin"), []byte{1, 0, 2}, 0o644); err != nil {
		t.Fatal(err)
	}
	d, _, err = Diff(context.Background(), dir, 1000)
	if err != nil || !strings.Contains(d, "Binary files") {
		t.Fatalf("binary: %q %v", d, err)
	}
	if _, _, err := Diff(context.Background(), "rel", 10); err == nil {
		t.Fatal("relative dir accepted")
	}
	if _, _, err := Diff(context.Background(), dir, 0); err == nil {
		t.Fatal("zero maxBytes accepted")
	}
}

func statusOf(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain=v1", "-uall").CombinedOutput()
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	return string(out)
}
