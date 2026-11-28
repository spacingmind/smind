package gitsafetest

import "testing"

// TestNewStrippedRepoStartsWithNoMarkers pins the fixture's own promise:
// nothing ran during the setup (the configuration is armed last and the
// markers are cleared before returning), and a plain read-only git call on
// the armed repository runs nothing either — so a test that later sees a
// marker knows it came from the operation under test.
func TestNewStrippedRepoStartsWithNoMarkers(t *testing.T) {
	r := NewStrippedRepo(t)
	if got := r.Markers(); len(got) != 0 {
		t.Fatalf("fresh fixture reports markers %q, want none", got)
	}
	if _, err := r.Run("status", "--porcelain"); err != nil {
		t.Fatalf("git status: %v", err)
	}
	if got := r.Markers(); len(got) != 0 {
		t.Fatalf("git status created markers %q, want none", got)
	}
}
