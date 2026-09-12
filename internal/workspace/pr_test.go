package workspace

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// newBareRepo creates a bare git repository in a temp dir -- a stand-in for
// a real "origin" remote. Pushing to it is a real, local (no-network) git
// push, matching how every other test in this package exercises git for
// real rather than mocking it.
func newBareRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitT(t, dir, "init", "--bare")
	return dir
}

// newRepoWithOrigin creates a real repo (newTestRepo) with its default
// branch renamed to "develop" (matching defaultBaseBranch), an "origin"
// remote pointing at a fresh bare repo, and "develop" already pushed there
// -- the shared clean-base starting point every CreatePR test builds on.
func newRepoWithOrigin(t *testing.T) (repo, bare string) {
	t.Helper()
	repo = newTestRepo(t)
	runGitT(t, repo, "branch", "-M", "develop")
	bare = newBareRepo(t)
	runGitT(t, repo, "remote", "add", "origin", bare)
	runGitT(t, repo, "push", "origin", "develop")
	return repo, bare
}

// ghCall records one recorded invocation of the stubbed runGH seam.
type ghCall struct {
	dir  string
	args []string
}

// stubGH replaces the package-level runGH seam for the duration of the
// test (restored via t.Cleanup) with fn, and returns a pointer to the slice
// every call gets appended to. Not run in parallel with other stubGH tests
// in this package, since runGH is process-global state.
func stubGH(t *testing.T, fn func(dir string, args ...string) (string, error)) *[]ghCall {
	t.Helper()
	calls := &[]ghCall{}
	orig := runGH
	runGH = func(dir string, args ...string) (string, error) {
		*calls = append(*calls, ghCall{dir: dir, args: args})
		return fn(dir, args...)
	}
	t.Cleanup(func() { runGH = orig })
	return calls
}

// remoteBranchSHA returns bare's refs/heads/<branch> commit, or "" if the
// branch doesn't exist there.
func remoteBranchSHA(t *testing.T, bare, branch string) string {
	t.Helper()
	cmd := exec.Command("git", "--git-dir", bare, "rev-parse", "refs/heads/"+branch)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return ""
	}
	return strings.TrimSpace(stdout.String())
}

