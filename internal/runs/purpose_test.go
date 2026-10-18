package runs

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

type finishRecorder struct {
	mu   sync.Mutex
	got  []RunFinished
	done chan RunFinished
	fn   func(RunFinished)
}

func newFinishRecorder() *finishRecorder {
	return &finishRecorder{done: make(chan RunFinished, 16)}
}

func (o *finishRecorder) OnRunFinished(f RunFinished) {
	if o.fn != nil {
		o.fn(f)
	}
	o.mu.Lock()
	o.got = append(o.got, f)
	o.mu.Unlock()
	o.done <- f
}

func (o *finishRecorder) wait(t *testing.T) RunFinished {
	t.Helper()
	select {
	case f := <-o.done:
		return f
	case <-time.After(5 * time.Second):
		t.Fatal("observer not called")
		return RunFinished{}
	}
}

type panicObserver struct{ called chan struct{} }

func (p panicObserver) OnRunFinished(RunFinished) {
	p.called <- struct{}{}
	panic("boom")
}

func TestRegistry_ObserverCalledOncePerFinishedRun(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	reg := newTestRegistry(t, st)
	runner := newTestRunner(wm)

	pan := panicObserver{called: make(chan struct{}, 4)}
	rec1, rec2 := newFinishRecorder(), newFinishRecorder()
	// Re-entrancy: the observer calls back into the registry; a lock held
	// across the call would deadlock here.
	rec1.fn = func(f RunFinished) {
		_ = reg.List(0)
		_ = reg.ListFiltered(0, 0, "")
		if _, _, err := reg.History(f.RunID); err != nil {
			t.Errorf("History in observer: %v", err)
		}
	}
	reg.AddObserver(pan)
	reg.AddObserver(rec1)
	reg.AddObserver(rec2)

	runID, err := reg.Start(context.Background(), wm, runner, task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, reg, runID, StatusDone, 5*time.Second)
	f1, f2 := rec1.wait(t), rec2.wait(t)
	if f1.RunID != runID || f1.TaskID != task.ID || f1.Status != "done" || f1.Purpose != "user" || f1.Provider != string(taskrunner.ProviderGLM) {
		t.Fatalf("RunFinished = %+v", f1)
	}
	if f2.RunID != runID {
		t.Fatalf("second observer = %+v", f2)
	}
	<-pan.called

	// Cancelled run.
	hangTask := newTestTask(t, wm, "hang")
	hangID, err := reg.Start(context.Background(), wm, runner, hangTask.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatal(err)
	}
	waitForHistoryLen(t, reg, hangID, 1, 5*time.Second)
	if err := reg.Stop(hangID); err != nil {
		t.Fatal(err)
	}
	if f := rec1.wait(t); f.RunID != hangID || f.Status != "stopped" {
		t.Fatalf("cancelled RunFinished = %+v", f)
	}
	rec2.wait(t)

	// Failed run (nonexistent codex command).
	failTask := newTestTask(t, wm, "")
	failRunner := taskrunner.New(wm, taskrunner.WithCodexCommand([]string{"/nonexistent/smind-test-codex-agent"}))
	failID, err := reg.Start(context.Background(), wm, failRunner, failTask.ID, 0, taskrunner.ProviderCodexNative, "hi", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if f := rec1.wait(t); f.RunID != failID || f.Status != "error" {
		t.Fatalf("failed RunFinished = %+v", f)
	}
	rec2.wait(t)

	rec1.mu.Lock()
	defer rec1.mu.Unlock()
	if len(rec1.got) != 3 {
		t.Fatalf("observer calls = %d, want exactly 3", len(rec1.got))
	}
}

func TestRegistry_ObserverToolCallCountDistinctIDs(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "structured")
	reg := newTestRegistry(t, st)
	rec := newFinishRecorder()
	reg.AddObserver(rec)

	runID, err := reg.Start(context.Background(), wm, newTestRunner(wm), task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, reg, runID, StatusDone, 5*time.Second)
	hist, _, _ := reg.History(runID)
	events := 0
	for _, e := range hist {
		if e.Type == taskrunner.EventTypeToolCall {
			events++
		}
	}
	if events < 2 {
		t.Fatalf("fixture has %d tool-call events, want >= 2 sharing one id", events)
	}
	if f := rec.wait(t); f.ToolCalls != 1 {
		t.Fatalf("ToolCalls = %d, want 1 distinct id", f.ToolCalls)
	}
}

