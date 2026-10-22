package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/forge"
	"github.com/spacingmind/smind/internal/store"
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
// branch renamed to "develop" (the workspace default base), an "origin"
// remote pointing at a fresh bare repo, and "develop" already pushed there
// -- the shared clean-base starting point every PR test builds on.
func newRepoWithOrigin(t *testing.T) (repo, bare string) {
	t.Helper()
	repo = newTestRepo(t)
	runGitT(t, repo, "branch", "-M", "develop")
	bare = newBareRepo(t)
	runGitT(t, repo, "remote", "add", "origin", bare)
	runGitT(t, repo, "push", "origin", "develop")
	return repo, bare
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

func containsFlagValue(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

// prTask is a workspace over repo (origin = a local bare repo) with one task.
func prTask(t *testing.T) (m *Manager, task store.Task, repo, bare string) {
	t.Helper()
	repo, bare = newRepoWithOrigin(t)
	m = newTestManager(t)
	w, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	task, err = m.CreateTask(w.ID, nil, "Add feature")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	return m, task, repo, bare
}

// divergedPRTask builds the cherry-pick scenario: an unpushed commit on the
// checkout, a task forked from it (its base pinned to that fork point), and
// no task commit yet.
func divergedPRTask(t *testing.T) (m *Manager, task store.Task, repo, bare string) {
	t.Helper()
	repo, bare = newRepoWithOrigin(t)
	writeAndCommit(t, repo, "unrelated.txt", "unpushed\n", "unrelated")
	m = newTestManager(t)
	w, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatal(err)
	}
	task, err = m.CreateTask(w.ID, nil, "Diverged")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.store.SetTaskBase(task.ID, "develop", worktreeHead(t, *task.WorktreePath)); err != nil {
		t.Fatal(err)
	}
	return m, task, repo, bare
}

// wantPRCode asserts err is a *forge.PRError with the wire code.
func wantPRCode(t *testing.T, err error, code string) *forge.PRError {
	t.Helper()
	var pe *forge.PRError
	if !errors.As(err, &pe) {
		t.Fatalf("error = %v (%T), want *forge.PRError %s", err, err, code)
	}
	if pe.Code != code {
		t.Fatalf("error code = %q (%v), want %q", pe.Code, pe, code)
	}
	return pe
}

func localBranchExists(t *testing.T, repo, name string) bool {
	t.Helper()
	return strings.TrimSpace(runGitOutputT(t, repo, "branch", "--list", name)) != ""
}

func TestPRPreview_CommitsAndStrategyDirect(t *testing.T) {
	m, task, repo, _ := prTask(t)
	writeAndCommit(t, *task.WorktreePath, "a.txt", "a\n", "first change")
	writeAndCommit(t, *task.WorktreePath, "b.txt", "b\n", "second change")
	// Uncommitted work is not a commit.
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "wip.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	info, err := m.PRPreview(context.Background(), task.ID, "")
	if err != nil {
		t.Fatalf("PRPreview() error = %v", err)
	}
	if info.Base != "develop" || info.Branch != *task.Branch || info.Head != *task.Branch || info.Strategy != PRStrategyDirect {
		t.Fatalf("info = %+v", info)
	}
	if want := strings.TrimSpace(runGitOutputT(t, repo, "rev-parse", "refs/remotes/origin/develop")); info.BaseSha != want {
		t.Fatalf("BaseSha = %q, want origin/develop %q", info.BaseSha, want)
	}
	if info.CommitCount != 2 || len(info.Commits) != 2 || info.Commits[0].Subject != "first change" || info.Commits[1].Subject != "second change" {
		t.Fatalf("commits = %+v (count %d), want oldest first", info.Commits, info.CommitCount)
	}
	if want := worktreeHead(t, *task.WorktreePath); info.Commits[1].SHA != want {
		t.Fatalf("last commit sha = %q, want HEAD %q", info.Commits[1].SHA, want)
	}
}

func TestPRPreview_CommitLimitKeepsOldestAndFullCount(t *testing.T) {
	m, task, _, _ := prTask(t)
	for i := 1; i <= 55; i++ {
		runGitT(t, *task.WorktreePath, "commit", "--allow-empty", "-m", fmt.Sprintf("c%d", i))
	}
	info, err := m.PRPreview(context.Background(), task.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if info.CommitCount != 55 || len(info.Commits) != 50 || info.Commits[0].Subject != "c1" || info.Commits[49].Subject != "c50" {
		t.Fatalf("count %d, listed %d, first %q last %q; want 55, 50, c1, c50", info.CommitCount, len(info.Commits),
			info.Commits[0].Subject, info.Commits[len(info.Commits)-1].Subject)
	}
}

func TestPRPreview_DivergedBaseIsCherryPickAndNeverFetches(t *testing.T) {
	m, task, _, bare := divergedPRTask(t)
	writeAndCommit(t, *task.WorktreePath, "tw.txt", "t\n", "task work")
	// With the remote gone, a preview that fetched would fail.
	if err := os.RemoveAll(bare); err != nil {
		t.Fatal(err)
	}
	info, err := m.PRPreview(context.Background(), task.ID, "")
	if err != nil {
		t.Fatalf("PRPreview() error = %v (a preview must not touch the network)", err)
	}
	if info.Strategy != PRStrategyCherryPick || info.Head != "smind/pr-task-"+strconv.FormatInt(task.ID, 10) || info.Branch != *task.Branch {
		t.Fatalf("info = %+v, want cherry-pick via smind/pr-task-<id>", info)
	}
	if info.CommitCount != 1 || info.Commits[0].Subject != "task work" {
		t.Fatalf("commits = %+v, want only the task's own", info.Commits)
	}
}

func TestPRPreview_InvalidBaseAndNoWorktree(t *testing.T) {
	m, task, _, _ := prTask(t)
	_, err := m.PRPreview(context.Background(), task.ID, "bad..name")
	wantPRCode(t, err, forge.CodeInvalidBranch)
	_, err = m.PRPreview(context.Background(), task.ID, "-oops")
	wantPRCode(t, err, forge.CodeInvalidBranch)
	if err := m.store.SetTaskBase(task.ID, "develop", ""); err != nil {
		t.Fatal(err)
	}
	_, err = m.PRPreview(context.Background(), task.ID, "")
	wantPRCode(t, err, forge.CodeBaseUnknown)
}

func TestPushForPR_CleanBase_PushesTaskBranch(t *testing.T) {
	m, task, _, bare := prTask(t)
	writeAndCommit(t, *task.WorktreePath, "feature.txt", "feature work\n", "add feature")

	got, err := m.PushForPR(context.Background(), task.ID, "")
	if err != nil {
		t.Fatalf("PushForPR() error = %v", err)
	}
	if got.Branch != *task.Branch || got.Strategy != PRStrategyDirect {
		t.Fatalf("PushForPR() = %+v", got)
	}
	if remote, local := remoteBranchSHA(t, bare, *task.Branch), worktreeHead(t, *task.WorktreePath); remote != local {
		t.Fatalf("origin's %s = %q, want the local tip %q", *task.Branch, remote, local)
	}
}

func TestPushForPR_UsesTaskBaseNotADevelopConstant(t *testing.T) {
	repo := newDetectRepo(t, "main", true)
	m := newTestManager(t)
	w, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatal(err)
	}
	task, err := m.CreateTask(w.ID, nil, "On main only")
	if err != nil {
		t.Fatal(err)
	}
	writeAndCommit(t, *task.WorktreePath, "f.txt", "f\n", "f")
	if _, err := m.PushForPR(context.Background(), task.ID, ""); err != nil {
		t.Fatalf("PushForPR() error = %v (the base is the task's main, not develop)", err)
	}

	// An explicit base still wins over the task's.
	runGitT(t, repo, "branch", "other")
	runGitT(t, repo, "push", "origin", "other")
	info, err := m.PRPreview(context.Background(), task.ID, "other")
	if err != nil || info.Base != "other" {
		t.Fatalf("PRPreview(other) = %+v, %v", info, err)
	}
}

func TestPushForPR_DivergedBase_PushesCleanPRTaskBranch(t *testing.T) {
	m, task, repo, bare := divergedPRTask(t)
	writeAndCommit(t, *task.WorktreePath, "taskwork.txt", "the task's own change\n", "add taskwork")
	// A branch under the old throwaway name (smind/pr-<id>) must not clash.
	oldName := "smind/pr-" + strconv.FormatInt(task.ID, 10)
	runGitT(t, repo, "branch", oldName)

	got, err := m.PushForPR(context.Background(), task.ID, "")
	if err != nil {
		t.Fatalf("PushForPR() error = %v", err)
	}
	want := "smind/pr-task-" + strconv.FormatInt(task.ID, 10)
	if got.Branch != want || got.Strategy != PRStrategyCherryPick {
		t.Fatalf("PushForPR() = %+v, want %s via cherry-pick", got, want)
	}
	if !remoteHasPath(t, bare, want, "taskwork.txt") {
		t.Fatalf("origin's %s is missing taskwork.txt", want)
	}
	if remoteHasPath(t, bare, want, "unrelated.txt") {
		t.Fatalf("origin's %s contains unrelated.txt -- unrelated history leaked into the PR branch", want)
	}
	if remoteBranchSHA(t, bare, *task.Branch) != "" {
		t.Fatalf("origin has the raw task branch %s; only %s should have been pushed", *task.Branch, want)
	}
	if localBranchExists(t, repo, want) {
		t.Fatalf("the local throwaway %s was left behind", want)
	}
	if !localBranchExists(t, repo, oldName) {
		t.Fatalf("the user's %s branch was deleted", oldName)
	}

	// A retry (the forge call failed after the push) replays the same commits
	// onto the same base: identical branch, so the push is a no-op, not a
	// rejection.
	first := remoteBranchSHA(t, bare, want)
	if _, err := m.PushForPR(context.Background(), task.ID, ""); err != nil {
		t.Fatalf("retry error = %v", err)
	}
	if again := remoteBranchSHA(t, bare, want); again != first {
		t.Fatalf("retry changed origin's %s: %s -> %s", want, first, again)
	}
}

func TestPushForPR_CherryPickConflict_CleansUp(t *testing.T) {
	m, task, repo, bare := divergedPRTask(t)
	// origin/develop moves with a conflicting README, from another clone.
	other := filepath.Join(t.TempDir(), "other")
	runGitT(t, t.TempDir(), "clone", "-b", "develop", bare, other)
	runGitT(t, other, "config", "user.email", "o@example.com")
	runGitT(t, other, "config", "user.name", "O")
	writeAndCommit(t, other, "README.md", "origin change\n", "origin readme")
	runGitT(t, other, "push", "origin", "develop")
	writeAndCommit(t, *task.WorktreePath, "README.md", "task change\n", "task readme")

	name := "smind/pr-task-" + strconv.FormatInt(task.ID, 10)
	for i := 0; i < 2; i++ { // the retry must fail the same way, not on "branch exists"
		_, err := m.PushForPR(context.Background(), task.ID, "")
		pe := wantPRCode(t, err, forge.CodeCherryPickConflict)
		if !strings.Contains(pe.Message, "`develop`") || !strings.Contains(pe.Message, "Update this branch from `develop` first.") {
			t.Fatalf("message = %q", pe.Message)
		}
		if localBranchExists(t, repo, name) {
			t.Fatalf("attempt %d left %s behind", i, name)
		}
		if remoteBranchSHA(t, bare, name) != "" {
			t.Fatalf("attempt %d pushed %s", i, name)
		}
	}
}

func TestPushForPR_PushRejectedByRemote_CleansUp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pre-receive hook needs a POSIX shell")
	}
	m, task, repo, bare := divergedPRTask(t)
	writeAndCommit(t, *task.WorktreePath, "tw.txt", "t\n", "tw")
	hook := filepath.Join(bare, "hooks", "pre-receive")
	if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho 'denied by policy' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := m.PushForPR(context.Background(), task.ID, "")
	pe := wantPRCode(t, err, forge.CodePushRejected)
	if d, _ := pe.Data["detail"].(string); !strings.Contains(d, "denied by policy") {
		t.Fatalf("detail = %q, want the remote's stderr tail", d)
	}
	if name := "smind/pr-task-" + strconv.FormatInt(task.ID, 10); localBranchExists(t, repo, name) {
		t.Fatalf("a failed push left %s behind", name)
	}
}

