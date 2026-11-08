package runs

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/history/native"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// ACP loads start a process: one per run, a cooldown after a failure, cancelled
// when a run starts on the chat or the Registry closes.

type slowAgent struct {
	starts  atomic.Int32
	started chan struct{} // closed on the first start
	release chan struct{} // a start returns once this is closed or its ctx ends
	err     error
}

func newSlowAgent() *slowAgent {
	return &slowAgent{started: make(chan struct{}), release: make(chan struct{})}
}

func (a *slowAgent) replay(ctx context.Context, ref native.Ref, max int64) ([]Event, error) {
	if a.starts.Add(1) == 1 {
		close(a.started)
	}
	select {
	case <-a.release:
		return a.events(), a.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (a *slowAgent) events() []Event {
	out := []Event{{Type: taskrunner.EventTypeUserMessage, Text: rebuildPrompt}}
	for _, e := range liveTurn("") {
		if e.Type == taskrunner.EventTypeText || e.Type == taskrunner.EventTypeThinking || e.Type == taskrunner.EventTypeToolCall {
			e.Raw = nil
			out = append(out, e)
		}
	}
	return out
}

// acpLoadEnv is rebuild env run "r" turned into an ACP run served by agent.
func acpLoadEnv(t *testing.T, agent *slowAgent) *rebuildEnv {
	t.Helper()
	env := newRebuildEnv(t, true)
	env.reg.SetRebuilder(native.New(native.Config{ACPReplay: agent.replay, MaxConcurrent: 2}))
	ref := native.Ref{Provider: "glm", Harness: native.HarnessACP, SessionID: "s1", Cwd: t.TempDir(), PromptSha256: sha256Hex(rebuildPrompt)}
	b, _ := json.Marshal(ref)
	if err := env.e.st.SetRunNativeRef("r", string(b)); err != nil {
		t.Fatal(err)
	}
	return env
}

func loadAsync(env *rebuildEnv) <-chan Rebuilt {
	ch := make(chan Rebuilt, 1)
	go func() { ch <- env.reg.RebuildHistoryConfirmed(context.Background(), "r", RequesterHuman, true) }()
	return ch
}

// Two confirmed requests for one run start one agent.
func TestACPLoadIsSingleFlightPerRun(t *testing.T) {
	agent := newSlowAgent()
	env := acpLoadEnv(t, agent)
	first := loadAsync(env)
	<-agent.started
	if r := env.reg.RebuildHistoryConfirmed(context.Background(), "r", RequesterHuman, true); r.Reason != native.ReasonBusy || agent.starts.Load() != 1 {
		t.Fatalf("second request: %q, %d starts", r.Reason, agent.starts.Load())
	}
	close(agent.release)
	if r := <-first; !r.OK() {
		t.Fatalf("first load: %q (%s)", r.Reason, r.Detail)
	}
	// after it ends the run can be loaded again
	if r := env.reg.RebuildHistoryConfirmed(context.Background(), "r", RequesterHuman, true); !r.OK() || agent.starts.Load() != 2 {
		t.Fatalf("after the first ended: %q, %d starts", r.Reason, agent.starts.Load())
	}
}

// A failed load is not retried inside the cooldown (same answer, no process), a
// success is never cooled, and the cooldown ends.
func TestACPLoadCooldownAfterAFailure(t *testing.T) {
	agent := newSlowAgent()
	close(agent.release)
	agent.err = errors.New("boom")
	env := acpLoadEnv(t, agent)
	now := time.Now()
	acpNow = func() time.Time { return now }
	t.Cleanup(func() { acpNow = time.Now })
	ctx := context.Background()

	first := env.reg.RebuildHistoryConfirmed(ctx, "r", RequesterHuman, true)
	second := env.reg.RebuildHistoryConfirmed(ctx, "r", RequesterHuman, true)
	if first.Reason != native.ReasonFailed || second.Reason != native.ReasonFailed || agent.starts.Load() != 1 {
		t.Fatalf("first %q, second %q, %d starts (want one)", first.Reason, second.Reason, agent.starts.Load())
	}
	now = now.Add(ACPLoadCooldown - time.Second)
	if r := env.reg.RebuildHistoryConfirmed(ctx, "r", RequesterHuman, true); r.Reason != native.ReasonFailed || agent.starts.Load() != 1 {
		t.Fatalf("inside the cooldown: %q, %d starts", r.Reason, agent.starts.Load())
	}
	now = now.Add(2 * time.Second)
	agent.err = nil
	if r := env.reg.RebuildHistoryConfirmed(ctx, "r", RequesterHuman, true); !r.OK() || agent.starts.Load() != 2 {
		t.Fatalf("after the cooldown: %q, %d starts", r.Reason, agent.starts.Load())
	}
	// an unconfirmed or machine request never reaches the cooldown or the agent
	if r := env.reg.RebuildHistoryConfirmed(ctx, "r", RequesterHuman, false); r.Reason != native.ReasonConfirmRequired {
		t.Fatalf("unconfirmed: %q", r.Reason)
	}
}

// A run that starts on the chat during a load cancels it and kills the agent;
// the answer is native_session_in_use and the run's start is not delayed.
func TestACPLoadIsCancelledWhenARunStartsOnTheChat(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

	// a finished run on the task's chat is the one we load
	first, err := reg.Start(context.Background(), wm, runner, task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, reg, first, StatusDone, 10*time.Second)
	row, err := st.GetRun(first)
	if err != nil {
		t.Fatal(err)
	}
	agent := newSlowAgent()
	reg.SetRebuilder(native.New(native.Config{ACPReplay: agent.replay}))
	ref := native.Ref{Provider: "glm", Harness: native.HarnessACP, SessionID: "s1", Cwd: t.TempDir(), PromptSha256: sha256Hex("hi")}
	b, _ := json.Marshal(ref)
	if err := st.SetRunNativeRef(first, string(b)); err != nil {
		t.Fatal(err)
	}

	got := make(chan Rebuilt, 1)
	go func() { got <- reg.RebuildHistoryConfirmed(context.Background(), first, RequesterHuman, true) }()
	<-agent.started
	second, err := reg.Start(context.Background(), wm, runner, task.ID, row.ChatID, taskrunner.ProviderGLM, "again", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-got:
		if r.Reason != native.ReasonSessionInUse {
			t.Fatalf("a run started during the load: %q", r.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the load was not cancelled by the run that started on its chat")
	}
	waitForStatus(t, reg, second, StatusDone, 10*time.Second)
}

// A running run on the load's own chat refuses it; one on another chat does not.
func TestACPLoadRefusedWhileTheChatRuns(t *testing.T) {
	agent := newSlowAgent()
	env := acpLoadEnv(t, agent)
	row, _ := env.e.st.GetRun("r")
	// the chat becomes busy between the request's check and the start: simulate by
	// making the request-time check pass and the BeforeStart check fail
	registerRunning(env.reg, "busy", env.e.task.ID, row.ChatID+1000, time.Now()) // another chat: must not block
	close(agent.release)
	if r := env.reg.RebuildHistoryConfirmed(context.Background(), "r", RequesterHuman, true); !r.OK() {
		t.Fatalf("a run on another chat blocked the load: %q", r.Reason)
	}
	registerRunning(env.reg, "busy2", env.e.task.ID, row.ChatID, time.Now())
	starts := agent.starts.Load()
	if r := env.reg.RebuildHistoryConfirmed(context.Background(), "r", RequesterHuman, true); r.Reason != native.ReasonSessionInUse || agent.starts.Load() != starts {
		t.Fatalf("a running chat: %q, %d new starts", r.Reason, agent.starts.Load()-starts)
	}
}

// Shutdown cancels the loads in flight, waits for their agents and refuses new ones.
func TestACPLoadIsCancelledAtShutdown(t *testing.T) {
	agent := newSlowAgent()
	env := acpLoadEnv(t, agent)
	got := loadAsync(env)
	<-agent.started
	closed := make(chan struct{})
	go func() { env.reg.CloseAll(); close(closed) }()
	select {
	case r := <-got:
		if r.Reason != native.ReasonCannotRebuild {
			t.Fatalf("a load during shutdown: %q", r.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CloseAll did not cancel the load")
	}
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("CloseAll did not return")
	}
	if r := env.reg.RebuildHistoryConfirmed(context.Background(), "r", RequesterHuman, true); r.Reason != native.ReasonCannotRebuild || agent.starts.Load() != 1 {
		t.Fatalf("after shutdown: %q, %d starts", r.Reason, agent.starts.Load())
	}
}
