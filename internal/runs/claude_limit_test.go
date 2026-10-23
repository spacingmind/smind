package runs

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// claudeScenarioEnv is a queued test env whose claude-native runs go through
// the fake CLI in the given scenario.
func claudeScenarioEnv(t *testing.T, scenario string) *queuedTestEnv {
	t.Helper()
	e := newQueuedTestEnv(t, "reply")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	e.runner = taskrunner.New(e.wm,
		taskrunner.WithSessionStore(taskrunner.NewChatSessionStore(e.st)),
		taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath}),
		taskrunner.WithClaudeCLIPath(self, "FAKE_CLAUDE_SCENARIO="+scenario))
	e.reg.SetStarter(e.wm, e.runner)
	return e
}

func startClaude(t *testing.T, e *queuedTestEnv) string {
	t.Helper()
	id, err := e.reg.Start(context.Background(), e.wm, e.runner, e.task.ID, 0, taskrunner.ProviderClaudeNative, "do the thing", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	return id
}

// TestFinish_ClaudeSessionLimitRefusalIsARateLimitedError: Claude Code answers
// an exhausted account with an empty "successful" result whose only text is
// "You've hit your session limit · resets 1pm (Asia/Saigon)". The run must not
// read as done: it is an error classified rate_limited with a reset time, and
// it puts the provider on cooldown.
func TestFinish_ClaudeSessionLimitRefusalIsARateLimitedError(t *testing.T) {
	t.Parallel()
	e := claudeScenarioEnv(t, "session-limit")
	id := startClaude(t, e)
	st := waitForStatus(t, e.reg, id, StatusError, 10*time.Second)
	if st.FailureKind != "rate_limited" || st.FailureWindow != "5h" || st.FailureResetAt == nil {
		t.Fatalf("failure = %q/%q/%v, want rate_limited/5h/non-nil", st.FailureKind, st.FailureWindow, st.FailureResetAt)
	}
	if st.Err != "You've hit your session limit · resets 1pm (Asia/Saigon)" {
		t.Fatalf("Err = %q", st.Err)
	}
	if st.Disposition != "" {
		t.Fatalf("disposition = %q, want none", st.Disposition)
	}
	if !st.FailureResetAt.After(time.Now().Add(-time.Minute)) {
		t.Fatalf("reset %v is in the past", st.FailureResetAt)
	}
	if _, ok, _ := e.st.GetProviderCooldown("claude-native", time.Now()); !ok && st.FailureResetAt.Before(time.Now().Add(6*time.Hour)) && st.FailureResetAt.After(time.Now()) {
		t.Fatal("no provider cooldown for claude-native despite a future reset")
	}
}

// TestFinish_NormalClaudeRunStaysDone: a normal reply is not touched.
func TestFinish_NormalClaudeRunStaysDone(t *testing.T) {
	t.Parallel()
	e := claudeScenarioEnv(t, "reply")
	id := startClaude(t, e)
	st := waitForStatus(t, e.reg, id, StatusDone, 10*time.Second)
	if st.FailureKind != "" || st.Err != "" {
		t.Fatalf("normal run: failure %q err %q", st.FailureKind, st.Err)
	}
}
