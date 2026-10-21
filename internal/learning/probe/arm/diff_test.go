package arm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// diffFx is a pristine base export plus an arm materialised from the same
// commit.
func diffFx(t *testing.T) (base, armDir string) {
	t.Helper()
	repo, sha := realRepo(t)
	root := t.TempDir()
	base, armDir = filepath.Join(root, "base"), filepath.Join(root, "arm")
	for _, d := range []string{base, armDir} {
		if _, err := Export(context.Background(), Spec{Repo: repo, BaseSHA: sha, Dir: d}); err != nil {
			t.Fatal(err)
		}
	}
	// The arm gets its own repo, like Materialise leaves it.
	git(t, armDir, "init", "-q", "-b", "probe")
	return base, armDir
}

func TestDiff_ModifiedAddedDeleted(t *testing.T) {
	base, armDir := diffFx(t)
	write(t, filepath.Join(armDir, "README.md"), "HELLO\n", 0o644)
	write(t, filepath.Join(armDir, "docs", "new.txt"), "fresh\nfile\n", 0o644)
	if err := os.Remove(filepath.Join(armDir, "cmd", "run.sh")); err != nil {
		t.Fatal(err)
	}
	d, n, err := Diff(context.Background(), base, armDir, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"diff --git a/README.md b/README.md", "-hello", "+HELLO", "deleted file mode", "a/cmd/run.sh",
		"diff --git a/docs/new.txt b/docs/new.txt", "new file mode", "+fresh", "+file"} {
		if !strings.Contains(d, want) {
			t.Errorf("diff lacks %q:\n%s", want, d)
		}
	}
	for _, bad := range []string{base, armDir, os.TempDir() + "/smind-probe-diff"} {
		if strings.Contains(d, bad) {
			t.Errorf("temp path %q leaked:\n%s", bad, d)
		}
	}
	if n != len(d) {
		t.Errorf("n = %d, len = %d (no clipping expected)", n, len(d))
	}
	// A clean arm has no diff, and the arm repo's .git is never part of it.
	clean, cleanArm := diffFx(t)
	if d, n, err := Diff(context.Background(), clean, cleanArm, 1000); err != nil || d != "" || n != 0 {
		t.Fatalf("clean arm: %q %d %v", d, n, err)
	}
}

func TestDiff_ArmGitConfigPayloadNeverRuns(t *testing.T) {
	base, armDir := diffFx(t)
	marker := filepath.Join(t.TempDir(), "pwned")
	payload := "touch " + marker
	cfg := "[core]\n\tfsmonitor = " + payload + "\n\tpager = " + payload + "\n\thooksPath = /nonexistent\n" +
		"[diff \"x\"]\n\ttextconv = " + payload + "\n\tcommand = " + payload + "\n[filter \"y\"]\n\tclean = " + payload + "\n\tsmudge = " + payload + "\n"
	write(t, filepath.Join(armDir, ".git", "config"), cfg, 0o644)
	write(t, filepath.Join(armDir, ".gitattributes"), "* diff=x filter=y\n", 0o644)
	write(t, filepath.Join(armDir, "README.md"), "changed\n", 0o644)
	d, _, err := Diff(context.Background(), base, armDir, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("an arm-planted git config executed (marker exists: %v)", err)
	}
	if !strings.Contains(d, "+changed") {
		t.Fatalf("diff missing the change:\n%s", d)
	}
	if strings.Contains(d, "fsmonitor") {
		t.Fatalf("arm .git leaked into the diff:\n%s", d)
	}
}

func TestDiff_SymlinksSkippedAndNeverRead(t *testing.T) {
	base, armDir := diffFx(t)
	outside := t.TempDir()
	write(t, filepath.Join(outside, "secret.txt"), "TOP-SECRET-VALUE\n", 0o644)
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(armDir, "leak")); err != nil {
		t.Skip("symlinks unavailable")
	}
	if err := os.Symlink(outside, filepath.Join(armDir, "linkdir")); err != nil {
		t.Fatal(err)
	}
	// A symlink replacing a tracked file reads as a deletion, not its target.
	if err := os.Remove(filepath.Join(armDir, "README.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(armDir, "README.md")); err != nil {
		t.Fatal(err)
	}
	d, _, err := Diff(context.Background(), base, armDir, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(d, "TOP-SECRET-VALUE") || strings.Contains(d, "leak") || strings.Contains(d, "linkdir") {
		t.Fatalf("a symlink target was read or listed:\n%s", d)
	}
	if !strings.Contains(d, "deleted file mode") {
		t.Fatalf("symlink-replaced file should read as deleted:\n%s", d)
	}
}

func TestDiff_ClipsAndReportsFullSize(t *testing.T) {
	base, armDir := diffFx(t)
	write(t, filepath.Join(armDir, "big.txt"), strings.Repeat("x\n", 5000), 0o644)
	d, n, err := Diff(context.Background(), base, armDir, 1000)
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

func TestDiff_BinaryAndArgs(t *testing.T) {
	base, armDir := diffFx(t)
	write(t, filepath.Join(armDir, "b.bin"), string([]byte{1, 0, 2}), 0o644)
	d, _, err := Diff(context.Background(), base, armDir, 1000)
	if err != nil || !strings.Contains(d, "Binary files") {
		t.Fatalf("binary: %q %v", d, err)
	}
	if _, _, err := Diff(context.Background(), "rel", armDir, 10); err == nil {
		t.Fatal("relative base accepted")
	}
	if _, _, err := Diff(context.Background(), base, armDir, 0); err == nil {
		t.Fatal("zero maxBytes accepted")
	}
}
