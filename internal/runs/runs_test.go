package runs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

func newTestStore(t *testing.T) *store.Store {
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
	return s
}

// newTestWorkspaceManager also returns the underlying store, since a
// Registry started against a task from wm must persist against that same
// store -- runs.task_id references tasks(id), enforced via foreign_keys
// pragma (see store.sqliteDSN).
func newTestWorkspaceManager(t *testing.T) (*workspace.Manager, *store.Store) {
	t.Helper()
	s := newTestStore(t)
	return workspace.New(s), s
}

func newTestRegistry(t *testing.T, st *store.Store) *Registry {
	t.Helper()
	reg, err := New(st)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return reg
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
	return taskrunner.New(wm, taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath}))
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

// waitForPermissionRequest polls runID's history for its first
// taskrunner.EventTypePermissionRequest event, returning it once found.
func waitForPermissionRequest(t *testing.T, reg *Registry, runID string, timeout time.Duration) taskrunner.Event {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		hist, _, err := reg.History(runID)
		if err != nil {
			t.Fatalf("History(%q) error = %v", runID, err)
		}
		for _, e := range hist {
			if e.Type == taskrunner.EventTypePermissionRequest {
				return e
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for run %q to record a permission request, history so far: %+v", runID, hist)
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
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

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
	wm, st := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

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
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

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
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

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
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

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
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

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
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

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
	reg := newTestRegistry(t, newTestStore(t))
	if err := reg.Stop("no-such-run"); err != ErrNotFound {
		t.Fatalf("Stop() error = %v, want ErrNotFound", err)
	}
}

// TestRegistry_Unsubscribe_DoesNotStopTheRun proves detaching a subscriber
// (calling unsubscribe) is unrelated to Stop: the run keeps going, and a
// later History/Subscribe on it still works normally.
func TestRegistry_Unsubscribe_DoesNotStopTheRun(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

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
	wm, st := newTestWorkspaceManager(t)
	taskA := newTestTask(t, wm, "")
	taskB := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

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
	wm, st := newTestWorkspaceManager(t)
	taskDone := newTestTask(t, wm, "")
	taskHang := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

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

// TestRegistry_PermissionRequest_AppearsInHistoryBlocksThenRespondPermissionUnblocks
// proves the full pending-permission bridge end to end: the fake agent's
// "permission" scenario issues a real session/request_permission call,
// which appears as a taskrunner.EventTypePermissionRequest in the run's
// history (the same path run.attach/run.logs read from) with the options it
// offered; the run stays running (not just "hasn't gotten around to
// finishing yet" -- confirmed by a real wait below) until
// RespondPermission answers it; and once answered, the blocked Decide call
// unblocks with that exact choice, which the fake agent echoes back in its
// final chunk, and a matching EventTypePermissionResolved is recorded
// after the request in history.
func TestRegistry_PermissionRequest_AppearsInHistoryBlocksThenRespondPermissionUnblocks(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "permission")
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

	runID, err := reg.Start(context.Background(), wm, runner, task.ID, taskrunner.ProviderGLM, "hi")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	req := waitForPermissionRequest(t, reg, runID, 5*time.Second)
	if req.PermissionRequestID == "" {
		t.Fatal("PermissionRequestID is empty")
	}
	if req.PermissionSummary != "Run a risky command" {
		t.Fatalf("PermissionSummary = %q, want %q", req.PermissionSummary, "Run a risky command")
	}
	wantOpts := []taskrunner.PermissionOption{
		{ID: "allow-1", Label: "Allow", Kind: "allow_once"},
		{ID: "deny-1", Label: "Deny", Kind: "reject_once"},
	}
	if len(req.PermissionOptions) != 2 || req.PermissionOptions[0] != wantOpts[0] || req.PermissionOptions[1] != wantOpts[1] {
		t.Fatalf("PermissionOptions = %+v, want %+v", req.PermissionOptions, wantOpts)
	}

	// Confirm the run is genuinely still blocked on the request, not just
	// not-yet-finished: give it a real moment and check it hasn't gone
	// terminal on its own.
	time.Sleep(100 * time.Millisecond)
	_, status, err := reg.History(runID)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if status.Status != StatusRunning {
		t.Fatalf("Status = %q before answering the permission request, want still %q", status.Status, StatusRunning)
	}

	if err := reg.RespondPermission(runID, req.PermissionRequestID, "allow-1"); err != nil {
		t.Fatalf("RespondPermission() error = %v", err)
	}

	status = waitForStatus(t, reg, runID, StatusDone, 5*time.Second)
	if status.StopReason != "end_turn" {
		t.Fatalf("StopReason = %q, want %q", status.StopReason, "end_turn")
	}

	hist, _, err := reg.History(runID)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	var sawResolvedAfterRequest, sawFinalChunk bool
	requestIdx, resolvedIdx := -1, -1
	for i, e := range hist {
		switch {
		case e.Type == taskrunner.EventTypePermissionRequest:
			requestIdx = i
		case e.Type == taskrunner.EventTypePermissionResolved:
			resolvedIdx = i
			if e.PermissionRequestID != req.PermissionRequestID {
				t.Fatalf("resolved event's PermissionRequestID = %q, want %q", e.PermissionRequestID, req.PermissionRequestID)
			}
			if e.PermissionOptionID != "allow-1" {
				t.Fatalf("resolved event's PermissionOptionID = %q, want %q", e.PermissionOptionID, "allow-1")
			}
			sawResolvedAfterRequest = requestIdx >= 0 && resolvedIdx > requestIdx
		case e.Type == taskrunner.EventTypeText && e.Text == "chose:allow-1":
			sawFinalChunk = true
		}
	}
	if !sawResolvedAfterRequest {
		t.Fatalf("EventTypePermissionResolved did not appear after EventTypePermissionRequest in history: %+v", hist)
	}
	if !sawFinalChunk {
		t.Fatalf("history does not include the agent's post-decision chunk reflecting the chosen option: %+v", hist)
	}
}

// TestRegistry_RespondPermission_DoubleAnswer_SecondCallIsAClearError proves
// answering the same request id twice is a clear, distinct error on the
// second call -- not a panic, not a silent no-op, and not delivered to a
// decider that already moved on.
func TestRegistry_RespondPermission_DoubleAnswer_SecondCallIsAClearError(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "permission")
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

	runID, err := reg.Start(context.Background(), wm, runner, task.ID, taskrunner.ProviderGLM, "hi")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	req := waitForPermissionRequest(t, reg, runID, 5*time.Second)

	if err := reg.RespondPermission(runID, req.PermissionRequestID, "allow-1"); err != nil {
		t.Fatalf("first RespondPermission() error = %v, want nil", err)
	}
	if err := reg.RespondPermission(runID, req.PermissionRequestID, "deny-1"); err == nil {
		t.Fatal("second RespondPermission() for the same request id: error = nil, want a clear error")
	}

	// The run must still have honored only the first answer.
	waitForStatus(t, reg, runID, StatusDone, 5*time.Second)
	hist, _, err := reg.History(runID)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	for _, e := range hist {
		if e.Type == taskrunner.EventTypeText && e.Text == "chose:deny-1" {
			t.Fatalf("run reflects the second (rejected) answer, want only the first: %+v", hist)
		}
	}
}

// TestRegistry_RespondPermission_UnknownRequestID_IsAClearError proves
// answering a request id the Registry never issued (for this run, or at
// all) is a clear error, not a panic or silent no-op.
func TestRegistry_RespondPermission_UnknownRequestID_IsAClearError(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "permission")
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

	runID, err := reg.Start(context.Background(), wm, runner, task.ID, taskrunner.ProviderGLM, "hi")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitForPermissionRequest(t, reg, runID, 5*time.Second)

	if err := reg.RespondPermission(runID, "no-such-request", "allow-1"); err == nil {
		t.Fatal("RespondPermission() with an unknown request id: error = nil, want a clear error")
	}

	// Clean up: the run is still blocked on the real pending request.
	if err := reg.Stop(runID); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

// TestRegistry_RespondPermission_UnknownRun_ReturnsErrNotFound proves
// RespondPermission on a run ID the Registry has never heard of behaves
// like Stop/History/Subscribe on an unknown ID: ErrNotFound, not a panic.
func TestRegistry_RespondPermission_UnknownRun_ReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	reg := newTestRegistry(t, newTestStore(t))
	if err := reg.RespondPermission("no-such-run", "req-1", "allow-1"); err != ErrNotFound {
		t.Fatalf("RespondPermission() error = %v, want ErrNotFound", err)
	}
}

