package probesvc

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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
