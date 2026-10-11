package runs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// TestTryBeginArchive_LatchSetBeforeActiveCheck proves the latch is set
// BEFORE the active-run check: a run started from inside the test hook
// (which runs between latching and the check) must be refused with
// ErrTaskArchiving, not sneak into reg.runs unseen -- the exact TOCTOU
// the latch exists to close.
func TestTryBeginArchive_LatchSetBeforeActiveCheck(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

	reg.mu.Lock()
	reg.testHookAfterLatch = func() {
		// A run attempting to start exactly inside the latch-then-check
		// window: with the correct ordering this is refused.
		_, err := reg.Start(context.Background(), wm, runner, task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
		if !errors.Is(err, ErrTaskArchiving) {
			t.Errorf("Start inside the latch window: err = %v, want ErrTaskArchiving", err)
		}
	}
	reg.mu.Unlock()
	t.Cleanup(func() {
		reg.mu.Lock()
		reg.testHookAfterLatch = nil
		reg.mu.Unlock()
	})

	release, err := reg.TryBeginArchive(task.ID)
	if err != nil {
		t.Fatalf("TryBeginArchive() error = %v", err)
	}
	defer release()
}

// TestTryBeginArchive_ReleasesLatchWhenActive proves an active run makes
// TryBeginArchive fail (and unlatch), and that after the run stops a
// later TryBeginArchive succeeds.
func TestTryBeginArchive_ReleasesLatchWhenActive(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

	runID, err := reg.Start(context.Background(), wm, runner, task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitForStatus(t, reg, runID, StatusRunning, 5*time.Second)

	_, err = reg.TryBeginArchive(task.ID)
	if err == nil {
		t.Fatal("TryBeginArchive with a running run: error = nil, want refusal")
	}
	if want := "running run"; !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want it to name the running run", err.Error())
	}

	// The failed attempt must have unlatched: after stopping the run, a
	// retry succeeds.
	if err := reg.Stop(runID); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	waitForStatus(t, reg, runID, StatusStopped, 5*time.Second)

	release, err := reg.TryBeginArchive(task.ID)
	if err != nil {
		t.Fatalf("TryBeginArchive after stop: error = %v, want success (latch was released on the earlier failure)", err)
	}
	release()
}