func TestRegistry_ListHidesSystemRunsUnlessIncluded(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	sysTask := newTestTask(t, wm, "")
	reg := newTestRegistry(t, st)
	runner := newTestRunner(wm)

	userID, err := reg.Start(context.Background(), wm, runner, task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatal(err)
	}
	sysID, err := reg.Start(context.Background(), wm, runner, sysTask.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "", WithPurpose("reflect"))
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, reg, userID, StatusDone, 5*time.Second)
	waitForStatus(t, reg, sysID, StatusDone, 5*time.Second)

	ids := func(rs []RunSummary) map[string]string {
		m := map[string]string{}
		for _, r := range rs {
			m[r.ID] = r.Purpose
		}
		return m
	}
	if got := ids(reg.List(0)); len(got) != 1 || got[userID] != "" {
		t.Fatalf("List = %v", got)
	}
	if got := ids(reg.ListFiltered(0, 0, "")); len(got) != 1 {
		t.Fatalf("ListFiltered = %v", got)
	}
	if got := ids(reg.ListFiltered(sysTask.ID, 0, "")); len(got) != 0 {
		t.Fatalf("ListFiltered(sys task) = %v", got)
	}
	got := ids(reg.ListWith(RunFilter{IncludeSystem: true}))
	if len(got) != 2 || got[sysID] != "reflect" {
		t.Fatalf("ListWith(IncludeSystem) = %v", got)
	}
	row, _ := st.GetRun(sysID)
	if row.Purpose != "reflect" {
		t.Fatalf("persisted purpose = %q", row.Purpose)
	}
}

func TestRegistry_RehydrateIgnoresSystemRuns(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	sysTask := newTestTask(t, wm, "")
	reg1 := newTestRegistry(t, st)
	runner := newTestRunner(wm)
	userID, _ := reg1.Start(context.Background(), wm, runner, task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
	sysID, err := reg1.Start(context.Background(), wm, runner, sysTask.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "", WithPurpose("curate"))
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, reg1, userID, StatusDone, 5*time.Second)
	waitForStatus(t, reg1, sysID, StatusDone, 5*time.Second)

	reg2 := newTestRegistry(t, st)
	got := reg2.ListWith(RunFilter{IncludeSystem: true})
	if len(got) != 1 || got[0].ID != userID {
		t.Fatalf("rehydrated runs = %+v, want only the user run", got)
	}
	if _, _, err := reg2.History(sysID); err == nil {
		t.Fatal("system run was rehydrated")
	}
}

func TestSystemRun_PermissionAutoDenied(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "permission")
	reg := newTestRegistry(t, st)

	runID, err := reg.Start(context.Background(), wm, newTestRunner(wm), task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "", WithPurpose("reflect"))
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, reg, runID, StatusDone, 5*time.Second)
	hist, _, _ := reg.History(runID)
	var resolved []taskrunner.Event
	for _, e := range hist {
		if e.Type == taskrunner.EventTypePermissionResolved {
			resolved = append(resolved, e)
		}
	}
	if len(resolved) != 1 || resolved[0].PermissionOptionID != "deny-1" || resolved[0].PermissionResolution != PermissionResolvedBySystem {
		t.Fatalf("resolved = %+v, want one system deny-1", resolved)
	}
}

func TestSystemRun_NoPendingPermissionNotification(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "permission")
	reg := newTestRegistry(t, st)
	n := &permissionCounter{}
	reg.SetNotifier(n)

	runID, err := reg.Start(context.Background(), wm, newTestRunner(wm), task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "", WithPurpose("reflect"))
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, reg, runID, StatusDone, 5*time.Second)
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.pending != 0 || n.resolved != 0 {
		t.Fatalf("permission notifications pending=%d resolved=%d, want none", n.pending, n.resolved)
	}
}

type permissionCounter struct {
	mu                sync.Mutex
	pending, resolved int
}

func (n *permissionCounter) NotifyRunStatus(RunStatus) {}
func (n *permissionCounter) NotifyPermissionPending(string, int64, int64, string, string, []taskrunner.PermissionOption) {
	n.mu.Lock()
	n.pending++
	n.mu.Unlock()
}
func (n *permissionCounter) NotifyPermissionResolved(string, int64, int64, string, string, string) {
	n.mu.Lock()
	n.resolved++
	n.mu.Unlock()
}
func (n *permissionCounter) NotifyChatQueueUpdated(int64, []store.ChatQueueItem) {}
