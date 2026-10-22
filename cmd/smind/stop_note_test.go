package main

import (
	"strings"
	"testing"
)

func TestStopNote_MaxTurnRequests(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ reason, contains string }{
		{"max_turn_requests", "turn limit"},
		{"max_tokens", "output token limit"},
		{"refusal", "refused"},
		{"end_turn", ""},
		{"", ""},
	} {
		got := stopNote(tc.reason)
		if tc.contains == "" && got != "" || !strings.Contains(got, tc.contains) {
			t.Errorf("stopNote(%q) = %q, want containing %q", tc.reason, got, tc.contains)
		}
	}
	if got := stopNote("max_turn_requests"); !strings.Contains(got, `"continue"`) {
		t.Errorf("turn-cap note should suggest continue: %q", got)
	}
}

func TestMCPTaskWait_StopNoteOnTurnCap(t *testing.T) {
	t.Parallel()
	out := buildTaskWaitOutput(runLogsResult{RunID: "r", Status: "done", StopReason: "max_turn_requests"}, nil, false)
	if out.State != "done" || out.StopNote == "" {
		t.Fatalf("out = %+v, want state done with a stopNote", out)
	}
	if clean := buildTaskWaitOutput(runLogsResult{RunID: "r", Status: "done", StopReason: "end_turn"}, nil, false); clean.StopNote != "" {
		t.Errorf("end_turn stopNote = %q, want empty", clean.StopNote)
	}
}

func TestMCPTaskStatus_StopNoteOnTurnCap(t *testing.T) {
	t.Parallel()
	out := buildTaskStatusOutput(runLogsResult{RunID: "r", Status: "done", StopReason: "max_turn_requests"})
	if out.Status != "done" || out.StopNote == "" {
		t.Fatalf("out = %+v, want done with a stopNote", out)
	}
}

func TestRenderDone_TurnCapHint(t *testing.T) {
	t.Parallel()
	got := renderDone("run-1", "max_turn_requests")
	if !strings.HasPrefix(got, "run run-1 finished: max_turn_requests\n") || !strings.Contains(got, "turn limit") {
		t.Errorf("renderDone = %q", got)
	}
	if got := renderDone("run-1", "end_turn"); got != "run run-1 finished: end_turn\n" {
		t.Errorf("renderDone(end_turn) = %q, want the bare line", got)
	}
}

func TestRunStatusCellShowsTheDisposition(t *testing.T) {
	if got := runStatusCell("done", "needs-continue", ""); got != "done (needs-continue)" {
		t.Fatalf("runStatusCell = %q", got)
	}
	if got := runStatusCell("done", "", ""); got != "done" {
		t.Fatalf("runStatusCell without a disposition = %q, want the plain status", got)
	}
}
