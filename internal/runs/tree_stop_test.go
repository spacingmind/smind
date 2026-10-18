package runs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// waitTerminal blocks until runID is no longer StatusRunning in memory,
// failing the test on timeout -- StopAndWait's tests must observe real
// terminal states, not race the finish path.
func waitTerminal(t *testing.T, reg *Registry, runID string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		r, err := reg.get(runID)
		if err != nil {
			t.Fatalf("get(%q) error = %v", runID, err)
		}
		r.mu.Lock()
		running := r.status == StatusRunning
		r.mu.Unlock()
		if !running {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %q never reached a terminal state", runID)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestStopAndWait_ReturnsWhenRunFinished proves StopAndWait both signals
// the stop and blocks until the run is fully terminal (closedCh fired),
// so a caller that gets nil can rely on the subprocess being gone.
func TestStopAndWait_ReturnsWhenRunFinished(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	root := newTestTask(t, wm, "hang")
	runner := taskrunner.New(wm, taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath}))
	reg := newTestRegistry(t, st)

	chat, err := wm.DefaultChat(root.ID)
	if err != nil {
		t.Fatalf("DefaultChat() error = %v", err)
	}
	runID, err := reg.Start(context.Background(), wm, runner, root.ID, chat.ID, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := reg.StopAndWait(ctx, runID); err != nil {
		t.Fatalf("StopAndWait() error = %v, want nil", err)
	}
	_, status, err := reg.History(runID)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if status.Status == StatusRunning {
		t.Fatalf("status = %q, want terminal after StopAndWait", status.Status)
	}
}

// TestStopAndWait_ContextDeadlineReturnsError proves a run that never
// reaches a terminal state surfaces as the context's error, not a hang.
// The stuck run is a hand-registered in-memory run whose cancel is a
// no-op (deterministic: no subprocess timing involved); it is removed
// from the registry via t.Cleanup so CloseAll's drain never blocks on it.
func TestStopAndWait_ContextDeadlineReturnsError(t *testing.T) {
	t.Parallel()
	_, st := newTestWorkspaceManager(t)
	reg := newTestRegistry(t, st)

	r := &run{
		id: "stuck", taskID: 1, chatID: 1, status: StatusRunning,
		cancel:             func() {},
		closedCh:           make(chan struct{}),
		subscribers:        make(map[int]*subQueue),
		pendingPermissions: make(map[string]chan string),
	}
	reg.mu.Lock()
	reg.runs[r.id] = r
	reg.mu.Unlock()
	t.Cleanup(func() {
		reg.mu.Lock()
		delete(reg.runs, r.id)
		reg.mu.Unlock()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := reg.StopAndWait(ctx, r.id)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("StopAndWait() error = %v, want context.DeadlineExceeded", err)
	}
}

// TestStopAndWait_AlreadyFinishedIsNil proves an already-terminal run --
// and a run unknown to memory -- are no-op nils, matching Stop's intent
// semantics.
func TestStopAndWait_AlreadyFinishedIsNil(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	root := newTestTask(t, wm, "")
	runner := taskrunner.New(wm, taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath}))
	reg := newTestRegistry(t, st)

	chat, err := wm.DefaultChat(root.ID)
	if err != nil {
		t.Fatalf("DefaultChat() error = %v", err)
	}
	runID, err := reg.Start(context.Background(), wm, runner, root.ID, chat.ID, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitTerminal(t, reg, runID)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := reg.StopAndWait(ctx, runID); err != nil {
		t.Fatalf("StopAndWait() on a finished run error = %v, want nil", err)
	}
	if err := reg.StopAndWait(ctx, "no-such-run"); err != nil {
		t.Fatalf("StopAndWait(unknown) error = %v, want nil", err)
	}
}