func TestPushForPR_PreExistingThrowawayBranchIsNeverDeleted(t *testing.T) {
	m, task, repo, _ := divergedPRTask(t)
	writeAndCommit(t, *task.WorktreePath, "tw.txt", "t\n", "tw")
	name := "smind/pr-task-" + strconv.FormatInt(task.ID, 10)
	runGitT(t, repo, "branch", name)
	if _, err := m.PushForPR(context.Background(), task.ID, ""); err == nil {
		t.Fatal("PushForPR() error = nil, want 'branch already exists'")
	}
	if !localBranchExists(t, repo, name) {
		t.Fatalf("PushForPR deleted %s, which it did not create", name)
	}
}

func TestPushForPR_NoCommitsBaseMissingAndInvalid(t *testing.T) {
	m, task, _, bare := prTask(t)
	_, err := m.PushForPR(context.Background(), task.ID, "")
	wantPRCode(t, err, forge.CodeNoCommits)

	writeAndCommit(t, *task.WorktreePath, "f.txt", "f\n", "f")
	_, err = m.PushForPR(context.Background(), task.ID, "no-such-branch")
	wantPRCode(t, err, forge.CodeBaseNotFound)
	_, err = m.PushForPR(context.Background(), task.ID, "a b")
	wantPRCode(t, err, forge.CodeInvalidBranch)
	if remoteBranchSHA(t, bare, *task.Branch) != "" {
		t.Fatal("a refused PushForPR pushed the branch")
	}
	_, err = m.PushForPR(context.Background(), task.ID+999, "")
	if err == nil {
		t.Fatal("PushForPR(unknown task) error = nil")
	}
}

