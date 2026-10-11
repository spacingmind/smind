package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/config"
)

// newTaskRefEnv creates the shared starting point for the ref-mode tests:
// a workspace repo ("develop") whose origin is a bare repo, with a second
// commit pushed to origin/develop that the local checkout deliberately
// does NOT have (its HEAD stays at the first commit). baseRef tasks must
// land on the second commit, proving they really resolved the ref rather
// than the checkout's HEAD.
func newTaskRefEnv(t *testing.T) (m *Manager, workspaceID int64, repo, bare, baseSHA, headSHA string) {
	t.Helper()
	repo, bare = newRepoWithOrigin(t)
	runGitT(t, repo, "config", "pull.rebase", "false")
	// origin/develop moves ahead without the local checkout following.
	headSHA = strings.TrimSpace(runGitOutputT(t, repo, "rev-parse", "HEAD"))
	writeAndCommit(t, repo, "second.txt", "second commit\n", "second")
	runGitT(t, repo, "push", "origin", "develop")
	baseSHA = strings.TrimSpace(runGitOutputT(t, repo, "rev-parse", "HEAD"))
	runGitT(t, repo, "reset", "--hard", headSHA)

	m = newTestManager(t)
	w, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	return m, w.ID, repo, bare, baseSHA, headSHA
}

// worktreeHead returns the worktree's current HEAD commit.
func worktreeHead(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(runGitOutputT(t, dir, "rev-parse", "HEAD"))
}

// taskRefState is the (task count, worktree dir count) baseline a test
// captures before a CreateTask it expects to fail, so the post-failure
// assertion is immune to leftovers from earlier tests in the same shared
// SMIND_HOME.
type taskRefState struct {
	tasks int
	dirs  int
}

func snapshotTaskRefState(t *testing.T, m *Manager, workspaceID int64) taskRefState {
	t.Helper()
	tasks, err := m.ListTasks(workspaceID)
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(config.Dir(), "worktrees", fmt.Sprint(workspaceID)))
	return taskRefState{tasks: len(tasks), dirs: len(entries)}
}

// assertNoTaskLeftBehind asserts the failure contract: a refused CreateTask
// added no task row and no worktree directory relative to before.
func assertNoTaskLeftBehind(t *testing.T, m *Manager, workspaceID int64, before taskRefState) {
	t.Helper()
	after := snapshotTaskRefState(t, m, workspaceID)
	if after.tasks != before.tasks {
		t.Fatalf("task count = %d, want unchanged %d (no row may survive a failed create)", after.tasks, before.tasks)
	}
	if after.dirs != before.dirs {
		t.Fatalf("worktree dir count = %d, want unchanged %d (no directory may survive a failed create)", after.dirs, before.dirs)
	}
}

func TestManager_CreateTask_BaseRef(t *testing.T) {
	m, wsID, _, _, baseSHA, headSHA := newTaskRefEnv(t)

	task, err := m.CreateTask(wsID, nil, "Branch off develop", WithBaseRef("origin/develop"))
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if got := worktreeHead(t, *task.WorktreePath); got != baseSHA {
		t.Fatalf("worktree HEAD = %s, want the origin/develop commit %s (checkout HEAD is %s)", got, baseSHA, headSHA)
	}
	if !strings.HasPrefix(*task.Branch, "smind/task-") {
		t.Fatalf("branch = %q, want a smind/task-<slug> branch", *task.Branch)
	}
}

func TestManager_CreateTask_BaseRefFetchFailureFallsBack(t *testing.T) {
	m, wsID, repo, bare, baseSHA, _ := newTaskRefEnv(t)

	// Make origin unreachable: repoint the remote at a nonexistent path.
	// The bounded best-effort fetch must fail without failing the create,
	// falling back to the (stale but present) remote-tracking ref.
	runGitT(t, repo, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "no-such-origin"))

	task, err := m.CreateTask(wsID, nil, "Fallback", WithBaseRef("origin/develop"))
	if err != nil {
		t.Fatalf("CreateTask() error = %v, want fallback to the existing remote-tracking ref", err)
	}
	if got := worktreeHead(t, *task.WorktreePath); got != baseSHA {
		t.Fatalf("worktree HEAD = %s, want the previously fetched origin/develop commit %s", got, baseSHA)
	}
	_ = bare
}

