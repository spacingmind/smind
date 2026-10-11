package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

func setupWorkspace(t *testing.T, m *Manager, script string) store.Workspace {
	t.Helper()
	repo := newTestRepo(t)
	w, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatal(err)
	}
	if script != "" {
		if w, err = m.store.SetWorkspaceScripts(w.ID, &script, nil); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

func waitSetup(t *testing.T, m *Manager, id int64, want string) store.Task {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		task, err := m.GetTask(id)
		if err != nil {
			t.Fatal(err)
		}
		if task.SetupStatus == want {
			return task
		}
		if time.Now().After(deadline) {
			t.Fatalf("task %d setup_status = %q, want %q (err=%q log=%q)", id, task.SetupStatus, want, task.SetupError, task.SetupLog)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSetup_AsyncCreateReturnsRunning(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	w := setupWorkspace(t, m, `sleep 1; echo "id=$SMIND_TASK_ID ws=$SMIND_WORKSPACE_ID" > setup.out; echo done`)
	start := time.Now()
	task, err := m.CreateTask(w.ID, nil, "async")
	if err != nil {
		t.Fatal(err)
	}
	if task.SetupStatus != SetupRunning {
		t.Fatalf("status = %q, want running", task.SetupStatus)
	}
	if time.Since(start) > 900*time.Millisecond {
		t.Fatalf("CreateTask blocked for %s", time.Since(start))
	}
	done := waitSetup(t, m, task.ID, SetupOK)
	if done.SetupLog == "" {
		t.Fatal("empty setup log")
	}
	if _, err := os.Stat(filepath.Join(*task.WorktreePath, "setup.out")); err != nil {
		t.Fatalf("script did not run in worktree: %v", err)
	}
}

func TestSetup_CreateTaskFailureKeepsWorktree(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	w := setupWorkspace(t, m, `echo boom; exit 4`)
	task, err := m.CreateTask(w.ID, nil, "fails")
	if err != nil {
		t.Fatal(err)
	}
	failed := waitSetup(t, m, task.ID, SetupFailed)
	if failed.SetupError == "" || failed.SetupLog == "" {
		t.Fatalf("task = %+v", failed)
	}
	if !dirExists(*task.WorktreePath) {
		t.Fatal("worktree removed after failed setup")
	}
}

func TestSetup_NoScriptLeavesStatusEmpty(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	w := setupWorkspace(t, m, "")
	task, err := m.CreateTask(w.ID, nil, "plain")
	if err != nil || task.SetupStatus != "" {
		t.Fatalf("task = %+v err=%v", task, err)
	}
}

func TestSetup_DaemonRestartMarksFailed(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	w := setupWorkspace(t, m, "")
	task, err := m.CreateTask(w.ID, nil, "restart")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.store.UpdateTaskSetup(task.ID, SetupRunning, "", "partial"); err != nil {
		t.Fatal(err)
	}
	m2 := New(m.store) // simulated daemon start
	got, err := m2.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SetupStatus != SetupFailed || got.SetupError != "interrupted by daemon restart" {
		t.Fatalf("task = %+v", got)
	}
}

func TestSetup_WindowsUnsupported(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	m.goos = "windows"
	w := setupWorkspace(t, m, "echo hi")
	if _, err := m.CreateTask(w.ID, nil, "win"); err == nil {
		t.Fatal("expected error")
	} else if !errors.Is(err, ErrSetupUnsupported) {
		t.Fatalf("err = %v", err)
	}
	tasks, _ := m.store.ListTasksByWorkspace(w.ID)
	if len(tasks) != 0 {
		t.Fatalf("task row created: %v", tasks)
	}
	// No script: unaffected.
	w2 := setupWorkspace(t, m, "")
	if _, err := m.CreateTask(w2.ID, nil, "ok"); err != nil {
		t.Fatal(err)
	}
}

func TestSetup_RepoFileIgnored(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	repo := newTestRepo(t)
	for _, f := range []string{"smind.json", "paseo.json"} {
		body := `{"worktree":{"setup":["touch REPO_SENTINEL"]},"setup":"touch REPO_SENTINEL"}`
		if err := os.WriteFile(filepath.Join(repo, f), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGitT(t, repo, "add", ".")
	runGitT(t, repo, "commit", "-m", "add configs")
	w, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatal(err)
	}
	task, err := m.CreateTask(w.ID, nil, "ignored")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	got, _ := m.GetTask(task.ID)
	if got.SetupStatus != "" {
		t.Fatalf("setup_status = %q", got.SetupStatus)
	}
	if _, err := os.Stat(filepath.Join(*task.WorktreePath, "REPO_SENTINEL")); err == nil {
		t.Fatal("repo file commands were executed")
	}
}
