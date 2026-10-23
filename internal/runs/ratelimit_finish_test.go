package runs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

const glmClockText = "429 Usage limit reached for 5 hour. Your limit will reset at 17:35:18"

// finishDirect registers a run (with a store row, so persistence is real),
// optionally feeds it a rate-limit signal, finishes it with err and returns
// the final status. It returns the registry and store for follow-up checks.
func finishDirect(t *testing.T, id string, sig *taskrunner.RateLimitSignal, runErr error) (*Registry, *store.Store, RunStatus) {
	t.Helper()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	reg := newTestRegistry(t, st)
	now := time.Now().UTC()
	if _, err := st.CreateRun(store.Run{ID: id, TaskID: task.ID, Provider: "glm", Prompt: "p", Status: "running", StartedAt: now}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	r := registerTestRun(reg, id)
	r.taskID, r.provider, r.prompt = task.ID, taskrunner.ProviderGLM, "p"
	reg.applyRateLimit(r, sig)
	reg.record(r, Event{Type: taskrunner.EventTypeText, Text: "hello"})
	reg.finish(r, runErr)
	_, status, err := reg.History(id)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	return reg, st, status
}

func TestFinish_GLM429ClassifiedAsRateLimited(t *testing.T) {
	t.Parallel()
	// Real end to end run: the fake GLM agent's quota-error scenario.
	got := runToErrorStatus(t, "quota-error")
	if got.Status != StatusError || got.Disposition != "" {
		t.Fatalf("status = %q disposition = %q, want error and none", got.Status, got.Disposition)
	}
	if got.FailureKind != "rate_limited" || got.FailureWindow != "5h" {
		t.Fatalf("failure = %q/%q, want rate_limited/5h", got.FailureKind, got.FailureWindow)
	}
	if want := time.Date(2026, 10, 22, 17, 35, 18, 0, time.Local); got.FailureResetAt == nil || !got.FailureResetAt.Equal(want) {
		t.Fatalf("FailureResetAt = %v, want %v", got.FailureResetAt, want)
	}
	// The wall-clock case (reset at HH:MM:SS) fills the reset instant.
	_, _, st := finishDirect(t, "glm-clock", nil, errors.New(glmClockText))
	if st.Status != StatusError || st.FailureKind != "rate_limited" || st.FailureWindow != "5h" || st.FailureResetAt == nil {
		t.Fatalf("clock case = %+v", st)
	}
	if st.Err != glmClockText {
		t.Fatalf("Err changed: %q", st.Err)
	}
}

func runToErrorStatus(t *testing.T, scenario string) RunStatus {
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
	return waitForStatus(t, reg, id, StatusError, 10*time.Second)
}

func TestFinish_ClaudeRejectedSignalClassified(t *testing.T) {
	t.Parallel()
	x := time.Unix(1790000000, 0)
	reg, _, st := finishDirect(t, "sig", &taskrunner.RateLimitSignal{Status: "rejected", ResetsAt: x, Type: "five_hour"}, errors.New("claude code prompt: exit status 1"))
	if st.FailureKind != "rate_limited" || st.FailureWindow != "5h" || st.FailureResetAt == nil || !st.FailureResetAt.Equal(x) {
		t.Fatalf("status = %+v", st)
	}
	hist, _, _ := reg.History("sig")
	for _, e := range hist {
		if e.Type == taskrunner.EventTypeRateLimit {
			t.Fatalf("rate-limit event was recorded into history: %+v", e)
		}
	}
}

func TestFinish_DoneRunNeverClassified(t *testing.T) {
	t.Parallel()
	_, _, st := finishDirect(t, "done", &taskrunner.RateLimitSignal{Status: "rejected", ResetsAt: time.Unix(1790000000, 0), Type: "five_hour"}, nil)
	if st.Status != StatusDone || st.FailureKind != "" || st.FailureWindow != "" || st.FailureResetAt != nil {
		t.Fatalf("status = %+v", st)
	}
}

func TestFinish_UnknownErrorNotClassified(t *testing.T) {
	t.Parallel()
	_, _, st := finishDirect(t, "unk", nil, errors.New("acp: rpc error -32603: Internal error"))
	if st.Status != StatusError || st.FailureKind != "" || st.FailureResetAt != nil {
		t.Fatalf("status = %+v", st)
	}
}

func TestFinish_FailurePersistedAndRehydrated(t *testing.T) {
	t.Parallel()
	_, st, live := finishDirect(t, "persist", nil, errors.New(glmClockText))
	row, err := st.GetRun("persist")
	if err != nil {
		t.Fatal(err)
	}
	if row.FailureKind != "rate_limited" || row.FailureWindow != "5h" || row.FailureResetAt == nil || !row.FailureResetAt.Equal(*live.FailureResetAt) {
		t.Fatalf("row = %q %q %v, live = %+v", row.FailureKind, row.FailureWindow, row.FailureResetAt, live)
	}
	reg2, err := New(st)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reg2.CloseAll)
	var found bool
	for _, s := range reg2.List(0) {
		if s.ID != "persist" {
			continue
		}
		found = true
		if s.FailureKind != "rate_limited" || s.FailureWindow != "5h" || s.FailureResetAt == nil || !s.FailureResetAt.Equal(*live.FailureResetAt) {
			t.Fatalf("rehydrated = %+v", s)
		}
	}
	if !found {
		t.Fatal("run missing after rehydrate")
	}
}

func TestStatusEnumUnchanged(t *testing.T) {
	t.Parallel()
	for got, want := range map[Status]string{StatusRunning: "running", StatusDone: "done", StatusError: "error", StatusStopped: "stopped", StatusInterrupted: "interrupted"} {
		if string(got) != want {
			t.Errorf("status %q != %q", got, want)
		}
	}
	if d := DispositionFor(StatusError, "max_turn_requests"); d != "" {
		t.Errorf("DispositionFor(error, max_turn_requests) = %q, want empty", d)
	}
}

// TestDrive_RateLimitEventIsAppliedNotRecorded runs a claude-native turn on
// the fake CLI's "rate-limit-rejected" scenario: drive's forwarder must apply
// the rejected signal to the run and keep the event out of history.
func TestDrive_RateLimitEventIsAppliedNotRecorded(t *testing.T) {
	t.Parallel()
	e := newIsolatedEnv(t, "rate-limit-rejected")
	id, err := e.start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitForStatus(t, e.reg, id, StatusDone, 15*time.Second)
	hist, _, err := e.reg.History(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range hist {
		if ev.Type == taskrunner.EventTypeRateLimit {
			t.Fatalf("rate-limit event recorded into history: %+v", ev)
		}
	}
	r, err := e.reg.get(id)
	if err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	sig := r.rateSignal
	r.mu.Unlock()
	if sig == nil || sig.Status != "rejected" {
		t.Fatalf("applied signal = %+v, want a rejected signal", sig)
	}
}