func TestManager_CreateTask_BaseRefRejectsOptionLikeInput(t *testing.T) {
	m, wsID, repo, _, _, _ := newTaskRefEnv(t)
	before := snapshotTaskRefState(t, m, wsID)

	// A sentinel that must never be created: gitWorktreeAddFrom would have
	// to actually run --upload-pack as an option for it to appear.
	touchPath := filepath.Join(t.TempDir(), "pwned")

	for _, ref := range []string{"-x", "--upload-pack=touch " + touchPath, "a b", "a..b"} {
		_, err := m.CreateTask(wsID, nil, "Bad ref", WithBaseRef(ref))
		if err == nil {
			t.Fatalf("CreateTask(baseRef=%q) error = nil, want rejection", ref)
		}
		if _, statErr := os.Stat(touchPath); statErr == nil {
			t.Fatalf("baseRef=%q created %s -- a git option was executed", ref, touchPath)
		}
	}
	assertNoTaskLeftBehind(t, m, wsID, before)
	if len(runGitOutputT(t, repo, "worktree", "list", "--porcelain")) == 0 {
		t.Fatal("unexpected empty worktree list")
	}
}

func TestManager_CreateTask_BaseRefUnknown(t *testing.T) {
	m, wsID, _, _, _, _ := newTaskRefEnv(t)
	before := snapshotTaskRefState(t, m, wsID)

	_, err := m.CreateTask(wsID, nil, "Unknown ref", WithBaseRef("origin/no-such-branch"))
	if err == nil {
		t.Fatal("CreateTask(unknown baseRef) error = nil, want an error")
	}
	if !strings.Contains(err.Error(), "does not resolve") {
		t.Fatalf("error = %q, want it to say the ref does not resolve", err.Error())
	}
	assertNoTaskLeftBehind(t, m, wsID, before)
}

func TestManager_CreateTask_CheckoutExistingBranch(t *testing.T) {
	m, wsID, repo, _, _, _ := newTaskRefEnv(t)
	runGitT(t, repo, "branch", "feat/x")

	task, err := m.CreateTask(wsID, nil, "Existing branch", WithExistingBranch("feat/x"))
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if *task.Branch != "feat/x" {
		t.Fatalf("branch = %q, want feat/x (no new branch)", *task.Branch)
	}
	if got := runGitOutputT(t, *task.WorktreePath, "rev-parse", "--abbrev-ref", "HEAD"); strings.TrimSpace(got) != "feat/x" {
		t.Fatalf("worktree HEAD = %q, want feat/x", got)
	}
}

func TestManager_CreateTask_CheckoutBranchAlreadyCheckedOut(t *testing.T) {
	m, wsID, repo, _, _, _ := newTaskRefEnv(t)
	before := snapshotTaskRefState(t, m, wsID)
	runGitT(t, repo, "branch", "feat/busy")
	// feat/busy is checked out in a real second worktree of the same repo,
	// so git must refuse to check it out again in yet another worktree.
	other := filepath.Join(t.TempDir(), "other-wt")
	runGitT(t, repo, "worktree", "add", other, "feat/busy")
	t.Cleanup(func() { runGitT(t, repo, "worktree", "remove", "--force", other) })

	_, err := m.CreateTask(wsID, nil, "Busy branch", WithExistingBranch("feat/busy"))
	if err == nil {
		t.Fatal("CreateTask(already checked out branch) error = nil, want git's refusal")
	}
	if !strings.Contains(err.Error(), "already checked out") && !strings.Contains(err.Error(), "already used by worktree") {
		t.Fatalf("error = %q, want git's 'already checked out' reason", err.Error())
	}
	assertNoTaskLeftBehind(t, m, wsID, before)
}

