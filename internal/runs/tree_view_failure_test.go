package runs

import (
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/tasktree"
)

// TestTaskRunFacts_FailureFromLatestFinishedRun (ADR-0041 R2): a task whose
// latest finished run hit a quota carries the typed failure into the
// task.tree facts; an ordinary error carries none.
func TestTaskRunFacts_FailureFromLatestFinishedRun(t *testing.T) {
	wm, st := newTestWorkspaceManager(t)
	limited := newTestTask(t, wm, "quota-error")
	reg := newTestRegistry(t, st)
	runner := taskrunner.New(wm,
		taskrunner.WithSessionStore(taskrunner.NewChatSessionStore(st)),
		taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath}))
	reg.SetStarter(wm, runner)
	id, err := reg.Start(t.Context(), wm, runner, limited.ID, 0, taskrunner.ProviderGLM, "go", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitForStatus(t, reg, id, StatusError, 10*time.Second)

	f := reg.TaskRunFacts()[limited.ID]
	if f.State != tasktree.StateError || f.FailureKind != "rate_limited" || f.FailureWindow != "5h" || f.FailureResetAt == nil {
		t.Fatalf("limited fact = %+v, want error / rate_limited / 5h / reset set", f)
	}
}
