package failure

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestGuidance_PerClass: a Lead routes on the class, so each guidance line
// says what to do and what NOT to do; an unknown or empty kind has none.
func TestGuidance_PerClass(t *testing.T) {
	for kind, want := range map[string][]string{
		KindAuthExpired:           {"human must sign in", "Do not retry", "do not fall back"},
		KindModelNotInPlan:        {"different model", "Do not retry the same model", "do not fall back"},
		KindPlanWindowExhausted:   {"resetAt", "fallbackProfileId", "Do not retry immediately"},
		KindTransient429:          {"short throttle", "not a plan limit", "Do not switch profile"},
		KindRateLimited:           {"unknown kind", "Do not assume the plan is exhausted"},
		KindCLIExited:             {"session is stored", "continue"},
		KindContextWindowExceeded: {"fresh chat", "do not retry the same chat", "do not fall back"},
		KindSessionBudgetExceeded: {"session budget", "A human raises it", "do not retry and do not fall back"},
		KindTooManyDenials:        {"review the permission policy", "do not retry the same run", "do not widen permissions"},
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
		KindContextWindowExceeded: 7, KindSessionBudgetExceeded: 7, KindTooManyDenials: 7,
		KindCLIExited: 1, "": 1, "martian": 1,
	} {
		if got := ExitCode(kind); got != want {
			t.Errorf("ExitCode(%q) = %d, want %d", kind, got, want)
		}
	}
}

// TestDescribe_EveryKnownClassHasASentence: Describe is the one daemon-owned
// label table; an empty or unknown kind says nothing.
func TestDescribe_EveryKnownClassHasASentence(t *testing.T) {
	for _, kind := range AllKinds() {
		if Describe(kind) == "" {
			t.Errorf("Describe(%q) = \"\", want a sentence", kind)
		}
	}
	if Describe("") != "" || Describe("martian") != "" {
		t.Fatal("empty or unknown kind must have no sentence")
	}
}

// TestDescribe_CLIExitedIsReadable: the sentence a human reads must explain
// what happened and that the session survives, not echo the internal class.
func TestDescribe_CLIExitedIsReadable(t *testing.T) {
	d := Describe(KindCLIExited)
	if d == "" || strings.Contains(d, "exited without a result") {
		t.Fatalf("Describe(cli_exited) = %q", d)
	}
	for _, w := range []string{"stopped before finishing", "session is saved", "continue"} {
		if !strings.Contains(d, w) {
			t.Fatalf("Describe(cli_exited) = %q, missing %q", d, w)
		}
	}
}

// TestFailureLabels_NoApprovalWords (AC7): no failure sentence, guidance line
// or action label may tell the reader to merge, or to approve / always allow
// anything -- those are human decisions.
func TestFailureLabels_NoApprovalWords(t *testing.T) {
	banned := regexp.MustCompile(`(?i)merge|approve|always[ _-]?allow`)
	for _, kind := range AllKinds() {
		if s := Describe(kind); s == "" || banned.MatchString(s) {
			t.Errorf("Describe(%q) = %q", kind, s)
		}
		if g := Guidance(kind); g == "" || banned.MatchString(g) {
			t.Errorf("Guidance(%q) = %q", kind, g)
		}
		if a := NextAction(kind, ActionContext{Provider: "glm", Model: "opus", Now: time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC), ResetAt: ptrTime(time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC))}); a == nil || a.Label == "" || banned.MatchString(a.Label) {
			t.Errorf("NextAction(%q) = %+v", kind, a)
		}
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