func TestManager_CreateTask_FromPR(t *testing.T) {
	m, wsID, repo, bare, _, _ := newTaskRefEnv(t)

	// Build a real "PR head" in the bare origin under refs/pull/7/head.
	prSHA := strings.TrimSpace(runGitOutputT(t, repo, "rev-parse", "HEAD"))
	runGitPushRef(t, repo, bare, prSHA, "refs/pull/7/head")

	fetches := &[]string{}
	orig := fetchPRHead
	fetchPRHead = func(dir, remote string, n int) error {
		*fetches = append(*fetches, fmt.Sprintf("%s %s %d", dir, remote, n))
		return orig(dir, remote, n)
	}
	t.Cleanup(func() { fetchPRHead = orig })

	task, err := m.CreateTask(wsID, nil, "From PR seven", WithPullRequest(7))
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if !strings.HasPrefix(*task.Branch, "smind/pr-7-") {
		t.Fatalf("branch = %q, want smind/pr-7-<slug>", *task.Branch)
	}
	if got := worktreeHead(t, *task.WorktreePath); got != prSHA {
		t.Fatalf("worktree HEAD = %s, want the PR head commit %s", got, prSHA)
	}
	if len(*fetches) != 1 || (*fetches)[0] != repo+" origin 7" {
		t.Fatalf("fetchPRHead calls = %v, want exactly one fetch of origin PR 7 in %s", *fetches, repo)
	}
}

func TestManager_CreateTask_FromPRInvalid(t *testing.T) {
	m, wsID, _, _, _, _ := newTaskRefEnv(t)
	if _, err := m.CreateTask(wsID, nil, "Zero", WithPullRequest(0)); err == nil {
		t.Fatal("CreateTask(prNumber=0) error = nil, want rejection (a supplied prNumber must be > 0)")
	} else if !strings.Contains(err.Error(), "prNumber must be > 0") {
		t.Fatalf("error = %q, want 'prNumber must be > 0'", err.Error())
	}

	_, err := m.CreateTask(wsID, nil, "Negative", WithPullRequest(-1))
	if err == nil {
		t.Fatal("CreateTask(prNumber=-1) error = nil, want rejection")
	}

	_, err = m.CreateTask(wsID, nil, "Missing PR", WithPullRequest(999))
	if err == nil {
		t.Fatal("CreateTask(missing PR) error = nil, want a clear fetch error")
	}
	if !strings.Contains(err.Error(), "GitHub") {
		t.Fatalf("error = %q, want it to name the GitHub-only requirement", err.Error())
	}
	assertNoTaskLeftBehind(t, m, wsID, snapshotTaskRefState(t, m, wsID))
}

func TestManager_CreateTask_RefModesMutuallyExclusive(t *testing.T) {
	m, wsID, _, _, _, _ := newTaskRefEnv(t)
	before := snapshotTaskRefState(t, m, wsID)

	cases := []struct {
		name string
		opts []CreateTaskOption
	}{
		{"baseRef+branch", []CreateTaskOption{WithBaseRef("origin/develop"), WithExistingBranch("develop")}},
		{"baseRef+pr", []CreateTaskOption{WithBaseRef("origin/develop"), WithPullRequest(7)}},
		{"branch+pr", []CreateTaskOption{WithExistingBranch("develop"), WithPullRequest(7)}},
		{"all three", []CreateTaskOption{WithBaseRef("origin/develop"), WithExistingBranch("develop"), WithPullRequest(7)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := m.CreateTask(wsID, nil, "Too many modes", tc.opts...)
			if err == nil {
				t.Fatalf("CreateTask(%s) error = nil, want rejection", tc.name)
			}
			if !strings.Contains(err.Error(), "at most one") {
				t.Fatalf("error = %q, want the mutual-exclusion message", err.Error())
			}
		})
	}
	assertNoTaskLeftBehind(t, m, wsID, before)
}

// runGitPushRef pushes sha to ref on the bare remote, used to fabricate a
// refs/pull/<n>/head ref locally (no GitHub involved).
func runGitPushRef(t *testing.T, dir, bare, sha, ref string) {
	t.Helper()
	runGitT(t, dir, "push", bare, sha+":"+ref)
}
