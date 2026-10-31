package probesvc

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/testutil"
)

func TestJanitor_DoesNotSweepActiveProbeDirs(t *testing.T) {
	t.Parallel()
	// Every directory looks hours old to the janitor.
	p := newPfx(t, func(d *Deps) { d.Now = func() time.Time { return time.Now().Add(5 * time.Hour) } })
	p.arms.behave = func(armJob, int) fakeArm { return fakeArm{block: true} }
	pr := p.start(false)
	job := func() armJob { <-p.arms.started; p.arms.mu.Lock(); defer p.arms.mu.Unlock(); return p.arms.jobs[0] }()
	orphan := filepath.Join(p.svc.armsRoot(), "arm-99-A")
	if err := os.MkdirAll(orphan, 0o700); err != nil {
		t.Fatal(err)
	}
	p.svc.sweepStale()
	for _, d := range []string{job.ArmDir, job.PrivateDir, orphan} {
		if _, err := os.Stat(d); err != nil {
			t.Fatalf("%s swept while a probe is active: %v", d, err)
		}
	}
	_ = p.svc.Cancel(p.prop.ID)
	p.wait(pr.ID)
	p.svc.sweepStale()
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan not swept once idle: %v", err)
	}
}

// sweepChildren must not delete through (or remove) a linked directory: a
// symlink, or a junction on Windows (ModeIrregular), pointing outside root.
func TestSweepChildren_LeavesLinkedDirsAlone(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "precious"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(root, "old")
	if err := os.Mkdir(old, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	testutil.Link(t, outside, link)
	past := time.Now().Add(-3 * time.Hour)
	_ = os.Chtimes(old, past, past)
	sweepChildren(root, time.Hour, time.Now())
	if _, err := os.Lstat(old); !os.IsNotExist(err) {
		t.Errorf("the old plain directory was not swept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "precious")); err != nil {
		t.Errorf("swept through the link: %v", err)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Errorf("the link itself was removed: %v", err)
	}
}
