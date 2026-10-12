//go:build windows

package workspace

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// winPidAlive reports whether a Windows pid currently names a live
// process (OpenProcess fails on terminated pids we hold no handle to).
func winPidAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	_ = windows.CloseHandle(h)
	return true
}

// waitDescendantsGone asserts every pid in pids is gone within d.
func waitDescendantsGone(t *testing.T, pids []int, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for _, pid := range pids {
		for winPidAlive(pid) {
			if time.Now().After(deadline) {
				t.Fatalf("pid %d still alive after %s", pid, d)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// treeScript is the shared script of the kill-tree tests: it starts a
// long-running background ping (the "grandchild"), records its own and
// the ping's Windows pids to pids.txt in the worktree (MSYS `$!` is a
// cygwin pid; /proc/<pid>/winpid translates), then waits forever.
const treeScript = `ping -n 300 127.0.0.1 &
echo "$(cat /proc/$$/winpid) $(cat /proc/$!/winpid)" > pids.txt
wait`

// readTreePids parses pids.txt once the script has written it.
func readTreePids(t *testing.T, worktree string) []int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		b, err := os.ReadFile(filepath.Join(worktree, "pids.txt"))
		if err == nil {
			fields := strings.Fields(string(b))
			if len(fields) != 2 {
				t.Fatalf("pids.txt = %q, want shell and ping winpids", b)
			}
			pids := make([]int, 0, 2)
			for _, f := range fields {
				pid, err := strconv.Atoi(f)
				if err != nil || pid <= 0 {
					t.Fatalf("pids.txt = %q: bad pid %q", b, f)
				}
				pids = append(pids, pid)
			}
			return pids
		}
		if time.Now().After(deadline) {
			t.Fatalf("pids.txt not written within 5s: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// newSpacedDollarRepo creates a test git repo under a path containing
// spaces and `$`, so env-value assertions prove values are passed
// verbatim, never shell-interpreted.
func newSpacedDollarRepo(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	dir := filepath.Join(base, "sm ind $ign")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitT(t, dir, "init")
	runGitT(t, dir, "config", "core.autocrlf", "false")
	runGitT(t, dir, "config", "user.email", "test@example.com")
	runGitT(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitT(t, dir, "add", ".")
	runGitT(t, dir, "commit", "-m", "initial commit")
	return dir
}

// TestSetupScript_RunsWithEnv_Windows (AC4): the script runs under sh in
// the worktree with the SMIND_* env; values containing spaces and `$`
// arrive verbatim.
func TestSetupScript_RunsWithEnv_Windows(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	repo := newSpacedDollarRepo(t)
	w, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatal(err)
	}
	script := `echo "wt=$SMIND_WORKTREE_PATH"
echo "br=$SMIND_BRANCH"
echo "id=$SMIND_TASK_ID"
echo "src=$SMIND_SOURCE_CHECKOUT_PATH"
echo "cwd=$(pwd -W)"`
	if w, err = m.store.SetWorkspaceScripts(w.ID, &script, nil); err != nil {
		t.Fatal(err)
	}
	task, err := m.CreateTask(w.ID, nil, "envcheck")
	if err != nil {
		t.Fatal(err)
	}
	done := waitSetup(t, m, task.ID, SetupOK)
	kv := map[string]string{}
	for _, line := range strings.Split(done.SetupLog, "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			kv[k] = v
		}
	}
	if got := kv["id"]; got != strconv.FormatInt(task.ID, 10) {
		t.Errorf("SMIND_TASK_ID = %q, want %d", got, task.ID)
	}
	if got := kv["wt"]; got != *task.WorktreePath {
		t.Errorf("SMIND_WORKTREE_PATH = %q, want %q", got, *task.WorktreePath)
	}
	if got := kv["br"]; got != *task.Branch {
		t.Errorf("SMIND_BRANCH = %q, want %q", got, *task.Branch)
	}
	// Spaces and `$` in the value must arrive verbatim (never expanded).
	if got := kv["src"]; got != repo {
		t.Errorf("SMIND_SOURCE_CHECKOUT_PATH = %q, want %q", got, repo)
	}
	// sh reports its cwd as a Windows path under `pwd -W` (e.g.
	// C:/Users/...); compare slash- and case-insensitively.
	want := filepath.ToSlash(*task.WorktreePath)
	if got := kv["cwd"]; !strings.EqualFold(got, want) {
		t.Errorf("cwd = %q, want %q", got, want)
	}
}

// TestSetupScript_TimeoutKillsTree_Windows (AC4): a timeout kills the
// shell AND its background grandchild via the kill-on-close Job Object.
func TestSetupScript_TimeoutKillsTree_Windows(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	m.wt.SetupTimeout = 2 * time.Second
	w := setupWorkspace(t, m, treeScript)
	task, err := m.CreateTask(w.ID, nil, "timeout")
	if err != nil {
		t.Fatal(err)
	}
	pids := readTreePids(t, *task.WorktreePath)
	// pids.txt exists and the task may already have timed out (2 s) --
	// only assert liveness while setup is still running.
	if got, _ := m.GetTask(task.ID); got.SetupStatus == SetupRunning {
		for _, pid := range pids {
			if !winPidAlive(pid) {
				t.Fatalf("pid %d dead before the timeout", pid)
			}
		}
	}
	done := waitSetup(t, m, task.ID, SetupFailed)
	if !strings.Contains(done.SetupError, "timed out after 2s") {
		t.Fatalf("setup_error = %q, want timeout", done.SetupError)
	}
	waitDescendantsGone(t, pids, 5*time.Second)
}

// TestSetupScript_CancelKillsTree_Windows (AC4): cancelling the setup
// (archive / daemon shutdown path) kills the whole tree the same way.
func TestSetupScript_CancelKillsTree_Windows(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	w := setupWorkspace(t, m, treeScript)
	task, err := m.CreateTask(w.ID, nil, "cancel")
	if err != nil {
		t.Fatal(err)
	}
	pids := readTreePids(t, *task.WorktreePath)
	for _, pid := range pids {
		if !winPidAlive(pid) {
			t.Fatalf("pid %d dead before cancellation", pid)
		}
	}
	m.cancelSetup(task.ID)
	done := waitSetup(t, m, task.ID, SetupFailed)
	if !strings.Contains(done.SetupError, "cancelled") {
		t.Fatalf("setup_error = %q, want cancelled", done.SetupError)
	}
	waitDescendantsGone(t, pids, 5*time.Second)
}
