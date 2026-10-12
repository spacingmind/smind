package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

func setScripts(t *testing.T, m *Manager, ws store.Workspace, setup, teardown string) {
	t.Helper()
	if _, err := m.store.SetWorkspaceScripts(ws.ID, &setup, &teardown); err != nil {
		t.Fatal(err)
	}
}

func TestTeardown_RunsAfterCheckpointBeforeRemove(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	w := setupWorkspace(t, m, "")
	// Sh receives this path inside a POSIX script; forward slashes work on
	// every OS while native backslashes would be eaten as escapes.
	out := filepath.ToSlash(filepath.Join(t.TempDir(), "teardown.out"))
	setScripts(t, m, w, "", `{ git log -1 --format=%s; [ -d "$SMIND_WORKTREE_PATH" ] && echo present; } > `+out)
	task, err := m.CreateTask(w.ID, nil, "td")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "work.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ArchiveTask(task.ID); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("teardown did not run: %v", err)
	}
	if !strings.Contains(string(b), checkpointCommitMessage) || !strings.Contains(string(b), "present") {
		t.Fatalf("teardown saw %q, want checkpoint commit and present worktree", b)
	}
	if dirExists(*task.WorktreePath) {
		t.Fatal("worktree not removed")
	}
}

func TestTeardown_FailureDoesNotBlockArchive(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	w := setupWorkspace(t, m, "")
	setScripts(t, m, w, "", `echo teardown-broke; exit 9`)
	task, err := m.CreateTask(w.ID, nil, "tdfail")
	if err != nil {
		t.Fatal(err)
	}
	got, err := m.ArchiveTask(task.ID)
	if err != nil {
		t.Fatalf("archive blocked by teardown failure: %v", err)
	}
	if !strings.Contains(got.SetupLog, "teardown failed") || !strings.Contains(got.SetupLog, "teardown-broke") {
		t.Fatalf("setup_log = %q", got.SetupLog)
	}
	if dirExists(*task.WorktreePath) {
		t.Fatal("worktree not removed")
	}
}

func TestTeardown_NotRunWhenCheckpointFails(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	w := setupWorkspace(t, m, "")
	marker := filepath.ToSlash(filepath.Join(t.TempDir(), "ran"))
	setScripts(t, m, w, "", `touch `+marker)
	task, err := m.CreateTask(w.ID, nil, "tdcp")
	if err != nil {
		t.Fatal(err)
	}
	// Make the checkpoint fail: an untracked file plus a pre-commit hook
	// that rejects every commit.
	hooks := filepath.Join(w.Path, ".git", "hooks")
	_ = os.MkdirAll(hooks, 0o755)
	if err := os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ArchiveTask(task.ID); err == nil {
		t.Fatal("archive should fail when the checkpoint fails")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("teardown ran despite checkpoint failure")
	}
	if !dirExists(*task.WorktreePath) {
		t.Fatal("worktree removed despite checkpoint failure")
	}
}

func TestArchive_KillsRunningSetup(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	w := setupWorkspace(t, m, `sleep 300`)
	task, err := m.CreateTask(w.ID, nil, "killme")
	if err != nil {
		t.Fatal(err)
	}
	if task.SetupStatus != SetupRunning {
		t.Fatalf("status = %q", task.SetupStatus)
	}
	time.Sleep(100 * time.Millisecond)
	start := time.Now()
	got, err := m.ArchiveTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatalf("archive took %s", time.Since(start))
	}
	if got.SetupStatus != SetupFailed || !strings.Contains(got.SetupError, "cancelled") {
		t.Fatalf("task = %+v", got)
	}
	m.setupMu.Lock()
	n := len(m.setupRuns)
	m.setupMu.Unlock()
	if n != 0 {
		t.Fatalf("%d setup runs still tracked", n)
	}
}

func TestSetup_RetryRunsAgain(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	w := setupWorkspace(t, m, `[ -f OK ] || { echo first-fail; exit 1; }; echo second-ok`)
	task, err := m.CreateTask(w.ID, nil, "retry")
	if err != nil {
		t.Fatal(err)
	}
	waitSetup(t, m, task.ID, SetupFailed)
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "OK"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	re, err := m.RetrySetup(task.ID)
	if err != nil || re.SetupStatus != SetupRunning {
		t.Fatalf("retry = %+v err=%v", re, err)
	}
	done := waitSetup(t, m, task.ID, SetupOK)
	if !strings.Contains(done.SetupLog, "second-ok") || done.SetupError != "" {
		t.Fatalf("task = %+v", done)
	}
}

func TestSetup_RetryWhileRunningRejected(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	w := setupWorkspace(t, m, `sleep 300`)
	task, err := m.CreateTask(w.ID, nil, "retryrun")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.RetrySetup(task.ID); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("err = %v", err)
	}
	m.CancelSetups()
	// No script: also an error.
	w2 := setupWorkspace(t, m, "")
	t2, _ := m.CreateTask(w2.ID, nil, "noscript")
	if _, err := m.RetrySetup(t2.ID); err == nil {
		t.Fatal("retry with no script should error")
	}
}

func TestSetup_RetryConcurrentOnlyOneRuns(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	// Forward slashes: the path is embedded in a POSIX script (backslashes
	// would be eaten as shell escapes on Windows).
	runLog := filepath.ToSlash(filepath.Join(t.TempDir(), "runs.log"))
	w := setupWorkspace(t, m, `echo run >> `+runLog+`; sleep 1`)
	task, err := m.CreateTask(w.ID, nil, "race")
	if err != nil {
		t.Fatal(err)
	}
	waitSetup(t, m, task.ID, SetupOK)
	if err := os.Remove(runLog); err != nil {
		t.Fatal(err)
	}

	const n = 8
	var wg sync.WaitGroup
	var ok, rejected atomic.Int32
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := m.RetrySetup(task.ID); err == nil {
				ok.Add(1)
			} else if strings.Contains(err.Error(), "already running") {
				rejected.Add(1)
			} else {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if ok.Load() != 1 || rejected.Load() != n-1 {
		t.Fatalf("ok=%d rejected=%d, want 1 and %d", ok.Load(), rejected.Load(), n-1)
	}
	waitSetup(t, m, task.ID, SetupOK)
	b, _ := os.ReadFile(runLog)
	if got := strings.Count(string(b), "run"); got != 1 {
		t.Fatalf("script ran %d times, want 1", got)
	}
}