// TestRegistry_Stop_WhilePermissionPending_Unblocks proves stopping a run
// while a permission request is still pending actually unblocks the
// provider's blocked Decide call (rather than hanging forever) and leaves
// no goroutine behind -- ACP's own session/request_permission dispatch
// runs with context.Background(), not anything derived from the run's own
// ctx, so this only passes if runPermissionDecider's extra select on the
// run's own ctx (see registry.go) is actually wired up.
func TestRegistry_Stop_WhilePermissionPending_Unblocks(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "permission")
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

	// Let the runtime settle from whatever earlier parallel subtests /
	// prior GC activity are still winding down, then take a goroutine-count
	// baseline immediately before starting the run under test.
	runtime.GC()
	time.Sleep(20 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	runID, err := reg.Start(context.Background(), wm, runner, task.ID, taskrunner.ProviderGLM, "hi")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	req := waitForPermissionRequest(t, reg, runID, 5*time.Second)
	if req.PermissionRequestID == "" {
		t.Fatal("PermissionRequestID is empty")
	}

	start := time.Now()
	if err := reg.Stop(runID); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	status := waitForStatus(t, reg, runID, StatusStopped, 5*time.Second)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("Stop() took %s to unblock the pending permission request, want promptly", elapsed)
	}
	if status.FinishedAt == nil {
		t.Fatal("FinishedAt is nil, want set once stopped")
	}

	// Answering the now-abandoned request must be a clear error, not a
	// silent success into a channel nobody will ever read again.
	if err := reg.RespondPermission(runID, req.PermissionRequestID, "allow-1"); err == nil {
		t.Fatal("RespondPermission() on a request abandoned by Stop: error = nil, want a clear error")
	}

	// Goroutine-leak check: poll for the count to settle back down near
	// baseline instead of asserting immediately, since the decider
	// goroutine (and the subprocess's own Close/Wait) unblock asynchronously
	// relative to Stop() returning.
	deadline := time.Now().Add(5 * time.Second)
	for {
		runtime.GC()
		time.Sleep(20 * time.Millisecond)
		current := runtime.NumGoroutine()
		if current <= baseline+2 { // small slack for unrelated background goroutines
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutine count did not settle back to baseline after Stop(): baseline=%d current=%d", baseline, current)
		}
	}
}

