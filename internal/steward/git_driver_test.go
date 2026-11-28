package steward

import (
	"context"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/gitsafe/gitsafetest"
)

// Test_steward_collect_does_not_run_drivers runs CollectGit in a repository
// whose configuration arms a textconv driver, an external diff program and
// an fsmonitor program. CollectGit reads diffs, logs and refs, so without
// the gitsafe hardening git would run the diff drivers while producing its
// output; the fsmonitor pin is covered by the validation and sandbox tests.
// It must not: no marker file may appear, and the facts must still report
// the real change, so the hardening cannot be hiding a broken collector.
func Test_steward_collect_does_not_run_drivers(t *testing.T) {
	r := gitsafetest.NewDriverRepo(t)
	r.Git(t, "add", "-A")
	r.Git(t, "commit", "-m", "change")
	r.ClearMarkers()
	base := r.Git(t, "rev-parse", "HEAD~1")
	head := r.Git(t, "rev-parse", "HEAD")

	facts, err := CollectGit(context.Background(), r.Dir, base, base, head)
	if err != nil {
		t.Fatalf("CollectGit: %v", err)
	}
	if got := r.Markers(); len(got) != 0 {
		t.Fatalf("CollectGit ran a driver git must never run; markers: %q", got)
	}
	if !strings.Contains(facts.Numstat, "a.txt") {
		t.Fatalf("Numstat = %q, want it to report a.txt changed", facts.Numstat)
	}
	if len(facts.Commits) == 0 {
		t.Fatalf("Commits is empty, want the collect commit")
	}
}
