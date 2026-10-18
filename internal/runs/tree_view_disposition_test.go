package runs

import (
	"context"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/tasktree"
)

// TestTaskRunFacts_DispositionOfTheLatestFinishedRun (D13): the task.tree
// read model says "done" for a run cut off at a cap, so it must also carry
// the needs-continue disposition -- it is what a supervisor reads for its
// children.
func TestTaskRunFacts_DispositionOfTheLatestFinishedRun(t *testing.T) {
	wm, st := newTestWorkspaceManager(t)
	capped := newTestTask(t, wm, "stop max_turn_requests")
	normal := newTestTask(t, wm, "stop end_turn")
	runner := taskrunner.New(wm, taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath}))
	reg := newTestRegistry(t, st)
	for _, taskID := range []int64{capped.ID, normal.ID} {
		id, err := reg.Start(context.Background(), wm, runner, taskID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
		if err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		waitForStatus(t, reg, id, StatusDone, 10*time.Second)
	}

	facts := reg.TaskRunFacts()
	if f := facts[capped.ID]; f.State != tasktree.StateDone || f.Disposition != DispositionNeedsContinue {
		t.Errorf("capped task fact = %+v, want done / needs-continue", f)
	}
	if f := facts[normal.ID]; f.State != tasktree.StateDone || f.Disposition != "" {
		t.Errorf("normal task fact = %+v, want done and no disposition", f)
	}
}