func TestPushForPR_HonoursCancelledContext(t *testing.T) {
	m, task, _, _ := prTask(t)
	writeAndCommit(t, *task.WorktreePath, "f.txt", "f\n", "f")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := m.PushForPR(ctx, task.ID, "")
	wantPRCode(t, err, forge.CodeTimeout)
}

func TestPushUpdate_FastForward(t *testing.T) {
	m, task, _, bare := prTask(t)
	writeAndCommit(t, *task.WorktreePath, "a.txt", "a\n", "a")
	if _, err := m.PushForPR(context.Background(), task.ID, ""); err != nil {
		t.Fatal(err)
	}
	writeAndCommit(t, *task.WorktreePath, "b.txt", "b\n", "b")
	if err := m.PushUpdate(context.Background(), task.ID, *task.Branch); err != nil {
		t.Fatalf("PushUpdate() error = %v", err)
	}
	if remote, local := remoteBranchSHA(t, bare, *task.Branch), worktreeHead(t, *task.WorktreePath); remote != local {
		t.Fatalf("origin = %q, want %q", remote, local)
	}

	// The PR's head ref can differ from the task branch (adopted PRs).
	if err := m.PushUpdate(context.Background(), task.ID, "feature/head"); err != nil {
		t.Fatalf("PushUpdate(other head) error = %v", err)
	}
	if remote, local := remoteBranchSHA(t, bare, "feature/head"), worktreeHead(t, *task.WorktreePath); remote != local {
		t.Fatalf("origin feature/head = %q, want %q", remote, local)
	}
}

