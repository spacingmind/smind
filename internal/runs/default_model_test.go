package runs

import (
	"context"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// TestStart_ConfiguredDefaultModel: a run that names no model gets the
// provider's configured default (recorded as its requested model), an explicit
// model wins over it, and without a configured default nothing changes.
func TestStart_ConfiguredDefaultModel(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	reg := newTestRegistry(t, st)
	withDefault := taskrunner.New(wm,
		taskrunner.WithSessionStore(taskrunner.NewChatSessionStore(st)),
		taskrunner.WithDefaultModel(taskrunner.ProviderGLM, "fake-model-b"),
		taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath, "models"}))
	without := taskrunner.New(wm,
		taskrunner.WithSessionStore(taskrunner.NewChatSessionStore(st)),
		taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath, "models"}))
	reg.SetStarter(wm, withDefault)

	startAndRead := func(runner *taskrunner.Runner, opts ...StartOption) string {
		t.Helper()
		id, err := reg.Start(context.Background(), wm, runner, task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "", opts...)
		if err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		row, err := st.GetRun(id)
		if err != nil {
			t.Fatalf("GetRun(%s) error = %v", id, err)
		}
		if err := reg.Stop(id); err != nil {
			t.Fatalf("Stop(%s) error = %v", id, err)
		}
		waitForStatus(t, reg, id, StatusStopped, 5*time.Second)
		return row.Model
	}

	if got := startAndRead(withDefault); got != "fake-model-b" {
		t.Errorf("no model, default configured: run model = %q, want the configured default", got)
	}
	if got := startAndRead(withDefault, WithModel("fake-model-a")); got != "fake-model-a" {
		t.Errorf("explicit model: run model = %q, want fake-model-a (explicit wins)", got)
	}
	if got := startAndRead(without); got != "" {
		t.Errorf("no model, no default: run model = %q, want empty (today's behaviour)", got)
	}
}
