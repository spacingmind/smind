package runs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
)

// fakeACPAgentPath is the compiled internal/taskrunner/fakeagent binary,
// reused here (see internal/wsapi's own test setup for the same pattern)
// so these tests can drive a real Runner.RunPrompt call end to end without
// depending on npx/network access.
var fakeACPAgentPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "smind-runs-test-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	fakeACPAgentPath = filepath.Join(dir, "fakeagent")
	build := exec.Command("go", "build", "-o", fakeACPAgentPath, "../taskrunner/fakeagent")
	build.Dir, err = os.Getwd()
	if err != nil {
		panic(err)
	}
	if out, err := build.CombinedOutput(); err != nil {
		panic(err.Error() + ": " + string(out))
	}

	smindHome, err := os.MkdirTemp("", "smind-runs-home-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(smindHome)
	os.Setenv("SMIND_HOME", smindHome)

	os.Exit(m.Run())
}

func newTestWorkspaceManager(t *testing.T) *workspace.Manager {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("store.Close() error = %v", err)
		}
	})
	return workspace.New(s)
}

func newTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	runGitT(t, dir, "init")
	runGitT(t, dir, "config", "user.email", "test@example.com")
	runGitT(t, dir, "config", "user.name", "Test")

	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGitT(t, dir, "add", "README.md")
	runGitT(t, dir, "commit", "-m", "initial commit")

	return dir
}

func runGitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

// newTestTask creates a workspace and a task with a real git worktree, and
// (if scenario is non-empty) writes it as the "scenario" file inside the
// worktree for the fake agent to read -- see internal/taskrunner/fakeagent.
func newTestTask(t *testing.T, wm *workspace.Manager, scenario string) store.Task {
	t.Helper()

	repo := newTestRepo(t)
	ws, err := wm.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	task, err := wm.CreateTask(ws.ID, nil, "Task")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	if scenario != "" {
		if err := os.WriteFile(filepath.Join(*task.WorktreePath, "scenario"), []byte(scenario), 0o644); err != nil {
			t.Fatalf("write scenario file: %v", err)
		}
	}

	return task
}

func newTestRunner(wm *workspace.Manager) *taskrunner.Runner {
	return taskrunner.New(wm, taskrunner.WithACPCommand([]string{fakeACPAgentPath}))
}

// drainToClose reads events off ch until it closes, with a timeout, and
// returns everything received.
func drainToClose(t *testing.T, ch <-chan Event, timeout time.Duration) []Event {
	t.Helper()
	var got []Event
	deadline := time.After(timeout)
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return got
			}
			got = append(got, e)
		case <-deadline:
			t.Fatalf("timed out after %s waiting for events to close, got %d so far: %+v", timeout, len(got), got)
		}
	}
}

func waitForStatus(t *testing.T, reg *Registry, runID string, want Status, timeout time.Duration) RunStatus {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		_, status, err := reg.History(runID)
		if err != nil {
			t.Fatalf("History(%q) error = %v", runID, err)
		}
		if status.Status == want {
			return status
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for run %q to reach status %q, still %q", runID, want, status.Status)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitForHistoryLen(t *testing.T, reg *Registry, runID string, n int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		hist, _, err := reg.History(runID)
		if err != nil {
			t.Fatalf("History(%q) error = %v", runID, err)
		}
		if len(hist) >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for run %q to have >= %d history events, has %d", runID, n, len(hist))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRegistry_Start_RunsToCompletion(t *testing.T) {
	t.Parallel()
	wm := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	reg := New()

	runID, err := reg.Start(context.Background(), wm, runner, task.ID, taskrunner.ProviderGLM, "hi")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	status := waitForStatus(t, reg, runID, StatusDone, 5*time.Second)
	if status.StopReason != "end_turn" {
		t.Fatalf("StopReason = %q, want %q", status.StopReason, "end_turn")
	}
	if status.TaskID != task.ID {
		t.Fatalf("TaskID = %d, want %d", status.TaskID, task.ID)
	}
	if status.FinishedAt == nil {
		t.Fatal("FinishedAt is nil, want set on a done run")
	}

	hist, _, err := reg.History(runID)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if len(hist) != 3 {
		t.Fatalf("got %d history events, want 3: %+v", len(hist), hist)
	}
	if hist[2].Type != taskrunner.EventTypeDone {
		t.Fatalf("last event = %+v, want EventTypeDone", hist[2])
	}
}

func TestRegistry_Start_UnknownTask_FailsSynchronously(t *testing.T) {
	t.Parallel()
	wm := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	reg := New()

	_, err := reg.Start(context.Background(), wm, runner, 999999, taskrunner.ProviderGLM, "hi")
	if err == nil {
		t.Fatal("Start() error = nil, want error for unknown task")
	}
	if len(reg.List()) != 0 {
		t.Fatalf("List() = %v, want no runs registered after a failed Start", reg.List())
	}
}

// TestRegistry_History_OnRunningRun_ReturnsWithoutBlocking proves History
// returns whatever has been recorded so far for a still-running run
// instead of waiting for it to finish -- the "hang" scenario blocks for an
// hour after its first chunk, so this only passes if History didn't wait
// for that.
func TestRegistry_History_OnRunningRun_ReturnsWithoutBlocking(t *testing.T) {
	t.Parallel()
	wm := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	reg := New()

	runID, err := reg.Start(context.Background(), wm, runner, task.ID, taskrunner.ProviderGLM, "hi")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = reg.Stop(runID) })

	waitForHistoryLen(t, reg, runID, 1, 5*time.Second)

	hist, status, err := reg.History(runID)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if status.Status != StatusRunning {
		t.Fatalf("Status = %q, want %q", status.Status, StatusRunning)
	}
	if len(hist) != 1 || hist[0].Text != "before hang" {
		t.Fatalf("history = %+v, want one chunk %q", hist, "before hang")
	}
}

