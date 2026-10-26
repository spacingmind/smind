package runs

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

const cliExitSecret = "sk-ant-api03-FAKEFAKEFAKEFAKEFAKEFAKE0123456789"

// TestCLIExit_TypedFailureWithPartialUsage: a CLI that dies without a result
// (exit 3, a SIGKILL, or a clean exit 0) ends the run as an error typed
// cli_exited; the message names the exit, the last event and the redacted
// stderr tail; the usage the stream reported before the exit is recorded as
// partial_stream with no cost; no retry or continue disposition appears.
func TestCLIExit_TypedFailureWithPartialUsage(t *testing.T) {
	t.Parallel()
	// The SDK reads the exit code racing the process reaper, so a plain exit
	// reports either its code or "exit status unknown"; a signal always the latter.
	for _, tc := range []struct{ scenario, wantExit string }{
		{"cli-exit-nonzero", "exit code 3|exit status unknown"},
		{"cli-exit-signal", "exit status unknown: killed by a signal"},
		{"cli-exit-zero", "exit code 0|exit status unknown"},
	} {
		t.Run(tc.scenario, func(t *testing.T) {
			t.Parallel()
			e := claudeScenarioEnv(t, tc.scenario)
			id := startClaude(t, e)
			st := waitForStatus(t, e.reg, id, StatusError, 10*time.Second)
			if st.FailureKind != "cli_exited" {
				t.Fatalf("FailureKind = %q, want cli_exited (err %q)", st.FailureKind, st.Err)
			}
			if !containsAny(st.Err, strings.Split(tc.wantExit, "|")) {
				t.Errorf("Err names neither of %q: %s", tc.wantExit, st.Err)
			}
			for _, want := range []string{"last event: stream_event", "heap out of memory", "[REDACTED]", "resume the session"} {
				if !strings.Contains(st.Err, want) {
					t.Errorf("Err missing %q: %s", want, st.Err)
				}
			}
			if strings.Contains(st.Err, "FAKEFAKE") || strings.Contains(st.Err, "sk-ant") {
				t.Errorf("Err leaks the stderr secret: %s", st.Err)
			}
			if st.Disposition != "" {
				t.Errorf("Disposition = %q, want none", st.Disposition)
			}
			row, err := e.st.GetRun(id)
			if err != nil || row.FailureKind != "cli_exited" {
				t.Fatalf("persisted failure kind = %q (%v)", row.FailureKind, err)
			}
			u, err := e.st.GetRunUsage(id)
			if err != nil {
				t.Fatalf("GetRunUsage() error = %v: the partial usage was not recorded", err)
			}
			// message 1 (100 in, 40 cache read, 10 cache write, 25 out) + message 2 start (200 in, 2 out).
			if u.Source != string(taskrunner.UsageSourcePartialStream) || u.CostUSD != nil ||
				u.InputTokens == nil || *u.InputTokens != 300 || u.OutputTokens == nil || *u.OutputTokens != 27 ||
				u.CachedInputTokens == nil || *u.CachedInputTokens != 40 || u.CacheWriteTokens == nil || *u.CacheWriteTokens != 10 {
				t.Fatalf("usage = %+v, want partial_stream 300/27/40/10 and no cost", u)
			}
			time.Sleep(200 * time.Millisecond)
			rows, err := e.st.ListRecentRuns(50)
			if err != nil {
				t.Fatalf("ListRecentRuns() error = %v", err)
			}
			if len(rows) != 1 {
				t.Fatalf("%d runs on the task after the failure, want 1 (no automatic retry)", len(rows))
			}
		})
	}
}