func TestPushUpdate_NeverForces(t *testing.T) {
	m, task, _, bare := prTask(t)
	writeAndCommit(t, *task.WorktreePath, "a.txt", "a\n", "a")
	if _, err := m.PushForPR(context.Background(), task.ID, ""); err != nil {
		t.Fatal(err)
	}
	// Someone else pushes to the PR branch.
	other := filepath.Join(t.TempDir(), "other")
	runGitT(t, t.TempDir(), "clone", "-b", *task.Branch, bare, other)
	runGitT(t, other, "config", "user.email", "o@example.com")
	runGitT(t, other, "config", "user.name", "O")
	writeAndCommit(t, other, "theirs.txt", "t\n", "their change")
	runGitT(t, other, "push", "origin", *task.Branch)
	theirs := remoteBranchSHA(t, bare, *task.Branch)

	writeAndCommit(t, *task.WorktreePath, "b.txt", "b\n", "b")
	err := m.PushUpdate(context.Background(), task.ID, *task.Branch)
	pe := wantPRCode(t, err, forge.CodePushRejected)
	if pe.Message != "GitHub rejected the push because the branch there has changes this branch doesn't. Update this branch first." {
		t.Fatalf("message = %q", pe.Message)
	}
	if got := remoteBranchSHA(t, bare, *task.Branch); got != theirs {
		t.Fatalf("origin moved to %s after a rejected update, want it untouched at %s", got, theirs)
	}
}