// TestRegistry_Subscribe_MidRunAttach_BackfillThenLive proves a subscriber
// joining mid-run gets backfilled with what already happened, then the
// remaining live events, in order, ending with the channel closing once
// the run goes terminal.
func TestRegistry_Subscribe_MidRunAttach_BackfillThenLive(t *testing.T) {
	t.Parallel()
	wm := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	reg := New()

	runID, err := reg.Start(context.Background(), wm, runner, task.ID, taskrunner.ProviderGLM, "hi")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Give the run a moment's head start so there's a real chance some
	// history exists before Subscribe -- not required for correctness
	// (Subscribe is correct at any point per its own doc comment), but
	// makes this test actually exercise the backfill path rather than
	// just the all-live path.
	waitForHistoryLen(t, reg, runID, 1, 5*time.Second)

	events, unsubscribe, err := reg.Subscribe(runID)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer unsubscribe()

	got := drainToClose(t, events, 5*time.Second)
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3: %+v", len(got), got)
	}
	if got[0].Text != "Hello, " || got[1].Text != "world!" {
		t.Fatalf("got = %+v, want texts %q then %q", got, "Hello, ", "world!")
	}
	if got[2].Type != taskrunner.EventTypeDone {
		t.Fatalf("got[2] = %+v, want EventTypeDone", got[2])
	}

	status := waitForStatus(t, reg, runID, StatusDone, time.Second)
	if status.StopReason != "end_turn" {
		t.Fatalf("StopReason = %q, want %q", status.StopReason, "end_turn")
	}
}

// TestRegistry_Subscribe_AlreadyFinished proves attaching to a run that's
// already terminal by the time Subscribe is called still gets its full
// history, then an immediately-closed channel -- not a hang waiting for
// events that will never come.
func TestRegistry_Subscribe_AlreadyFinished(t *testing.T) {
	t.Parallel()
	wm := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	reg := New()

	runID, err := reg.Start(context.Background(), wm, runner, task.ID, taskrunner.ProviderGLM, "hi")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitForStatus(t, reg, runID, StatusDone, 5*time.Second)

	events, unsubscribe, err := reg.Subscribe(runID)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer unsubscribe()

	got := drainToClose(t, events, time.Second)
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3: %+v", len(got), got)
	}
}

// TestRegistry_Stop_FromAnyCaller_CancelsTheRun proves Stop actually
// cancels a running turn (subprocess killed, run reaches StatusStopped)
// with no notion of "which connection started it" baked into Registry
// itself -- Stop just takes a run ID.
func TestRegistry_Stop_FromAnyCaller_CancelsTheRun(t *testing.T) {
	t.Parallel()
	wm := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	reg := New()

	runID, err := reg.Start(context.Background(), wm, runner, task.ID, taskrunner.ProviderGLM, "hi")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitForHistoryLen(t, reg, runID, 1, 5*time.Second)

	start := time.Now()
	if err := reg.Stop(runID); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	status := waitForStatus(t, reg, runID, StatusStopped, 5*time.Second)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("Stop() took %s to take effect, want well under the fake agent's 1h hang", elapsed)
	}
	if status.FinishedAt == nil {
		t.Fatal("FinishedAt is nil, want set once stopped")
	}
}

func TestRegistry_Stop_AlreadyFinished_IsNotAnError(t *testing.T) {
	t.Parallel()
	wm := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	reg := New()

	runID, err := reg.Start(context.Background(), wm, runner, task.ID, taskrunner.ProviderGLM, "hi")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitForStatus(t, reg, runID, StatusDone, 5*time.Second)

	if err := reg.Stop(runID); err != nil {
		t.Fatalf("Stop() on a finished run: error = %v, want nil", err)
	}
	_, status, err := reg.History(runID)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if status.Status != StatusDone {
		t.Fatalf("Status = %q after Stop() on a finished run, want unchanged %q", status.Status, StatusDone)
	}
}