// TestCLIExit_NotifiesParentLikeAnyFailedChild: a Peer whose CLI died wakes its
// parent with a child event naming the typed failure.
func TestCLIExit_NotifiesParentLikeAnyFailedChild(t *testing.T) {
	t.Parallel()
	e := newNotifyEnv(t)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	e.runner = taskrunner.New(e.wm,
		taskrunner.WithSessionStore(taskrunner.NewChatSessionStore(e.st)),
		taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath}),
		taskrunner.WithDefaultModel(taskrunner.ProviderClaudeNative, "claude-haiku-5-5"), taskrunner.WithDefaultModel(taskrunner.ProviderClaudeNative, "claude-haiku-5-5"), taskrunner.WithClaudeCLIPath(self, "FAKE_CLAUDE_SCENARIO=cli-exit-nonzero"))
	e.reg.SetStarter(e.wm, e.runner)
	out, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.child.ID, 0, taskrunner.ProviderClaudeNative, "work", taskrunner.PermissionSettings{}, "", WhenBusyReject, store.ChatQueueSourceAgent, e.task.ID, e.parentChat.ID)
	if err != nil {
		t.Fatalf("StartWhenBusy() error = %v", err)
	}
	waitForStatus(t, e.reg, out.RunID, StatusError, 10*time.Second)
	waitUntil(t, "child event", func() bool { return len(e.events(t, e.parentChat.ID)) == 1 })
	if p := e.events(t, e.parentChat.ID)[0].Prompt; !strings.Contains(p, "error") || !strings.Contains(p, "cli_exited") {
		t.Fatalf("child event %q does not name the error and cli_exited", p)
	}
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// exitErrFor runs scenario to its cli_exited failure and returns the run error text.
func exitErrFor(t *testing.T, scenario string) (string, time.Duration) {
	t.Helper()
	e := claudeScenarioEnv(t, scenario)
	start := time.Now()
	id := startClaude(t, e)
	st := waitForStatus(t, e.reg, id, StatusError, 15*time.Second)
	if st.FailureKind != "cli_exited" {
		t.Fatalf("FailureKind = %q, want cli_exited (err %q)", st.FailureKind, st.Err)
	}
	return st.Err, time.Since(start)
}

// TestCLIExit_StderrDrainedBeforeError: the CLI writes three stderr chunks
// with pauses and exits non-zero; the stream ends at stdout EOF, but the error
// must still carry every chunk.
func TestCLIExit_StderrDrainedBeforeError(t *testing.T) {
	t.Parallel()
	got, _ := exitErrFor(t, "cli-exit-chunks")
	for _, want := range []string{"chunk-one", "chunk-two", "chunk-three"} {
		if !strings.Contains(got, want) {
			t.Errorf("Err missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, "stderr incomplete") {
		t.Errorf("Err marks complete stderr as incomplete: %s", got)
	}
}

// TestCLIExit_StderrDrain_BoundedWhenPipeHeldOpen: a grandchild keeps stderr
// open for 6s after the CLI exits; the error is built within the drain bound
// TestCLIExit_StderrDrain_BoundedWhenPipeHeldOpen: a grandchild holding the
// CLI's stderr open cannot hold the error back. Since SDK v0.4.2 the SDK cuts
// its stderr copy at its own WaitDelay and still reports the reaped exit
// status, so the error arrives within the bound with the stderr written
// before the exit; the "(stderr incomplete)" mark is only set on the fallback
// path, because the SDK does not say whether its copy was cut.
func TestCLIExit_StderrDrain_BoundedWhenPipeHeldOpen(t *testing.T) {
	t.Parallel()
	got, took := exitErrFor(t, "cli-exit-held")
	if !strings.Contains(got, "held-line") {
		t.Errorf("Err lost the stderr written before the exit: %s", got)
	}
	if took > 4500*time.Millisecond {
		t.Errorf("error took %s, want within the drain bound", took)
	}
}

// TestCLIExit_NoStderr_StillTyped: a CLI that writes nothing to stderr still
// ends as a typed cli_exited error, stderr "empty", with no incomplete mark.
func TestCLIExit_NoStderr_StillTyped(t *testing.T) {
	t.Parallel()
	got, _ := exitErrFor(t, "cli-exit-nostderr")
	if !strings.Contains(got, "stderr: empty") || strings.Contains(got, "stderr incomplete") {
		t.Errorf("Err = %s, want stderr: empty and no incomplete mark", got)
	}
}
