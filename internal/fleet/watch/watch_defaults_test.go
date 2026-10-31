package watch

import "testing"

// TestWatcherDefaultFailureLimitIsThree pins ADR-0048's "three failed
// observations means status unknown" default: the other watch tests set
// FailureLimit explicitly, so without this a changed default would go unseen.
func TestWatcherDefaultFailureLimitIsThree(t *testing.T) {
	w := New(Options{})
	if got := w.o.FailureLimit; got != 3 {
		t.Fatalf("default FailureLimit = %d, want 3", got)
	}
}
