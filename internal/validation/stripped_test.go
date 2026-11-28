package validation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/gitsafe/gitsafetest"
)

// Test_validation_digest_runs_no_armed_program takes the validator's
// snapshot (Take: `rev-parse HEAD` then `git status --porcelain`, the read
// behind the digest) in a repository armed with every program class the
// stripped mode neutralises, with a.flt made stat-dirty so the status read
// would re-run its clean filter. No armed program may run, and the
// snapshot must still report the change, so the stripped mode is not
// hiding a broken read. The fixture sets GIT_CONFIG_* through t.Setenv,
// so no t.Parallel here.
func Test_validation_digest_runs_no_armed_program(t *testing.T) {
	r := gitsafetest.NewStrippedRepo(t)
	if err := os.WriteFile(filepath.Join(r.Dir, "a.flt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatalf("write a.flt: %v", err)
	}

	snap, err := Take(context.Background(), r.Dir)
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if got := r.Markers(); len(got) != 0 {
		t.Fatalf("Take ran an armed program git must never run; markers: %q", got)
	}
	if !strings.Contains(snap.Status, "a.flt") {
		t.Fatalf("Status = %q, want the a.flt modification", snap.Status)
	}
}
