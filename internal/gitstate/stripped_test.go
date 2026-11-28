package gitstate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/gitsafe/gitsafetest"
)

// Test_gitstate_runner_runs_no_armed_program runs DefaultRunner's status
// read (the one the UI polls) in a repository armed with every program
// class the stripped mode neutralises: hooks, clean/smudge and process
// filters, a merge driver, a credential helper, an ssh command, a signing
// program and an uploadpack program. a.flt and a.fl2 are made stat-dirty
// first, so an unhardened `git status` would re-run their clean filters.
// None of the armed programs may run, and the status must still report
// the real changes, so the stripped mode is not hiding a broken read.
// The fixture sets GIT_CONFIG_* through t.Setenv, so no t.Parallel here.
func Test_gitstate_runner_runs_no_armed_program(t *testing.T) {
	r := gitsafetest.NewStrippedRepo(t)
	for _, name := range []string{"a.flt", "a.fl2"} {
		if err := os.WriteFile(filepath.Join(r.Dir, name), []byte("dirty "+name+"\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	out, err := DefaultRunner(context.Background(), r.Dir, "status", "--porcelain")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if got := r.Markers(); len(got) != 0 {
		t.Fatalf("DefaultRunner ran an armed program git must never run; markers: %q", got)
	}
	if !strings.Contains(out, "a.flt") || !strings.Contains(out, "a.fl2") {
		t.Fatalf("status = %q, want the a.flt and a.fl2 modifications", out)
	}
}
