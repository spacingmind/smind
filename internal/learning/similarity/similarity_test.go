package similarity

import "testing"

const skillBodyA = `Run the Go tests with the race detector before pushing.
Use "go test ./... -race -count=1" from the repo root.
If a test hangs, check for a missing t.Cleanup.
Read the failing output fully before editing code.
Re-run only the failing package after the fix.`

// skillBodyB is skillBodyA with one sentence reworded and two lines reordered.
const skillBodyB = `Run the Go tests with the race detector before pushing.
Check for a missing t.Cleanup when a test hangs.
Use "go test ./... -race -count=1" from the repo root.
Read the failing output fully before editing code.
Re-run only the failing package after the fix.`

const looselyA = `Run the Go tests with the race detector before pushing a change so data races are caught early.`

const looselyB = `Before opening a pull request, run the full test suite with the race flag enabled to catch data races.`

func TestSimilarity_Deterministic(t *testing.T) {
	a := skillBodyA
	b := skillBodyB
	first := Score(a, b)
	for i := 0; i < 50; i++ {
		if got := Score(a, b); got != first {
			t.Fatalf("non-deterministic: iteration %d got %v, want %v", i, got, first)
		}
		if got := Score(b, a); got != first {
			t.Fatalf("not symmetric: Score(b,a)=%v, want %v", got, first)
		}
	}
}

func TestSimilarity_Bands(t *testing.T) {
	cases := []struct {
		name     string
		a, b     string
		min, max float64
	}{
		{"identical", "Use when a Go test hangs.", "Use when a Go test hangs.", 1.0, 1.0},
		{"case and punctuation", "Use when X!", "use when x", 1.0, 1.0},
		{"unrelated", "run go tests with race detector", "format commit messages with conventional commits", 0.0, 0.3},
		{"near duplicate", skillBodyA, skillBodyB, 0.85, 1.0},
		{"loosely related", looselyA, looselyB, 0.3, 0.85},
		{"empty", "", "anything", 0.0, 0.0},
		{"empty second", "anything", "", 0.0, 0.0},
	}
	for _, tc := range cases {
		got := Score(tc.a, tc.b)
		t.Logf("%s: score=%v", tc.name, got)
		if got < tc.min || got > tc.max {
			t.Errorf("%s: Score=%v, want in [%v, %v]", tc.name, got, tc.min, tc.max)
		}
	}
}