// TestRegistry_CloseAll_StopsEveryRunningRunAndWaitsForThem proves CloseAll
// (used at daemon shutdown -- see internal/server.Server.Close) actually
// terminates every still-running run.start-originated Run, not just asks
// them to stop: it must return only once each one's subprocess has really
// exited (StatusStopped, not still StatusRunning) -- the same "gone, not
// just asked to go" guarantee internal/terminal.Registry.CloseAll already
// gives its own callers, proven here for internal/runs the same way.
func TestRegistry_CloseAll_StopsEveryRunningRunAndWaitsForThem(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

	const n = 3
	runIDs := make([]string, n)
	for i := 0; i < n; i++ {
		task := newTestTask(t, wm, "hang")
		runID, err := reg.Start(context.Background(), wm, runner, task.ID, taskrunner.ProviderGLM, "hi")
		if err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		runIDs[i] = runID
	}

	for _, id := range runIDs {
		waitForHistoryLen(t, reg, id, 1, 5*time.Second) // confirm each turn is genuinely under way, not just registered
	}

	start := time.Now()
	reg.CloseAll()
	elapsed := time.Since(start)
	if elapsed > 5*time.Second {
		t.Fatalf("CloseAll() took %s to stop %d runs, want promptly", elapsed, n)
	}

	for _, id := range runIDs {
		_, status, err := reg.History(id)
		if err != nil {
			t.Fatalf("History(%q) error = %v", id, err)
		}
		if status.Status != StatusStopped {
			t.Fatalf("run %q status after CloseAll() = %q, want %q (CloseAll must wait for the subprocess to actually exit, not just signal it)", id, status.Status, StatusStopped)
		}
		if status.FinishedAt == nil {
			t.Fatalf("run %q FinishedAt is nil after CloseAll(), want set", id)
		}
	}
}

// TestRegistry_CloseAll_NoRunningRuns_ReturnsImmediately proves CloseAll is
// a cheap no-op when there's nothing to stop -- it must not block on
// already-finished or never-started runs.
func TestRegistry_CloseAll_NoRunningRuns_ReturnsImmediately(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

	task := newTestTask(t, wm, "")
	runID, err := reg.Start(context.Background(), wm, runner, task.ID, taskrunner.ProviderGLM, "hi")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitForStatus(t, reg, runID, StatusDone, 5*time.Second)

	start := time.Now()
	reg.CloseAll()
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("CloseAll() with no running runs took %s, want near-instant", elapsed)
	}

	_, status, err := reg.History(runID)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if status.Status != StatusDone {
		t.Fatalf("already-finished run's status changed by CloseAll(): %q, want still %q", status.Status, StatusDone)
	}
}