func TestRegistry_Stop_UnknownRun_ReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	reg := New()
	if err := reg.Stop("no-such-run"); err != ErrNotFound {
		t.Fatalf("Stop() error = %v, want ErrNotFound", err)
	}
}

// TestRegistry_Unsubscribe_DoesNotStopTheRun proves detaching a subscriber
// (calling unsubscribe) is unrelated to Stop: the run keeps going, and a
// later History/Subscribe on it still works normally.
func TestRegistry_Unsubscribe_DoesNotStopTheRun(t *testing.T) {
	t.Parallel()
	wm := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	reg := New()

	runID, err := reg.Start(context.Background(), wm, runner, task.ID, taskrunner.ProviderGLM, "hi")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = reg.Stop(runID) })

	events, unsubscribe, err := reg.Subscribe(runID)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	select {
	case <-events:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the first backfilled/live event")
	}
	unsubscribe()

	// Give the (nonexistent, since detaching doesn't stop anything) stop
	// signal time to have taken effect if it wrongly had one.
	time.Sleep(50 * time.Millisecond)
	_, status, err := reg.History(runID)
	if err != nil {
		t.Fatalf("History() after unsubscribe: error = %v", err)
	}
	if status.Status != StatusRunning {
		t.Fatalf("Status = %q after unsubscribe, want still %q", status.Status, StatusRunning)
	}

	events2, unsubscribe2, err := reg.Subscribe(runID)
	if err != nil {
		t.Fatalf("Subscribe() after prior unsubscribe: error = %v", err)
	}
	defer unsubscribe2()
	select {
	case _, ok := <-events2:
		if !ok {
			t.Fatal("events2 closed immediately, want the still-running run's backfill")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out re-subscribing after a prior unsubscribe")
	}
}

// TestRegistry_TwoRuns_DoNotCrossContaminate proves two concurrently
// running Runs keep fully independent history/subscriber state.
func TestRegistry_TwoRuns_DoNotCrossContaminate(t *testing.T) {
	t.Parallel()
	wm := newTestWorkspaceManager(t)
	taskA := newTestTask(t, wm, "")
	taskB := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	reg := New()

	runA, err := reg.Start(context.Background(), wm, runner, taskA.ID, taskrunner.ProviderGLM, "hi a")
	if err != nil {
		t.Fatalf("Start(A) error = %v", err)
	}
	runB, err := reg.Start(context.Background(), wm, runner, taskB.ID, taskrunner.ProviderGLM, "hi b")
	if err != nil {
		t.Fatalf("Start(B) error = %v", err)
	}
	if runA == runB {
		t.Fatalf("Start() returned the same run ID twice: %q", runA)
	}

	waitForStatus(t, reg, runA, StatusDone, 5*time.Second)
	waitForStatus(t, reg, runB, StatusDone, 5*time.Second)

	histA, statusA, err := reg.History(runA)
	if err != nil {
		t.Fatalf("History(A) error = %v", err)
	}
	histB, statusB, err := reg.History(runB)
	if err != nil {
		t.Fatalf("History(B) error = %v", err)
	}
	if len(histA) != 3 || len(histB) != 3 {
		t.Fatalf("len(histA) = %d, len(histB) = %d, want 3 and 3", len(histA), len(histB))
	}
	if statusA.TaskID != taskA.ID || statusB.TaskID != taskB.ID {
		t.Fatalf("statusA.TaskID = %d, statusB.TaskID = %d, want %d and %d", statusA.TaskID, statusB.TaskID, taskA.ID, taskB.ID)
	}
}

func TestRegistry_List_ReflectsCurrentStatus(t *testing.T) {
	t.Parallel()
	wm := newTestWorkspaceManager(t)
	taskDone := newTestTask(t, wm, "")
	taskHang := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	reg := New()

	runDone, err := reg.Start(context.Background(), wm, runner, taskDone.ID, taskrunner.ProviderGLM, "hi")
	if err != nil {
		t.Fatalf("Start(done) error = %v", err)
	}
	waitForStatus(t, reg, runDone, StatusDone, 5*time.Second)

	runStopped, err := reg.Start(context.Background(), wm, runner, taskHang.ID, taskrunner.ProviderGLM, "hi")
	if err != nil {
		t.Fatalf("Start(hang) error = %v", err)
	}
	waitForHistoryLen(t, reg, runStopped, 1, 5*time.Second)
	if err := reg.Stop(runStopped); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	waitForStatus(t, reg, runStopped, StatusStopped, 5*time.Second)

	byID := map[string]RunSummary{}
	for _, s := range reg.List() {
		byID[s.ID] = s
	}
	if got := byID[runDone].Status; got != StatusDone {
		t.Fatalf("List()[done].Status = %q, want %q", got, StatusDone)
	}
	if got := byID[runStopped].Status; got != StatusStopped {
		t.Fatalf("List()[stopped].Status = %q, want %q", got, StatusStopped)
	}
}