func TestPushUpdate_InvalidRemoteBranch(t *testing.T) {
	m, task, _, _ := prTask(t)
	for _, name := range []string{"", "-f", "a..b", "x y", "HEAD:evil", "a:b"} {
		err := m.PushUpdate(context.Background(), task.ID, name)
		wantPRCode(t, err, forge.CodeInvalidBranch)
	}
}

// TestPRGit_NoForceFlagAnywhere is the source-level guard behind "never
// --force": no push in this file may carry a force flag or a forcing refspec.
func TestPRGit_NoForceFlagAnywhere(t *testing.T) {
	b, err := os.ReadFile("pr.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, bad := range []string{"--force", "force-with-lease", "--mirror", "--delete", `"+refs/heads/"+local`, `"+refs/heads/"+remote`} {
		if strings.Contains(src, bad) {
			t.Fatalf("pr.go contains %q", bad)
		}
	}
}

func TestPRGitError_Classification(t *testing.T) {
	bg := context.Background()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	cases := []struct {
		name   string
		ctx    context.Context
		stderr string
		code   string
	}{
		{"https auth", bg, "remote: Invalid username or password.\nfatal: Authentication failed for 'https://github.com/o/r.git/'", forge.CodePushAuth},
		{"no credentials", bg, "fatal: could not read Username for 'https://github.com': terminal prompts disabled", forge.CodePushAuth},
		{"ssh key", bg, "git@github.com: Permission denied (publickey).\nfatal: Could not read from remote repository.", forge.CodePushAuth},
		{"403", bg, "fatal: unable to access 'https://github.com/o/r.git/': The requested URL returned error: 403", forge.CodePushAuth},
		{"repo not found", bg, "remote: Repository not found.", forge.CodePushAuth},
		{"non fast forward", bg, " ! [rejected]        a -> a (non-fast-forward)\nerror: failed to push some refs", forge.CodePushRejected},
		{"fetch first", bg, "hint: Updates were rejected because the remote contains work that you do not have locally. fetch first", forge.CodePushRejected},
		{"hook decline", bg, " ! [remote rejected] a -> a (pre-receive hook declined)", forge.CodePushRejected},
		{"no remote", bg, "fatal: 'origin' does not appear to be a git repository", forge.CodeNoRemote},
		{"other", bg, "fatal: something odd", forge.CodeError},
		{"cancelled", cancelled, "", forge.CodeTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pe := prGitError(tc.ctx, errors.New("exit status 128"), tc.stderr)
			if pe.Code != tc.code {
				t.Fatalf("code = %q, want %q", pe.Code, tc.code)
			}
			if strings.Contains(pe.Message, "fatal") || strings.Contains(pe.Message, "exit status") {
				t.Fatalf("message carries raw git output: %q", pe.Message)
			}
		})
	}
}

func TestPRGitError_ScrubsTokensAndNeverHasArgv(t *testing.T) {
	stderr := "fatal: unable to access 'https://x-access-token:ghp_abcdefghijklmnopqrstuvwxyz0123456789@github.com/o/r.git/': nope\n" +
		"Authorization: bearer ghp_abcdefghijklmnopqrstuvwxyz0123456789"
	pe := prGitError(context.Background(), errors.New("git [-c core.fsmonitor=false push origin refs/heads/x:refs/heads/x]: exit status 128"), stderr)
	all := fmt.Sprintf("%s|%v|%v", pe.Message, pe.Data, pe.RPCData())
	for _, leak := range []string{"ghp_", "x-access-token:ghp", "core.fsmonitor", "refs/heads/x", "exit status"} {
		if strings.Contains(all, leak) {
			t.Fatalf("error leaks %q: %s", leak, all)
		}
	}
}
