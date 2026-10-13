package runs

import (
	"context"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/taskrunner"

	"github.com/spacingmind/smind/internal/tasktree"
)

// waitForFactState polls TaskRunFacts until taskID reports want (or the
// deadline passes), because a started run's status flips to running
// asynchronously in its drive goroutine.
func waitForFactState(t *testing.T, reg *Registry, taskID int64, want tasktree.State) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		f := reg.TaskRunFacts()[taskID]
		if f.State == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("task %d fact state never became %q", taskID, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestTaskRunFacts_StatesAndRunningIDs proves the per-task summary: an
// idle (run-less) task is simply absent, a running run reports running
// with its id, and after the run finishes the latest finished state wins.
func TestTaskRunFacts_StatesAndRunningIDs(t *testing.T) {
	wm, st := newTestWorkspaceManager(t)
	reg := newTestRegistry(t, st)
	runner := newTestRunner(wm)
	task := newTestTask(t, wm, "")

	facts := reg.TaskRunFacts()
	if _, ok := facts[task.ID]; ok {
		t.Fatalf("idle task %d present in facts %+v", task.ID, facts)
	}

	runID, err := reg.Start(context.Background(), wm, runner, task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitForFactState(t, reg, task.ID, tasktree.StateRunning)
	f := reg.TaskRunFacts()[task.ID]
	if len(f.RunningRunIDs) != 1 || f.RunningRunIDs[0] != runID {
		t.Fatalf("runningRunIds = %v, want [%s]", f.RunningRunIDs, runID)
	}

	waitForStatus(t, reg, runID, StatusDone, 5*time.Second)
	f = reg.TaskRunFacts()[task.ID]
	if f.State != tasktree.StateDone {
		t.Fatalf("after finish state = %q, want done", f.State)
	}
	if len(f.RunningRunIDs) != 0 {
		t.Fatalf("runningRunIds after finish = %v, want empty", f.RunningRunIDs)
	}
}

// TestTaskRunFacts_InterruptedMapsToError pins the interrupted->error
// mapping: a rehydrated interrupted run reports error, not idle.
func TestTaskRunFacts_InterruptedMapsToError(t *testing.T) {
	wm, st := newTestWorkspaceManager(t)
	reg := newTestRegistry(t, st)
	runner := newTestRunner(wm)
	task := newTestTask(t, wm, "")
	if _, err := reg.Start(context.Background(), wm, runner, task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, ""); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Restart the registry over the same store: New reconciles the
	// still-running row to interrupted and rehydrates it.
	reg2 := newTestRegistry(t, st)
	if f := reg2.TaskRunFacts()[task.ID]; f.State != tasktree.StateError {
		t.Fatalf("interrupted state = %q, want error", f.State)
	}
}
