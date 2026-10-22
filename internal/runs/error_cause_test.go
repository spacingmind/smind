package runs

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/taskrunner"
)

func textEv(s string) Event { return Event{Type: taskrunner.EventTypeText, Text: s} }

func TestAgentErrorCause(t *testing.T) {
	t.Parallel()
	tool := Event{Type: taskrunner.EventTypeToolCall}
	long := "[error] " + strings.Repeat("x", 400)
	tests := []struct {
		name string
		hist []Event
		want string
	}{
		{"none", []Event{textEv("hello")}, ""},
		{"empty", nil, ""},
		{"single chunk", []Event{textEv("working"), tool, textEv("[error] 429 Usage limit reached. Resets at 17:35")}, "429 Usage limit reached. Resets at 17:35"},
		{"marker split across chunks", []Event{tool, textEv("[err"), textEv("or] 429 Usage "), textEv("limit reached")}, "429 Usage limit reached"},
		{"only the first line", []Event{tool, textEv("[error] first line\nsecond line")}, "first line"},
		{"an old error the agent got past does not count", []Event{textEv("[error] transient"), tool, textEv("all fine now")}, ""},
		{"text before the marker is ignored", []Event{tool, textEv("note: [error] boom")}, "boom"},
		{"clipped", []Event{tool, textEv(long)}, strings.Repeat("x", maxAgentErrorCause) + "…"},
	}
	for _, tt := range tests {
		if got := agentErrorCause(tt.hist); got != tt.want {
			t.Errorf("%s: agentErrorCause = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// runToError starts a run on the fake agent's scenario and returns the error
// message the finished run recorded.
func runToError(t *testing.T, scenario string) string {
	t.Helper()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, scenario)
	reg := newTestRegistry(t, st)
	runner := taskrunner.New(wm,
		taskrunner.WithSessionStore(taskrunner.NewChatSessionStore(st)),
		taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath}))
	reg.SetStarter(wm, runner)
	id, err := reg.Start(context.Background(), wm, runner, task.ID, 0, taskrunner.ProviderGLM, "go", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	got := waitForStatus(t, reg, id, StatusError, 10*time.Second)
	row, err := st.GetRun(id)
	if err != nil || row.ErrMsg != got.Err {
		t.Fatalf("persisted err = %q (%v), want it to match the live status %q", row.ErrMsg, err, got.Err)
	}
	return got.Err
}

func TestFinish_PromotesAgentErrorText(t *testing.T) {
	t.Parallel()
	got := runToError(t, "quota-error")
	if !strings.HasPrefix(got, "429 Usage limit reached for 5 hour. Your limit will reset at 2026-10-22 17:35:18 (") || !strings.Contains(got, "rpc error -32603: Internal error") {
		t.Fatalf("run error = %q, want the agent's cause first and the original error in parentheses", got)
	}
}

func TestFinish_NoAgentErrorTextUnchanged(t *testing.T) {
	t.Parallel()
	got := runToError(t, "plain-error")
	if strings.HasPrefix(got, "(") || !strings.Contains(got, "rpc error -32603: Internal error") || strings.Contains(got, "429") {
		t.Fatalf("run error = %q, want the original error only", got)
	}
}

func TestFinish_RPCErrorDataReachesTheRunError(t *testing.T) {
	t.Parallel()
	got := runToError(t, "data-error")
	if !strings.Contains(got, "rpc error -32603: Internal error (rate limited: retry after 17:35)") {
		t.Fatalf("run error = %q, want the JSON-RPC error's data in the message", got)
	}
}
