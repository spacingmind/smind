package runs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
)

// newSetupBusyEnv builds a queuedTestEnv whose task is created by a
// workspace with a setup script that blocks until <worktree>/GO exists, then
// exits with exitCode. The Manager's setup-finish hook is wired to the
// registry exactly as wsapi.New does.
func newSetupBusyEnv(t *testing.T, exitCode string) *queuedTestEnv {
	t.Helper()
	wm, st := newTestWorkspaceManager(t)
	repo := newTestRepo(t)
	ws, err := wm.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatal(err)
	}
	script := `while [ ! -f GO ]; do sleep 0.05; done; exit ` + exitCode
	if _, err := st.SetWorkspaceScripts(ws.ID, &script, nil); err != nil {
		t.Fatal(err)
	}
	task, err := wm.CreateTask(ws.ID, nil, "Task")
	if err != nil {
		t.Fatal(err)
	}
	if task.SetupStatus != workspace.SetupRunning {
		t.Fatalf("setup_status = %q, want running", task.SetupStatus)
	}
	runner := taskrunner.New(wm,
		taskrunner.WithSessionStore(taskrunner.NewChatSessionStore(st)),
		taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath}))
	reg := newTestRegistry(t, st)
	reg.SetStarter(wm, runner)
	wm.SetSetupFinishedHook(func(int64) { reg.DeliverQueued() })
	t.Cleanup(wm.CancelSetups)
	return &queuedTestEnv{wm: wm, st: st, task: task, runner: runner, reg: reg}
}

func (e *queuedTestEnv) finishSetup(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(*e.task.WorktreePath, "GO"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func (e *queuedTestEnv) queueDuringSetup(t *testing.T, mode WhenBusy) StartOutcome {
	t.Helper()
	out, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, 0, taskrunner.ProviderGLM, "hello", taskrunner.PermissionSettings{}, "", mode, store.ChatQueueSourceHuman, 0, 0)
	if err != nil {
		t.Fatalf("StartWhenBusy(%s) during setup error = %v", mode, err)
	}
	if !out.Queued || out.QueueItemID == 0 {
		t.Fatalf("outcome = %+v, want queued", out)
	}
	return out
}

func TestSetup_QueuedPromptDeliveredAfterSetup(t *testing.T) {
	t.Parallel()
	for _, mode := range []WhenBusy{WhenBusyQueue, WhenBusyInterrupt} {
		e := newSetupBusyEnv(t, "0")
		out := e.queueDuringSetup(t, mode)
		time.Sleep(150 * time.Millisecond)
		if item, _ := e.st.GetChatQueueItem(out.QueueItemID); item.Status != store.ChatQueueStatusQueued {
			t.Fatalf("%s: item delivered while setup still running: %+v", mode, item)
		}
		e.finishSetup(t)
		item := e.waitForQueuedStatus(t, out.QueueItemID, store.ChatQueueStatusDelivered)
		if item.RunID == nil {
			t.Fatalf("%s: delivered item has no run id", mode)
		}
	}
}

func TestSetup_QueuedPromptDeliveredAfterFailedSetup(t *testing.T) {
	t.Parallel()
	e := newSetupBusyEnv(t, "1")
	out := e.queueDuringSetup(t, WhenBusyQueue)
	e.finishSetup(t)
	item := e.waitForQueuedStatus(t, out.QueueItemID, store.ChatQueueStatusDelivered)
	if item.RunID == nil {
		t.Fatal("delivered item has no run id")
	}
	task, _ := e.wm.GetTask(e.task.ID)
	if task.SetupStatus != workspace.SetupFailed {
		t.Fatalf("setup_status = %q, want failed", task.SetupStatus)
	}
}

func TestSetup_RejectWhenBusyNamesSetup(t *testing.T) {
	t.Parallel()
	e := newSetupBusyEnv(t, "0")
	for _, mode := range []WhenBusy{"", WhenBusyReject} {
		_, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, 0, taskrunner.ProviderGLM, "x", taskrunner.PermissionSettings{}, "", mode, store.ChatQueueSourceHuman, 0, 0)
		if err == nil || !strings.Contains(err.Error(), "setup script") {
			t.Fatalf("mode %q error = %v, want one naming the setup script", mode, err)
		}
	}
}

// TestSetup_StartRefusedWhileSetupRunning: the plain Start path (task.prompt,
// run.start default, CLI) must not start a run inside a half-installed
// worktree either.
func TestSetup_StartRefusedWhileSetupRunning(t *testing.T) {
	t.Parallel()
	e := newSetupBusyEnv(t, "0")
	_, err := e.reg.Start(context.Background(), e.wm, e.runner, e.task.ID, 0, taskrunner.ProviderGLM, "x", taskrunner.PermissionSettings{}, "")
	var sb *SetupBusyError
	if err == nil || !errors.As(err, &sb) || !strings.Contains(err.Error(), "setup script") {
		t.Fatalf("Start() error = %v, want *SetupBusyError naming the setup script", err)
	}
	if got := e.reg.List(0); len(got) != 0 {
		t.Fatalf("a run was registered: %+v", got)
	}
}
