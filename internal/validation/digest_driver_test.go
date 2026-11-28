package validation

import (
	"context"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/gitsafe/gitsafetest"
)

// Test_validation_snapshot_does_not_run_fsmonitor takes a Snapshot in a
// repository whose core.fsmonitor names a program: `git status --porcelain`
// runs it, so an unhardened snapshot would both execute a program the
// repository's config names and leave its marker behind. Take must not, and
// the snapshot must still report the worktree dirty, so the hardening is
// not hiding a broken status read.
func Test_validation_snapshot_does_not_run_fsmonitor(t *testing.T) {
	r := gitsafetest.NewDriverRepo(t)

	snap, err := Take(context.Background(), r.Dir)
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if got := r.Markers(); len(got) != 0 {
		t.Fatalf("Take ran a driver git must never run; markers: %q", got)
	}
	if !strings.Contains(snap.Status, "a.txt") {
		t.Fatalf("Status = %q, want the a.txt modification", snap.Status)
	}
}
