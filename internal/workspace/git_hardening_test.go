package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// hostileFsmonitorRepo returns a repo whose config makes git execute a script
// (touching the returned marker) for any command that consults fsmonitor.
func hostileFsmonitorRepo(t *testing.T) (repo, marker string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs a /bin/sh hook")
	}
	repo = newTestRepo(t)
	marker = filepath.Join(t.TempDir(), "marker")
	hook := filepath.Join(t.TempDir(), "hook.sh")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ntouch "+marker+"\nprintf '\\0'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	runGitT(t, repo, "config", "core.fsmonitor", hook)
	return repo, marker
}

// The daemon's diff path must not execute an agent-editable core.fsmonitor.
func TestGitRunner_HostileFsmonitorNotExecuted(t *testing.T) {
	repo, marker := hostileFsmonitorRepo(t)
	head := strings.TrimSpace(runGitOutputT(t, repo, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := taskDiffStat(repo, head); err != nil {
		t.Fatalf("taskDiffStat() error = %v", err)
	}
	if _, err := runGitOutput(repo, "status", "--porcelain"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("core.fsmonitor hook ran: marker file exists")
	}
}

func TestManager_TaskDiffStat_BaseUnknown(t *testing.T) {
	m, wsID, _, _, _, _ := newTaskRefEnv(t)
	task, err := m.CreateTask(wsID, nil, "Stat")
	if err != nil {
		t.Fatal(err)
	}
	wt := *getTask(t, m, task.ID).WorktreePath
	if err := os.WriteFile(filepath.Join(wt, "a.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, ins, del, err := m.TaskDiffStat(task.ID)
	if err != nil || files != 1 || ins != 2 || del != 0 {
		t.Fatalf("TaskDiffStat = %d/%d/%d, %v; want 1/2/0, nil", files, ins, del, err)
	}

	if err := m.store.SetTaskBase(task.ID, "develop", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := m.TaskDiffStat(task.ID); !errors.Is(err, ErrBaseUnknown) {
		t.Fatalf("err = %v, want ErrBaseUnknown", err)
	}
}