// TestRegistry_Start_PersistsRunRowImmediately proves a run's row is
// queryable directly via the store as soon as Start returns -- before the
// turn does anything -- so a run recorded as started is durable even if the
// daemon dies immediately after.
func TestRegistry_Start_PersistsRunRowImmediately(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

	runID, err := reg.Start(context.Background(), wm, runner, task.ID, taskrunner.ProviderGLM, "hi")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = reg.Stop(runID) })

	row, err := st.GetRun(runID)
	if err != nil {
		t.Fatalf("store.GetRun(%q) error = %v", runID, err)
	}
	if row.TaskID != task.ID || row.Provider != string(taskrunner.ProviderGLM) || row.Prompt != "hi" {
		t.Fatalf("persisted run row = %+v", row)
	}
	if row.Status != string(StatusRunning) {
		t.Fatalf("persisted run row Status = %q, want %q", row.Status, StatusRunning)
	}
}

// TestRegistry_Record_PersistsEventsInOrder proves every event a run emits
// is persisted, in the same order Registry.History reports them, readable
// directly via the store (not just through the Registry's in-memory view).
func TestRegistry_Record_PersistsEventsInOrder(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

	runID, err := reg.Start(context.Background(), wm, runner, task.ID, taskrunner.ProviderGLM, "hi")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitForStatus(t, reg, runID, StatusDone, 5*time.Second)

	memHist, _, err := reg.History(runID)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if len(memHist) == 0 {
		t.Fatal("History() returned no events, nothing to compare persistence against")
	}

	persisted, err := st.ListRunEvents(runID)
	if err != nil {
		t.Fatalf("store.ListRunEvents() error = %v", err)
	}
	if len(persisted) != len(memHist) {
		t.Fatalf("persisted event count = %d, want %d (matching in-memory History)", len(persisted), len(memHist))
	}
	for i, pe := range persisted {
		if pe.Seq != int64(i) {
			t.Fatalf("persisted event %d Seq = %d, want %d", i, pe.Seq, i)
		}
		decoded, err := decodeEvent(pe.EventData)
		if err != nil {
			t.Fatalf("decodeEvent(%d) error = %v", i, err)
		}
		if decoded.Type != memHist[i].Type || decoded.Text != memHist[i].Text || decoded.StopReason != memHist[i].StopReason {
			t.Fatalf("persisted event %d = %+v, want it to match in-memory event %+v", i, decoded, memHist[i])
		}
	}
}

// TestRegistry_Finish_PersistsTerminalStatus proves both a normal
// completion and a deliberate Stop persist their terminal status/
// finished_at/stop_reason, not just the in-memory RunStatus.
func TestRegistry_Finish_PersistsTerminalStatus(t *testing.T) {
	t.Parallel()

	t.Run("done", func(t *testing.T) {
		t.Parallel()
		wm, st := newTestWorkspaceManager(t)
		task := newTestTask(t, wm, "")
		runner := newTestRunner(wm)
		reg := newTestRegistry(t, st)

		runID, err := reg.Start(context.Background(), wm, runner, task.ID, taskrunner.ProviderGLM, "hi")
		if err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		waitForStatus(t, reg, runID, StatusDone, 5*time.Second)

		row, err := st.GetRun(runID)
		if err != nil {
			t.Fatalf("store.GetRun() error = %v", err)
		}
		if row.Status != string(StatusDone) {
			t.Fatalf("persisted Status = %q, want %q", row.Status, StatusDone)
		}
		if row.FinishedAt == nil {
			t.Fatal("persisted FinishedAt is nil, want set")
		}
		if row.StopReason == "" {
			t.Fatal("persisted StopReason is empty, want set")
		}
	})

	t.Run("stopped", func(t *testing.T) {
		t.Parallel()
		wm, st := newTestWorkspaceManager(t)
		task := newTestTask(t, wm, "hang")
		runner := newTestRunner(wm)
		reg := newTestRegistry(t, st)

		runID, err := reg.Start(context.Background(), wm, runner, task.ID, taskrunner.ProviderGLM, "hi")
		if err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		waitForHistoryLen(t, reg, runID, 1, 5*time.Second)
		if err := reg.Stop(runID); err != nil {
			t.Fatalf("Stop() error = %v", err)
		}
		waitForStatus(t, reg, runID, StatusStopped, 5*time.Second)

		row, err := st.GetRun(runID)
		if err != nil {
			t.Fatalf("store.GetRun() error = %v", err)
		}
		if row.Status != string(StatusStopped) {
			t.Fatalf("persisted Status = %q, want %q", row.Status, StatusStopped)
		}
		if row.FinishedAt == nil {
			t.Fatal("persisted FinishedAt is nil, want set")
		}
	})
}

