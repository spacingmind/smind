package gitstate

import (
	"context"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/gitsafe/gitsafetest"
)

// Test_gitstate_runner_does_not_run_drivers_or_fsmonitor runs DefaultRunner
// in a repository whose configuration arms a textconv driver, an external
// diff program and an fsmonitor program. The diff call renders a patch —
// the real exposure: an unhardened `git diff` would run the external diff
// and the textconv — and the status call would run the fsmonitor. Neither
// must happen, and both calls must still return the real output, so the
// hardening is not hiding a broken runner.
func Test_gitstate_runner_does_not_run_drivers_or_fsmonitor(t *testing.T) {
	r := gitsafetest.NewDriverRepo(t)
	ctx := context.Background()

	patch, err := DefaultRunner(ctx, r.Dir, "diff", "--no-color")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if !strings.Contains(patch, "-one") || !strings.Contains(patch, "+two") {
		t.Fatalf("diff output = %q, want the a.txt -one/+two hunk", patch)
	}

	status, err := DefaultRunner(ctx, r.Dir, "status", "--porcelain")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(status, "a.txt") {
		t.Fatalf("status output = %q, want the a.txt modification", status)
	}

	if got := r.Markers(); len(got) != 0 {
		t.Fatalf("DefaultRunner ran a driver git must never run; markers: %q", got)
	}
}
