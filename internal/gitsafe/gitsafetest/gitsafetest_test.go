package gitsafetest

import (
	"slices"
	"testing"
)

// Test_driverrepo_arms_the_drivers proves each of the three armed drivers
// really runs under plain git: only then does a later driver test's "no
// marker" assertion mean the hardening worked, rather than the fixture
// being inert.
func Test_driverrepo_arms_the_drivers(t *testing.T) {
	r := NewDriverRepo(t)
	if got := r.Markers(); len(got) != 0 {
		t.Fatalf("markers after NewDriverRepo: %q, want none (setup must not run the drivers)", got)
	}

	r.Git(t, "diff")
	if !slices.Contains(r.Markers(), extMarker) {
		t.Fatalf("plain git diff did not run the armed external diff; markers: %q", r.Markers())
	}
	r.ClearMarkers()

	// External diff off via --no-ext-diff (textconv stays on): an empty
	// "-c diff.external=" is NOT unset on current git -- git 2.54 dies with
	// "cannot run : No such file or directory" -- so the flag is the one
	// portable way to see the textconv driver fire on its own.
	r.Git(t, "diff", "--no-ext-diff")
	if !slices.Contains(r.Markers(), textconvMarker) {
		t.Fatalf("git diff --no-ext-diff did not run the armed textconv; markers: %q", r.Markers())
	}
	r.ClearMarkers()

	r.Git(t, "status")
	if !slices.Contains(r.Markers(), fsmMarker) {
		t.Fatalf("git status did not run the armed fsmonitor; markers: %q", r.Markers())
	}
}
