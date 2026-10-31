package failure

import (
	"strings"
	"testing"
)

// TestGuidance_PerClass: a Lead routes on the class, so each guidance line
// says what to do and what NOT to do; an unknown or empty kind has none.
func TestGuidance_PerClass(t *testing.T) {
	for kind, want := range map[string][]string{
		KindAuthExpired:         {"human must sign in", "Do not retry", "do not fall back"},
		KindModelNotInPlan:      {"different model", "Do not retry the same model", "do not fall back"},
		KindPlanWindowExhausted: {"resetAt", "fallbackProfileId", "Do not retry immediately"},
		KindTransient429:        {"short throttle", "not a plan limit", "Do not switch profile"},
		KindRateLimited:         {"unknown kind", "Do not assume the plan is exhausted"},
		KindCLIExited:           {"session is stored", "continue"},
	} {
		got := Guidance(kind)
		for _, w := range want {
			if !strings.Contains(got, w) {
				t.Errorf("Guidance(%q) = %q, missing %q", kind, got, w)
			}
		}
	}
	if Guidance("") != "" || Guidance("martian") != "" {
		t.Fatal("empty or unknown kind must have no guidance")
	}
}

// TestExitCode_PerClass: 6 = a rate limit (wait or retry), 7 = a human must
// fix setup, 1 = anything else.
func TestExitCode_PerClass(t *testing.T) {
	for kind, want := range map[string]int{
		KindPlanWindowExhausted: 6, KindTransient429: 6, KindRateLimited: 6,
		KindAuthExpired: 7, KindModelNotInPlan: 7,
		KindCLIExited: 1, "": 1, "martian": 1,
	} {
		if got := ExitCode(kind); got != want {
			t.Errorf("ExitCode(%q) = %d, want %d", kind, got, want)
		}
	}
}
