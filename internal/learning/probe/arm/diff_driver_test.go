package arm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/gitsafe/gitsafetest"
)

// Test_arm_diff_does_not_run_drivers runs Diff with its two directories
// inside a repository whose configuration arms a textconv driver, an
// external diff program and an fsmonitor program. The diff must render the
// real -one/+two hunk and leave no marker.
//
// Note: Diff never runs git inside either directory (cmd.Dir is a daemon
// temp dir outside any repository), so the armed repo's config cannot reach
// this call at all; the gitsafe flags and the from-scratch environment are
// what the test pins, plus the fact that moving the call inside a repo one
// day would still not run the drivers.
func Test_arm_diff_does_not_run_drivers(t *testing.T) {
	r := gitsafetest.NewDriverRepo(t)
	baseDir := filepath.Join(r.Dir, "base-export")
	armDir := filepath.Join(r.Dir, "the-arm")
	for _, d := range []struct {
		dir, content string
	}{{baseDir, "one\n"}, {armDir, "two\n"}} {
		if err := os.MkdirAll(d.dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d.dir, "a.txt"), []byte(d.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	diff, _, err := Diff(context.Background(), baseDir, armDir, 1<<20)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(diff, "-one") || !strings.Contains(diff, "+two") {
		t.Fatalf("diff = %q, want the a.txt -one/+two hunk", diff)
	}
	if got := r.Markers(); len(got) != 0 {
		t.Fatalf("Diff ran a driver git must never run; markers: %q", got)
	}
}
