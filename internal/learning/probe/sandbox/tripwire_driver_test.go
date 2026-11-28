package sandbox

import (
	"testing"

	"github.com/spacingmind/smind/internal/gitsafe/gitsafetest"
)

// Test_tripwire_snapshot_does_not_run_fsmonitor snapshots a repository whose
// core.fsmonitor names a program: `git status --porcelain=v2` runs it, so an
// unhardened tripwire would execute a program the sandboxed repo's own
// configuration names. takeRepo must not, and it must still read the
// repository, so the hardening is not hiding a broken snapshot.
func Test_tripwire_snapshot_does_not_run_fsmonitor(t *testing.T) {
	r := gitsafetest.NewDriverRepo(t)

	snap, err := takeRepo(r.Dir)
	if err != nil {
		t.Fatalf("takeRepo: %v", err)
	}
	if got := r.Markers(); len(got) != 0 {
		t.Fatalf("takeRepo ran a driver git must never run; markers: %q", got)
	}
	if snap.head == "" {
		t.Fatalf("snapshot head is empty, want the repository's HEAD")
	}
}
