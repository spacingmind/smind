package runs

import (
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

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

// TestDispositionOf is the J1 table: which finished runs offer a continue. Each
// row names why. The rate_limited rows hinge on the reset time against Now.
func TestDispositionOf(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Minute), now.Add(time.Hour)
	tests := []struct {
		name string
		f    DispositionFacts
		want string
	}{
		{"done at max_turn_requests", DispositionFacts{Status: StatusDone, StopReason: "max_turn_requests"}, DispositionNeedsContinue},
		{"done at max_tokens", DispositionFacts{Status: StatusDone, StopReason: "max_tokens"}, DispositionNeedsContinue},
		{"done at budget_exceeded", DispositionFacts{Status: StatusDone, StopReason: "budget_exceeded"}, DispositionNeedsContinue},
		{"stopped by daemon_restart", DispositionFacts{Status: StatusStopped, StopReason: StopReasonDaemonRestart}, DispositionNeedsContinue},
		{"interrupted by daemon_restart", DispositionFacts{Status: StatusInterrupted, StopReason: StopReasonDaemonRestart}, DispositionNeedsContinue},
		{"daemon_restart_fresh: no session", DispositionFacts{Status: StatusInterrupted, StopReason: StopReasonDaemonRestartFresh}, ""},
		{"stopped by the user", DispositionFacts{Status: StatusStopped, StopReason: "cancelled", HasSession: true}, ""},
		{"done normally", DispositionFacts{Status: StatusDone, StopReason: "end_turn", HasSession: true}, ""},
		{"error cli_exited with a session", DispositionFacts{Status: StatusError, FailureKind: "cli_exited", HasSession: true}, DispositionNeedsContinue},
		{"error cli_exited without a session", DispositionFacts{Status: StatusError, FailureKind: "cli_exited"}, ""},
		{"error rate_limited, reset passed", DispositionFacts{Status: StatusError, FailureKind: "rate_limited", FailureResetAt: &past, HasSession: true}, DispositionNeedsContinue},
		{"error rate_limited, reset at Now", DispositionFacts{Status: StatusError, FailureKind: "rate_limited", FailureResetAt: &now, HasSession: true}, DispositionNeedsContinue},
		{"error rate_limited, reset in the future", DispositionFacts{Status: StatusError, FailureKind: "rate_limited", FailureResetAt: &future, HasSession: true}, ""},
		{"error rate_limited, reset unknown", DispositionFacts{Status: StatusError, FailureKind: "rate_limited", HasSession: true}, ""},
		{"error rate_limited, reset passed, no session", DispositionFacts{Status: StatusError, FailureKind: "rate_limited", FailureResetAt: &past}, ""},
		{"error permission denied/timeout (no kind)", DispositionFacts{Status: StatusError, HasSession: true}, ""},
		{"error auth", DispositionFacts{Status: StatusError, FailureKind: "auth", HasSession: true}, ""},
		{"error invalid model", DispositionFacts{Status: StatusError, FailureKind: "invalid_model", HasSession: true}, ""},
		{"error refused", DispositionFacts{Status: StatusError, FailureKind: "refused", HasSession: true}, ""},
		{"cli_exited kind on a running run", DispositionFacts{Status: StatusRunning, FailureKind: "cli_exited", HasSession: true}, ""},
		{"cli_exited kind on a done run", DispositionFacts{Status: StatusDone, FailureKind: "cli_exited", HasSession: true}, ""},
	}
	for _, tt := range tests {
		tt.f.Now = now
		if got := DispositionOf(tt.f); got != tt.want {
			t.Errorf("%s: DispositionOf = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// A finished cli_exited run reads needs-continue through the registry only when
// its chat has a stored session, live and after a daemon restart (rehydrate).
func TestFinishCLIExitedDispositionNeedsSession(t *testing.T) {
	t.Parallel()
	for _, withSession := range []bool{true, false} {
		wm, st := newTestWorkspaceManager(t)
		task := newTestTask(t, wm, "")
		chats, err := st.ListChatsByTask(task.ID, false)
		if err != nil || len(chats) == 0 {
			t.Fatalf("ListChatsByTask: %v %d", err, len(chats))
		}
		chatID := chats[0].ID
		if withSession {
			if _, err := st.BindChatProvider(chatID, "claude-native"); err != nil {
				t.Fatal(err)
			}
			if _, err := st.SetChatAgentSession(chatID, `{"id":"s1"}`); err != nil {
				t.Fatal(err)
			}
		}
		reg := newTestRegistry(t, st)
		id := "cli-exit"
		if _, err := st.CreateRun(store.Run{ID: id, TaskID: task.ID, ChatID: chatID, Provider: "claude-native", Prompt: "p", Status: "running", StartedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
		r := registerTestRun(reg, id)
		r.taskID, r.chatID, r.provider, r.prompt = task.ID, chatID, taskrunner.ProviderClaudeNative, "p"
		reg.finish(r, &taskrunner.CLIExitError{ExitCode: -1})
		want := ""
		if withSession {
			want = DispositionNeedsContinue
		}
		_, status, err := reg.History(id)
		if err != nil {
			t.Fatal(err)
		}
		if status.FailureKind != "cli_exited" || status.Disposition != want {
			t.Fatalf("withSession=%v live: kind=%q disposition=%q, want cli_exited/%q", withSession, status.FailureKind, status.Disposition, want)
		}
		if got := reg.TaskRunFacts()[task.ID].Disposition; got != want {
			t.Fatalf("withSession=%v TaskRunFacts disposition = %q, want %q", withSession, got, want)
		}
		// A restarted daemon: rehydrated from the row, and the summary of an evicted/stored row.
		reg2 := newTestRegistry(t, st)
		_, status2, err := reg2.History(id)
		if err != nil {
			t.Fatal(err)
		}
		if status2.Disposition != want {
			t.Fatalf("withSession=%v rehydrated disposition = %q, want %q", withSession, status2.Disposition, want)
		}
		row, err := st.GetRun(id)
		if err != nil {
			t.Fatal(err)
		}
		if got := reg2.summaryFromRow(row).Disposition; got != want {
			t.Fatalf("withSession=%v summaryFromRow disposition = %q, want %q", withSession, got, want)
		}
	}
}
