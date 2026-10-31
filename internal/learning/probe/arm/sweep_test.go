package arm

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/testutil"
)

func TestArmEnd_ArmDirRemoved(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, DirPrefix+"x")
	write(t, filepath.Join(dir, "ro", "f.txt"), "x", 0o444)
	if err := os.Chmod(filepath.Join(dir, "ro"), 0o500); err != nil {
		t.Fatal(err)
	}
	if err := Remove(root, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("arm dir still exists")
	}
	for _, bad := range []string{root, filepath.Join(root, "other"), filepath.Join(root, DirPrefix+"a", "b"), filepath.Dir(root)} {
		if err := Remove(root, bad); err == nil {
			t.Errorf("Remove accepted %q", bad)
		}
	}
}

func TestStartupSweep_RemovesOrphanArmDirs(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	old := filepath.Join(root, DirPrefix+"old")
	fresh := filepath.Join(root, DirPrefix+"fresh")
	other := filepath.Join(root, "keep-me")
	outside := t.TempDir()
	write(t, filepath.Join(old, "f"), "x", 0o444)
	write(t, filepath.Join(fresh, "f"), "x", 0o644)
	write(t, filepath.Join(other, "f"), "x", 0o644)
	write(t, filepath.Join(outside, "precious"), "x", 0o644)
	testutil.Link(t, outside, filepath.Join(root, DirPrefix+"link"))
	_ = os.Chtimes(old, now.Add(-2*time.Hour), now.Add(-2*time.Hour))
	_ = os.Chtimes(other, now.Add(-2*time.Hour), now.Add(-2*time.Hour))
	n, err := SweepOrphans(root, time.Hour, now)
	if err != nil || n != 1 {
		t.Fatalf("SweepOrphans = %d, %v; want 1, nil", n, err)
	}
	for p, want := range map[string]bool{old: false, fresh: true, other: true, filepath.Join(outside, "precious"): true} {
		if _, err := os.Lstat(p); (err == nil) != want {
			t.Errorf("%s exists=%v, want %v", p, err == nil, want)
		}
	}
	if n, err := SweepOrphans(filepath.Join(root, "missing"), time.Hour, now); n != 0 || err != nil {
		t.Errorf("missing root: %d, %v", n, err)
	}
	if _, err := SweepOrphans("relative", time.Hour, now); err == nil {
		t.Error("relative root accepted")
	}
}
