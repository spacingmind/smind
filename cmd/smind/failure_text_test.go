package main

import (
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/runs/failure"
)

func actFor(kind string) *failure.Action {
	return failure.NextAction(kind, failure.ActionContext{Provider: "glm", ProviderLabel: "GLM", Model: "opus", Now: time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)})
}

// TestFailureText_PerClass: the CLI status cell names the class and the next
// step; a plan limit keeps its window/reset sentence.
func TestFailureText_PerClass(t *testing.T) {
	now := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	reset := now.Add(2 * time.Hour)
	for _, tc := range []struct {
		row  runSummaryRow
		want []string
	}{
		{runSummaryRow{Status: "error", FailureKind: failure.KindAuthExpired, FailureAction: actFor(failure.KindAuthExpired)}, []string{"auth expired", "next: Sign in again to GLM"}},
		{runSummaryRow{Status: "error", FailureKind: failure.KindModelNotInPlan, FailureAction: actFor(failure.KindModelNotInPlan)}, []string{"model not in plan", "next: Pick another model"}},
		{runSummaryRow{Status: "error", FailureKind: failure.KindTransient429, FailureAction: actFor(failure.KindTransient429)}, []string{"transient 429", "next: GLM throttled briefly: retry"}},
		{runSummaryRow{Status: "error", FailureKind: failure.KindPlanWindowExhausted, FailureWindow: "5h", FailureResetAt: &reset,
			FailureAction: failure.NextAction(failure.KindPlanWindowExhausted, failure.ActionContext{Window: "5h", ResetAt: &reset, Now: now})}, []string{"rate limited (5h window), resets 12:00", "next: Wait"}},
	} {
		got := runStatusText(tc.row, now)
		for _, w := range tc.want {
			if !strings.Contains(got, w) {
				t.Errorf("runStatusText(%s) = %q, missing %q", tc.row.FailureKind, got, w)
			}
		}
	}
	// No action (an old daemon): the old text, no "next:".
	if got := runStatusText(runSummaryRow{Status: "error", FailureKind: failure.KindPlanWindowExhausted, FailureWindow: "5h"}, now); strings.Contains(got, "next:") {
		t.Errorf("no action must add no next: %q", got)
	}
}

func TestWaitExitCode_NewClasses(t *testing.T) {
	for kind, want := range map[string]int{
		failure.KindPlanWindowExhausted: 6, failure.KindTransient429: 6, failure.KindRateLimited: 6,
		failure.KindAuthExpired: 7, failure.KindModelNotInPlan: 7, failure.KindCLIExited: 1, "": 1,
	} {
		if got := waitExitCode(runSummaryRow{Status: "error", FailureKind: kind}); got != want {
			t.Errorf("waitExitCode(%q) = %d, want %d", kind, got, want)
		}
	}
}

// TestTaskStatus_MessageRoutesOnClass: task_status and task_wait say what the
// class means and what NOT to do, so a Lead can route on failureKind alone.
func TestTaskStatus_MessageRoutesOnClass(t *testing.T) {
	for kind, want := range map[string][]string{
		failure.KindAuthExpired:         {"auth_expired", "Sign in again to GLM", "Do not retry", "do not fall back"},
		failure.KindModelNotInPlan:      {"model_not_in_plan", "Pick another model", "Do not retry the same model"},
		failure.KindPlanWindowExhausted: {"plan_window_exhausted", "Do not retry immediately"},
		failure.KindTransient429:        {"transient_429", "Do not switch profile"},
	} {
		res := runLogsResult{RunID: "r-1", Status: "error", FailureKind: kind, Action: actFor(kind)}
		st := buildTaskStatusOutput(res)
		for _, w := range want {
			if !strings.Contains(st.FailureNote, w) {
				t.Errorf("task_status %s failureNote = %q, missing %q", kind, st.FailureNote, w)
			}
		}
		wt := buildTaskWaitOutput(res, nil, false)
		if wt.State != "error" || !strings.Contains(wt.Message, "Run r-1 failed: "+kind) || !strings.Contains(wt.Message, want[len(want)-1]) {
			t.Errorf("task_wait %s message = %q", kind, wt.Message)
		}
	}
	if st := buildTaskStatusOutput(runLogsResult{RunID: "r-2", Status: "done"}); st.FailureNote != "" {
		t.Errorf("a done run has no failureNote: %q", st.FailureNote)
	}
}

func TestMCPInstructions_ListEveryClass(t *testing.T) {
	for _, k := range []string{failure.KindAuthExpired, failure.KindPlanWindowExhausted, failure.KindModelNotInPlan, failure.KindTransient429, failure.KindRateLimited, failure.KindCLIExited} {
		if !strings.Contains(mcpInstructions, k) {
			t.Errorf("mcpInstructions does not mention failureKind %q", k)
		}
	}
	if strings.Contains(mcpInstructions, "A failed run with failureKind rate_limited hit a provider quota") {
		t.Error("the old single-class rate_limited paragraph is still there")
	}
}