// TestRegistry_RestartSimulation_HistorySurvivesAcrossRegistries proves the
// whole point of this persistence layer: a run driven to completion by one
// Registry is fully visible (List + History, identical content) from a
// brand-new Registry built later against the same store -- simulating a
// daemon restart, since a fresh process only ever constructs a fresh
// Registry.
func TestRegistry_RestartSimulation_HistorySurvivesAcrossRegistries(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	reg1 := newTestRegistry(t, st)

	runID, err := reg1.Start(context.Background(), wm, runner, task.ID, taskrunner.ProviderGLM, "hi")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitForStatus(t, reg1, runID, StatusDone, 5*time.Second)
	wantHist, wantStatus, err := reg1.History(runID)
	if err != nil {
		t.Fatalf("History() on reg1 error = %v", err)
	}

	// reg1 is simply discarded here (no Close/CloseAll) -- nothing about
	// Registry itself is persistent; only what went through st is.
	reg2 := newTestRegistry(t, st)

	gotHist, gotStatus, err := reg2.History(runID)
	if err != nil {
		t.Fatalf("History() on reg2 (rehydrated) error = %v", err)
	}
	if gotStatus.Status != wantStatus.Status || gotStatus.TaskID != wantStatus.TaskID || gotStatus.Provider != wantStatus.Provider {
		t.Fatalf("rehydrated RunStatus = %+v, want %+v", gotStatus, wantStatus)
	}
	if len(gotHist) != len(wantHist) {
		t.Fatalf("rehydrated History len = %d, want %d", len(gotHist), len(wantHist))
	}
	for i := range wantHist {
		if gotHist[i].Type != wantHist[i].Type || gotHist[i].Text != wantHist[i].Text || gotHist[i].StopReason != wantHist[i].StopReason {
			t.Fatalf("rehydrated History[%d] = %+v, want %+v", i, gotHist[i], wantHist[i])
		}
	}

	summaries := reg2.List()
	found := false
	for _, s := range summaries {
		if s.ID == runID {
			found = true
		}
	}
	if !found {
		t.Fatalf("List() on rehydrated Registry = %+v, want it to include run %q", summaries, runID)
	}

	// A rehydrated run behaves like any other finished run: Subscribe
	// immediately backfills and closes.
	events, unsubscribe, err := reg2.Subscribe(runID)
	if err != nil {
		t.Fatalf("Subscribe() on rehydrated run error = %v", err)
	}
	defer unsubscribe()
	n := 0
	for range events {
		n++
	}
	if n != len(wantHist) {
		t.Fatalf("Subscribe() on rehydrated run delivered %d events, want %d", n, len(wantHist))
	}
}

// TestRegistry_InterruptedReconciliation_MarksStaleRunningRunsInterrupted
// proves a run left at status "running" in the store (simulating a daemon
// crash -- no CloseAll, no graceful Stop) comes back as StatusInterrupted,
// not StatusRunning, the next time a Registry is built against that store.
func TestRegistry_InterruptedReconciliation_MarksStaleRunningRunsInterrupted(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	reg1 := newTestRegistry(t, st)

	runID, err := reg1.Start(context.Background(), wm, runner, task.ID, taskrunner.ProviderGLM, "hi")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitForHistoryLen(t, reg1, runID, 1, 5*time.Second)

	// Simulate a crash: reg1 (and its live subprocess) is abandoned without
	// Stop/CloseAll ever running, so the persisted row is left "running".
	// Kill the actual fakeagent subprocess directly so it doesn't linger
	// past the test.
	t.Cleanup(func() { _ = reg1.Stop(runID) })

	row, err := st.GetRun(runID)
	if err != nil {
		t.Fatalf("store.GetRun() before reconciliation error = %v", err)
	}
	if row.Status != string(StatusRunning) {
		t.Fatalf("precondition: persisted Status = %q, want %q", row.Status, StatusRunning)
	}

	reg2 := newTestRegistry(t, st)

	_, status, err := reg2.History(runID)
	if err != nil {
		t.Fatalf("History() on reg2 error = %v", err)
	}
	if status.Status != StatusInterrupted {
		t.Fatalf("rehydrated Status = %q, want %q", status.Status, StatusInterrupted)
	}

	reconciled, err := st.GetRun(runID)
	if err != nil {
		t.Fatalf("store.GetRun() after reconciliation error = %v", err)
	}
	if reconciled.Status != string(StatusInterrupted) {
		t.Fatalf("persisted Status after reconciliation = %q, want %q", reconciled.Status, StatusInterrupted)
	}
}