// remoteHasPath reports whether path exists in bare's branch at HEAD --
// used to prove the diverged-base path replays only the task's own
// commits, not whatever else was sitting on the branch it forked from.
func remoteHasPath(t *testing.T, bare, branch, path string) bool {
	t.Helper()
	cmd := exec.Command("git", "--git-dir", bare, "show", "refs/heads/"+branch+":"+path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	return cmd.Run() == nil
}

func writeAndCommit(t *testing.T, dir, path, content, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	runGitT(t, dir, "add", path)
	runGitT(t, dir, "commit", "-m", message)
}

// TestManager_CreatePR_CleanBase covers the happy path: the task branch's
// fork point is already an ancestor of the (freshly-fetched) base branch,
// so CreatePR pushes the task branch directly and opens the PR from it.
func TestManager_CreatePR_CleanBase(t *testing.T) {
	repo, bare := newRepoWithOrigin(t)
	m := newTestManager(t)
	w, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	task, err := m.CreateTask(w.ID, nil, "Add feature")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	writeAndCommit(t, *task.WorktreePath, "feature.txt", "feature work\n", "add feature")

	calls := stubGH(t, func(_ string, _ ...string) (string, error) {
		return "https://github.com/example/repo/pull/1\n", nil
	})

	result, err := m.CreatePR(task.ID, "")
	if err != nil {
		t.Fatalf("CreatePR() error = %v", err)
	}
	if result.URL != "https://github.com/example/repo/pull/1" {
		t.Fatalf("CreatePR().URL = %q, want the trimmed gh output", result.URL)
	}

	if len(*calls) != 1 {
		t.Fatalf("gh calls = %+v, want exactly 1", *calls)
	}
	call := (*calls)[0]
	wantArgs := []string{"pr", "create", "--base", "develop", "--head", *task.Branch, "--title", "Add feature", "--body",
		"Opened from smind task #" + strconv.FormatInt(task.ID, 10) + " via task.createPr."}
	if !equalArgs(call.args, wantArgs) {
		t.Fatalf("gh args = %v, want %v", call.args, wantArgs)
	}

	// The task branch itself (not some clean equivalent) was pushed to
	// origin, since the base hadn't diverged.
	localSHA := strings.TrimSpace(runGitOutputT(t, *task.WorktreePath, "rev-parse", "HEAD"))
	if remoteBranchSHA(t, bare, *task.Branch) != localSHA {
		t.Fatalf("origin's %s = %q, want it to match the local branch tip %q", *task.Branch, remoteBranchSHA(t, bare, *task.Branch), localSHA)
	}
}

// TestManager_CreatePR_DivergedBase covers Session 1's own edge case: the
// workspace checkout carried an unrelated, not-yet-pushed commit at the
// moment the task's worktree was created, so the task branch's fork point
// is not an ancestor of the base. CreatePR must cherry-pick only the
// task's own commit(s) onto a clean smind/pr-<id> branch rather than
// opening a PR stuffed with the unrelated history.
func TestManager_CreatePR_DivergedBase(t *testing.T) {
	repo, bare := newRepoWithOrigin(t)
	// Simulate Session 1's gap: the workspace repo's checkout gains a real
	// commit that is never pushed to origin before a task worktree forks
	// off it.
	writeAndCommit(t, repo, "unrelated.txt", "unrelated, unpushed work\n", "unrelated work not yet pushed")

	m := newTestManager(t)
	w, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	task, err := m.CreateTask(w.ID, nil, "Fix bug")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	writeAndCommit(t, *task.WorktreePath, "taskwork.txt", "the task's own change\n", "add taskwork")

	calls := stubGH(t, func(_ string, _ ...string) (string, error) {
		return "https://github.com/example/repo/pull/2\n", nil
	})

	result, err := m.CreatePR(task.ID, "")
	if err != nil {
		t.Fatalf("CreatePR() error = %v", err)
	}
	if result.URL != "https://github.com/example/repo/pull/2" {
		t.Fatalf("CreatePR().URL = %q", result.URL)
	}

	wantHead := "smind/pr-" + strconv.FormatInt(task.ID, 10)
	if len(*calls) != 1 {
		t.Fatalf("gh calls = %+v, want exactly 1", *calls)
	}
	call := (*calls)[0]
	if !containsFlagValue(call.args, "--head", wantHead) {
		t.Fatalf("gh args = %v, want --head %q", call.args, wantHead)
	}
	if !containsFlagValue(call.args, "--base", "develop") {
		t.Fatalf("gh args = %v, want --base develop", call.args)
	}

	// The clean branch pushed to origin carries the task's own change...
	if !remoteHasPath(t, bare, wantHead, "taskwork.txt") {
		t.Fatalf("origin's %s is missing taskwork.txt", wantHead)
	}
	// ...but not the unrelated commit the task branch's fork point carried.
	if remoteHasPath(t, bare, wantHead, "unrelated.txt") {
		t.Fatalf("origin's %s contains unrelated.txt -- unrelated history leaked into the PR branch", wantHead)
	}
	// The task's own branch itself was never pushed as-is.
	if remoteBranchSHA(t, bare, *task.Branch) != "" {
		t.Fatalf("origin has the raw task branch %s pushed; only the clean %s branch should have been", *task.Branch, wantHead)
	}
}

// TestManager_CreatePR_GHFailure proves a gh failure (e.g. Session 1's own
// live 429) surfaces as a descriptive error rather than being swallowed,
// even though the git push side of the operation already succeeded.
func TestManager_CreatePR_GHFailure(t *testing.T) {
	repo, bare := newRepoWithOrigin(t)
	m := newTestManager(t)
	w, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	task, err := m.CreateTask(w.ID, nil, "Add feature")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	writeAndCommit(t, *task.WorktreePath, "feature.txt", "feature work\n", "add feature")

	stubGH(t, func(_ string, _ ...string) (string, error) {
		return "", errFake429
	})

	result, err := m.CreatePR(task.ID, "")
	if err == nil {
		t.Fatalf("CreatePR() error = nil, want the gh failure surfaced")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Fatalf("CreatePR() error = %q, want it to mention the gh failure (429)", err.Error())
	}
	if result.URL != "" {
		t.Fatalf("CreatePR() result = %+v, want a zero value on failure", result)
	}

	// The push itself is a separate git step that already ran (and
	// succeeded) before gh was invoked -- proving the failure is gh's
	// alone, not a symptom of the push never having happened.
	localSHA := strings.TrimSpace(runGitOutputT(t, *task.WorktreePath, "rev-parse", "HEAD"))
	if remoteBranchSHA(t, bare, *task.Branch) != localSHA {
		t.Fatalf("origin's %s = %q, want it to match the local branch tip %q even though gh failed",
			*task.Branch, remoteBranchSHA(t, bare, *task.Branch), localSHA)
	}
}

var errFake429 = fakeErr("gh: HTTP 429: rate limited")

type fakeErr string

func (e fakeErr) Error() string { return string(e) }

func equalArgs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func containsFlagValue(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}
