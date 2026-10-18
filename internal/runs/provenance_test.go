package runs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
)

// O1 provenance-capture tests. Each drives the real fake ACP agent against
// a real git worktree (newTestTask) and asserts on both the in-memory
// RunStatus and the persisted store row.

// newProvEnv builds the standard test env for provenance tests: a task
// with a real git worktree, a runner on the fake ACP agent, and a
// registry. scenario lands as the fake agent's "scenario" file (see
// newTestTask); note that any non-empty scenario leaves an untracked file
// in the worktree, i.e. a dirty start.
func newProvEnv(t *testing.T, scenario string) (*workspace.Manager, *store.Store, store.Task, *Registry, *taskrunner.Runner) {
	t.Helper()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, scenario)
	reg := newTestRegistry(t, st)
	runner := newTestRunner(wm)
	return wm, st, task, reg, runner
}

func provStart(t *testing.T, reg *Registry, wm *workspace.Manager, runner *taskrunner.Runner, task store.Task) string {
	t.Helper()
	runID, err := reg.Start(context.Background(), wm, runner, task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	return runID
}

// ignoreAgentMarkers commits a .gitignore for the fake agent's own
// bookkeeping files, so a worktree driven by the fake agent can actually
// end clean (the agent writes an untracked "session-init-method" marker in
// its cwd on every run).
func ignoreAgentMarkers(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("session-init-method\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	gitCommitAll(t, dir, "ignore fake-agent markers")
}

// gitCommitAll commits everything in dir (tracked, staged and untracked).
func gitCommitAll(t *testing.T, dir, msg string) {
	t.Helper()
	runGitT(t, dir, "add", "-A")
	runGitT(t, dir, "commit", "-m", msg)
}

// gitHeadOf returns dir's HEAD sha.
func gitHeadOf(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// TestRun_ProvenanceCapture_CommitDuringRun: a commit made while the run
// is under way (the fake agent blocks on the "hang" scenario, the test
// commits) yields head_sha != base_sha, both in the store row and in
// RunStatus.
func TestRun_ProvenanceCapture_CommitDuringRun(t *testing.T) {
	t.Parallel()
	wm, st, task, reg, runner := newProvEnv(t, "hang")
	runID := provStart(t, reg, wm, runner, task)
	waitForHistoryLen(t, reg, runID, 1, 5*time.Second) // agent under way: base already captured
	t.Cleanup(func() { _ = reg.Stop(runID) })

	base := gitHeadOf(t, *task.WorktreePath)
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "mid.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write mid.txt: %v", err)
	}
	gitCommitAll(t, *task.WorktreePath, "mid-run commit")
	head := gitHeadOf(t, *task.WorktreePath)

	_ = reg.Stop(runID)
	status := waitForStatus(t, reg, runID, StatusStopped, 5*time.Second)

	if status.BaseSHA == nil || *status.BaseSHA != base {
		t.Fatalf("BaseSHA = %v, want %q", status.BaseSHA, base)
	}
	if status.HeadSHA == nil || *status.HeadSHA != head {
		t.Fatalf("HeadSHA = %v, want %q (the mid-run commit)", status.HeadSHA, head)
	}
	if status.HeadSHA == status.BaseSHA {
		t.Fatal("HeadSHA == BaseSHA despite a mid-run commit")
	}
	row, err := st.GetRun(runID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if row.BaseSHA == nil || *row.BaseSHA != base || row.HeadSHA == nil || *row.HeadSHA != head {
		t.Fatalf("persisted provenance = %v/%v, want %q/%q", row.BaseSHA, row.HeadSHA, base, head)
	}
}

// TestRun_ProvenanceCapture_DirtyEdit: a clean run reports dirty=false; a
// run that leaves an uncommitted edit reports dirty=true at the terminal
// state.
func TestRun_ProvenanceCapture_DirtyEdit(t *testing.T) {
	t.Run("clean run is not dirty", func(t *testing.T) {
		t.Parallel()
		wm, _, task, reg, runner := newProvEnv(t, "")
		ignoreAgentMarkers(t, *task.WorktreePath)
		runID := provStart(t, reg, wm, runner, task)
		status := waitForStatus(t, reg, runID, StatusDone, 5*time.Second)
		if status.Dirty == nil || *status.Dirty {
			t.Fatalf("Dirty = %v, want false (clean worktree)", status.Dirty)
		}
		if status.BaseSHA == nil || status.HeadSHA == nil || *status.BaseSHA != *status.HeadSHA {
			t.Fatalf("shas = %v/%v, want equal and set", status.BaseSHA, status.HeadSHA)
		}
	})

	t.Run("uncommitted edit is dirty", func(t *testing.T) {
		t.Parallel()
		wm, st, task, reg, runner := newProvEnv(t, "")
		if err := os.WriteFile(filepath.Join(*task.WorktreePath, "README.md"), []byte("edited\n"), 0o644); err != nil {
			t.Fatalf("edit README: %v", err)
		}
		runID := provStart(t, reg, wm, runner, task)
		status := waitForStatus(t, reg, runID, StatusDone, 5*time.Second)
		if status.Dirty == nil || !*status.Dirty {
			t.Fatalf("Dirty = %v, want true (uncommitted edit)", status.Dirty)
		}
		row, err := st.GetRun(runID)
		if err != nil {
			t.Fatalf("GetRun() error = %v", err)
		}
		if row.Dirty == nil || !*row.Dirty {
			t.Fatalf("persisted Dirty = %v, want true", row.Dirty)
		}
	})
}

// TestRun_ProvenanceCapture_StartedDirty_EndsClean: start dirty, the
// mid-run commit cleans everything up, and the terminal capture
// overwrites dirty to false (latest successful capture wins).
func TestRun_ProvenanceCapture_StartedDirty_EndsClean(t *testing.T) {
	t.Parallel()
	wm, st, task, reg, runner := newProvEnv(t, "hang")
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "edit.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatalf("write edit.txt: %v", err)
	}
	runID := provStart(t, reg, wm, runner, task)
	waitForHistoryLen(t, reg, runID, 1, 5*time.Second)
	t.Cleanup(func() { _ = reg.Stop(runID) })

	// The run started dirty (the untracked edit); commit everything while
	// the agent blocks.
	gitCommitAll(t, *task.WorktreePath, "clean it up")

	_ = reg.Stop(runID)
	status := waitForStatus(t, reg, runID, StatusStopped, 5*time.Second)
	if status.Dirty == nil || *status.Dirty {
		t.Fatalf("Dirty = %v, want false (terminal capture overwrites the dirty start)", status.Dirty)
	}
	if status.BaseSHA == nil || status.HeadSHA == nil || *status.BaseSHA == *status.HeadSHA {
		t.Fatalf("shas = %v/%v, want both set and different", status.BaseSHA, status.HeadSHA)
	}
	row, err := st.GetRun(runID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if row.Dirty == nil || *row.Dirty {
		t.Fatalf("persisted Dirty = %v, want false", row.Dirty)
	}
}

// TestRun_ProvenanceCapture_NonGitDir: a worktree whose .git link is gone
// records NULLs, the run still finishes done, and no run event mentions
// git (a capture failure is logged, never surfaced on the run).
func TestRun_ProvenanceCapture_NonGitDir(t *testing.T) {
	t.Parallel()
	wm, st, task, reg, runner := newProvEnv(t, "")
	// A task worktree is a linked worktree: ".git" is a file pointing at
	// the repo's admin dir. Removing it makes the dir a non-git dir.
	if err := os.Remove(filepath.Join(*task.WorktreePath, ".git")); err != nil {
		t.Fatalf("remove .git link: %v", err)
	}

	runID := provStart(t, reg, wm, runner, task)
	status := waitForStatus(t, reg, runID, StatusDone, 5*time.Second)
	if status.BaseSHA != nil || status.HeadSHA != nil || status.Dirty != nil {
		t.Fatalf("provenance = %v/%v/%v, want nil/nil/nil for a non-git dir", status.BaseSHA, status.HeadSHA, status.Dirty)
	}
	row, err := st.GetRun(runID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if row.BaseSHA != nil || row.HeadSHA != nil || row.Dirty != nil {
		t.Fatalf("persisted provenance = %v/%v/%v, want nil/nil/nil", row.BaseSHA, row.HeadSHA, row.Dirty)
	}
	hist, _, err := reg.History(runID)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	for _, e := range hist {
		if strings.Contains(strings.ToLower(e.Text), "git") {
			t.Fatalf("event mentions git for a non-git dir: %+v", e)
		}
	}
}

// TestRun_ProvenanceCapture_GitMissing: git disappearing from PATH before
// the terminal capture leaves the base values intact and never affects
// the run's outcome. PATH is emptied only after the run (and its agent
// subprocess) is already under way, so the fake agent's own spawn is
// unaffected; the head capture in finish then runs against a PATH with no
// git and must record NULL, not panic or fail the run.
func TestRun_ProvenanceCapture_GitMissing(t *testing.T) {
	// Not parallel: it empties PATH for the whole process (t.Setenv).
	wm, st, task, reg, runner := newProvEnv(t, "hang")
	runID := provStart(t, reg, wm, runner, task)
	waitForHistoryLen(t, reg, runID, 1, 5*time.Second) // agent spawned, base captured with a normal PATH
	t.Cleanup(func() { _ = reg.Stop(runID) })

	base := gitHeadOf(t, *task.WorktreePath)
	t.Setenv("PATH", t.TempDir())

	_ = reg.Stop(runID)
	status := waitForStatus(t, reg, runID, StatusStopped, 5*time.Second)
	if status.Status != StatusStopped {
		t.Fatalf("status = %q, want stopped even with git missing", status.Status)
	}
	if status.BaseSHA == nil || *status.BaseSHA != base {
		t.Fatalf("BaseSHA = %v, want the start capture %q to survive", status.BaseSHA, base)
	}
	if status.HeadSHA != nil {
		t.Fatalf("HeadSHA = %v, want nil (git missing at the terminal capture)", status.HeadSHA)
	}
	if status.Dirty == nil {
		t.Fatal("Dirty = nil, want the start capture's value to survive")
	}
	row, err := st.GetRun(runID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if row.HeadSHA != nil || row.BaseSHA == nil {
		t.Fatalf("persisted = %v/%v, want base kept and head NULL", row.BaseSHA, row.HeadSHA)
	}
}

// TestRun_ProvenanceCapture_QueueDeliveredRun: a run delivered through the
// chat queue (ADR-0021's StartWhenBusy path) records provenance too.
func TestRun_ProvenanceCapture_QueueDeliveredRun(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	runA := e.startRun(t, "a")
	chatID := e.chatIDOf(t, runA)

	out, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, chatID, taskrunner.ProviderGLM, "b", taskrunner.PermissionSettings{}, "", WhenBusyQueue, store.ChatQueueSourceHuman, 0, 0)
	if err != nil {
		t.Fatalf("StartWhenBusy(queue) error = %v", err)
	}
	if !out.Queued {
		t.Fatalf("StartWhenBusy(queue) = %+v, want queued", out)
	}
	waitForStatus(t, e.reg, runA, StatusDone, 5*time.Second)

	deadline := time.Now().Add(5 * time.Second)
	var runB string
	for {
		runs := e.reg.List(chatID)
		if len(runs) >= 2 {
			runB = runs[0].ID
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no second run started for chat %d: %+v", chatID, runs)
		}
		time.Sleep(5 * time.Millisecond)
	}
	status := waitForStatus(t, e.reg, runB, StatusDone, 5*time.Second)
	if status.BaseSHA == nil || status.HeadSHA == nil || status.Dirty == nil {
		t.Fatalf("delivered run provenance = %v/%v/%v, want all captured", status.BaseSHA, status.HeadSHA, status.Dirty)
	}
}

// TestRun_ProvenanceCapture_StopMidRun: a stopped run still records its
// head sha (the terminal capture is independent of the run's cancelled
// context).
func TestRun_ProvenanceCapture_StopMidRun(t *testing.T) {
	t.Parallel()
	wm, st, task, reg, runner := newProvEnv(t, "hang")
	runID := provStart(t, reg, wm, runner, task)
	waitForHistoryLen(t, reg, runID, 1, 5*time.Second)
	t.Cleanup(func() { _ = reg.Stop(runID) })

	base := gitHeadOf(t, *task.WorktreePath)
	_ = reg.Stop(runID)
	status := waitForStatus(t, reg, runID, StatusStopped, 5*time.Second)
	if status.HeadSHA == nil || *status.HeadSHA != base {
		t.Fatalf("HeadSHA = %v, want %q (nothing changed, but captured)", status.HeadSHA, base)
	}
	row, err := st.GetRun(runID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if row.HeadSHA == nil || *row.HeadSHA != base {
		t.Fatalf("persisted HeadSHA = %v, want %q", row.HeadSHA, base)
	}
}

// TestRehydratedRun_KeepsProvenance: after a restart (a fresh Registry on
// the same store), the run still reports its provenance.
func TestRehydratedRun_KeepsProvenance(t *testing.T) {
	t.Parallel()
	wm, st, task, reg, runner := newProvEnv(t, "")
	runID := provStart(t, reg, wm, runner, task)
	live := waitForStatus(t, reg, runID, StatusDone, 5*time.Second)
	chatID := live.ChatID

	reg2, err := New(st)
	if err != nil {
		t.Fatalf("New() (rehydrate) error = %v", err)
	}
	t.Cleanup(reg2.CloseAll)

	_, status, err := reg2.History(runID)
	if err != nil {
		t.Fatalf("History() after rehydrate error = %v", err)
	}
	if status.BaseSHA == nil || *status.BaseSHA != *live.BaseSHA ||
		status.HeadSHA == nil || *status.HeadSHA != *live.HeadSHA ||
		status.Dirty == nil || *status.Dirty != *live.Dirty {
		t.Fatalf("rehydrated = %v/%v/%v, want live %v/%v/%v",
			status.BaseSHA, status.HeadSHA, status.Dirty, live.BaseSHA, live.HeadSHA, live.Dirty)
	}
	listed := reg2.List(chatID)
	if len(listed) != 1 || listed[0].BaseSHA == nil || listed[0].HeadSHA == nil || listed[0].Dirty == nil {
		t.Fatalf("List() after rehydrate = %+v, want provenance carried", listed)
	}
}

// A git that hangs (a slow fsmonitor hook, a stuck filesystem) must cost
// the run at most the capture timeout at each end: the run still finishes
// "done", with NULL provenance and no stuck "running" state.
func TestRun_ProvenanceCapture_HungGit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell shim")
	}
	// Not parallel: PATH and the package-level timeout.
	wm, st, task, reg, runner := newProvEnv(t, "")
	old := provenanceTimeout
	provenanceTimeout = 300 * time.Millisecond
	t.Cleanup(func() { provenanceTimeout = old })
	shim := t.TempDir()
	// `sleep` is a child that outlives the killed shell and ends by itself.
	if err := os.WriteFile(filepath.Join(shim, "git"), []byte("#!/bin/sh\nsleep 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shim+string(os.PathListSeparator)+os.Getenv("PATH"))

	started := time.Now()
	runID := provStart(t, reg, wm, runner, task)
	status := waitForStatus(t, reg, runID, StatusDone, 10*time.Second)
	if took := time.Since(started); took > 8*time.Second {
		t.Fatalf("run took %v with a hung git, want it bounded by the capture timeouts", took)
	}
	if status.BaseSHA != nil || status.HeadSHA != nil || status.Dirty != nil {
		t.Fatalf("provenance = %v/%v/%v, want all nil when git hangs", status.BaseSHA, status.HeadSHA, status.Dirty)
	}
	row, err := st.GetRun(runID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if row.BaseSHA != nil || row.HeadSHA != nil || row.Dirty != nil {
		t.Fatalf("persisted provenance = %v/%v/%v, want NULLs", row.BaseSHA, row.HeadSHA, row.Dirty)
	}
}

// StartWhenBusy's immediate-start branch builds its own run (not via Start):
// it must record provenance too.
func TestRun_ProvenanceCapture_StartWhenBusyImmediate(t *testing.T) {
	t.Parallel()
	wm, st, task, reg, runner := newProvEnv(t, "")
	out, err := reg.StartWhenBusy(context.Background(), wm, runner, task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "", WhenBusyReject, store.ChatQueueSourceHuman, 0, 0)
	if err != nil {
		t.Fatalf("StartWhenBusy() error = %v", err)
	}
	if out.RunID == "" {
		t.Fatalf("StartWhenBusy() = %+v, want an immediate run", out)
	}
	status := waitForStatus(t, reg, out.RunID, StatusDone, 10*time.Second)
	if status.BaseSHA == nil || status.HeadSHA == nil || status.Dirty == nil {
		t.Fatalf("provenance = %v/%v/%v, want all recorded on the immediate-start path", status.BaseSHA, status.HeadSHA, status.Dirty)
	}
	if row, err := st.GetRun(out.RunID); err != nil || row.BaseSHA == nil || row.HeadSHA == nil {
		t.Fatalf("persisted = %+v, %v; want base and head", row, err)
	}
}
