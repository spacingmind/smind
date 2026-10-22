package runs

import "testing"

func TestDispositionFor(t *testing.T) {
	tests := []struct {
		status Status
		reason string
		want   string
	}{
		// A clean finish at a cap: done, but cut off.
		{StatusDone, "max_turn_requests", DispositionNeedsContinue},
		{StatusDone, "max_tokens", DispositionNeedsContinue},
		{StatusDone, "budget_exceeded", DispositionNeedsContinue},
		// Normal endings and the agent's own choices need nothing.
		{StatusDone, "end_turn", ""},
		{StatusDone, "", ""},
		{StatusDone, "refusal", ""},
		{StatusDone, "cancelled", ""},
		// Only a run that ended done: failures, stops and live runs say so already.
		{StatusError, "max_turn_requests", ""},
		{StatusStopped, "max_turn_requests", ""},
		{StatusInterrupted, "max_tokens", ""},
		{StatusRunning, "max_turn_requests", ""},
		// The daemon going down cut the run off: continue picks it back up (only with a session to resume).
		{StatusStopped, StopReasonDaemonRestart, DispositionNeedsContinue},
		{StatusInterrupted, StopReasonDaemonRestart, DispositionNeedsContinue},
		{StatusStopped, StopReasonDaemonRestartFresh, ""},
		{StatusInterrupted, StopReasonDaemonRestartFresh, ""},
		{StatusDone, StopReasonDaemonRestart, ""},
		{StatusError, StopReasonDaemonRestart, ""},
	}
	for _, tt := range tests {
		if got := DispositionFor(tt.status, tt.reason); got != tt.want {
			t.Errorf("DispositionFor(%s, %q) = %q, want %q", tt.status, tt.reason, got, tt.want)
		}
	}
}

// The status values are unchanged by D13: a disposition is not a status.
func TestDispositionIsNotAStatus(t *testing.T) {
	for _, s := range []Status{StatusRunning, StatusDone, StatusError, StatusStopped, StatusInterrupted} {
		if string(s) == DispositionNeedsContinue {
			t.Fatalf("status %q collides with the disposition value", s)
		}
		if !ValidRunListStatus(s) {
			t.Fatalf("status %q is no longer a valid run.list status", s)
		}
	}
	if ValidRunListStatus(Status(DispositionNeedsContinue)) {
		t.Fatal(`"needs-continue" must not be accepted as a run status`)
	}
}
