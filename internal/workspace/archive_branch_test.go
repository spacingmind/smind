//go:build !windows

package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/forge"
	"github.com/spacingmind/smind/internal/gitstate"
	"github.com/spacingmind/smind/internal/store"
)

// archiveEnv is a workspace over a repo whose origin is a local bare repo
// (never a real forge), plus the helpers the archive-cleanup tests share.
type archiveEnv struct {
	m     *Manager
	ws    store.Workspace
	repo  string
	bare  string
	fetch int // base fetches the Manager ran
}

func newArchiveEnv(t *testing.T) *archiveEnv {
	t.Helper()
	repo, bare := newRepoWithOrigin(t)
	m := newTestManager(t)
	w, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	e := &archiveEnv{m: m, ws: w, repo: repo, bare: bare}
	m.fetchBaseFn = func(dir, base string) { e.fetch++; fetchBaseBounded(dir, base) }
	return e
}

// task creates a task on a new smind-owned branch with one commit on it.
func (e *archiveEnv) task(t *testing.T, title string) store.Task {
	t.Helper()
	task, err := e.m.CreateTask(e.ws.ID, nil, title)
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	writeAndCommit(t, *task.WorktreePath, strings.ReplaceAll(title, " ", "-")+".txt", "work\n", "work: "+title)
	return task
}

// userBranchTask creates a task that checks out the user's own branch.
func (e *archiveEnv) userBranchTask(t *testing.T, branch string) store.Task {
	t.Helper()
	runGitT(t, e.repo, "branch", branch)
	task, err := e.m.CreateTask(e.ws.ID, nil, "user branch", WithExistingBranch(branch))
	if err != nil {
		t.Fatalf("CreateTask(existing branch) error = %v", err)
	}
	writeAndCommit(t, *task.WorktreePath, "user.txt", "work\n", "user work")
	return task
}

// mergeIntoOrigin merges task's branch into develop with a merge commit and
// pushes develop, so the branch is an ancestor of origin/develop.
func (e *archiveEnv) mergeIntoOrigin(t *testing.T, task store.Task) {
	t.Helper()
	runGitT(t, e.repo, "merge", "--no-ff", "-m", "merge", *task.Branch)
	runGitT(t, e.repo, "push", "origin", "develop")
}

// squashIntoOrigin lands task's changes as one new commit on develop (the
// branch itself does not become an ancestor).
func (e *archiveEnv) squashIntoOrigin(t *testing.T, task store.Task) {
	t.Helper()
	runGitT(t, e.repo, "merge", "--squash", *task.Branch)
	runGitT(t, e.repo, "commit", "-m", "squash")
	runGitT(t, e.repo, "push", "origin", "develop")
}

// withPR makes the Manager see a PR snapshot in the given state whose head sha
// is the branch tip (what the poller would hold), as gitstate.Service.Fresh
// does in the daemon.
func (e *archiveEnv) withPR(state string) {
	e.m.SetGitFacts(func(id int64) (gitstate.TaskGit, error) {
		t, err := e.m.GetTask(id)
		if err != nil {
			return gitstate.TaskGit{}, err
		}
		in := gitstate.Input{
			TaskID: id, RepoDir: e.ws.Path, WorktreePath: *t.WorktreePath, Branch: *t.Branch,
			BaseRef: t.BaseRef, BaseSha: t.BaseSha, BranchOwned: t.BranchOwned,
			Conflicts: gitstate.NewConflictCache(),
			PR:        &forge.PR{Number: 9, State: state, HeadSha: gitHeadSha(*t.WorktreePath, "HEAD")},
		}
		return gitstate.Compute(context.Background(), gitstate.DefaultRunner, in), nil
	})
}

func TestArchivePreview_CleanMergedOwned(t *testing.T) {
	t.Parallel()
	e := newArchiveEnv(t)
	task := e.task(t, "clean merged")
	e.mergeIntoOrigin(t, task)

	p, err := e.m.ArchivePreview(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Branch.Name != *task.Branch || !p.Branch.Owned {
		t.Fatalf("branch = %+v", p.Branch)
	}
	if p.Merged.State != "merged" || p.Merged.Via != "ancestor" {
		t.Fatalf("merged = %+v, want merged via ancestor", p.Merged)
	}
	if p.Base.Ref != "develop" {
		t.Fatalf("base = %+v, want develop", p.Base)
	}
	if !p.CanDeleteBranch || !p.DeleteBranchDefault || p.DeleteBlockedReason != "" {
		t.Fatalf("canDelete=%v default=%v reason=%q, want true/true/none", p.CanDeleteBranch, p.DeleteBranchDefault, p.DeleteBlockedReason)
	}
	// The commit reached origin/develop through the merge: nothing is unpushed.
	if p.Risks.Uncommitted != (PreviewUncommitted{}) || p.Risks.CommitsAheadOfOrigin != 0 || p.Risks.Published {
		t.Fatalf("risks = %+v, want nothing at risk", p.Risks)
	}
	if !p.IgnoredFilesLost || p.HasTeardown {
		t.Fatalf("ignoredFilesLost=%v hasTeardown=%v", p.IgnoredFilesLost, p.HasTeardown)
	}
}

func TestArchivePreview_OwnedUnmerged(t *testing.T) {
	t.Parallel()
	e := newArchiveEnv(t)
	task := e.task(t, "unmerged")

	p, err := e.m.ArchivePreview(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.CanDeleteBranch || p.DeleteBranchDefault {
		t.Fatalf("canDelete=%v default=%v, want false", p.CanDeleteBranch, p.DeleteBranchDefault)
	}
	if p.DeleteBlockedReason != "The branch is not merged anywhere yet." {
		t.Fatalf("reason = %q", p.DeleteBlockedReason)
	}
	if p.Merged.State != "unmerged" || p.Risks.CommitsAheadOfBase != 1 || p.Risks.CommitsAheadOfOrigin != 1 || p.Risks.Published {
		t.Fatalf("merged=%+v risks=%+v, want unmerged, 1 ahead of base, 1 unpushed, unpublished", p.Merged, p.Risks)
	}
}

func TestArchivePreview_DirtyMerged(t *testing.T) {
	t.Parallel()
	e := newArchiveEnv(t)
	task := e.task(t, "dirty merged")
	e.mergeIntoOrigin(t, task)
	wt := *task.WorktreePath
	if err := os.WriteFile(filepath.Join(wt, "README.md"), []byte("hello\nmore\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "new.txt"), []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := e.m.ArchivePreview(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.CanDeleteBranch || p.DeleteBlockedReason != forgeMsg(forge.CodeBranchDirty) {
		t.Fatalf("canDelete=%v reason=%q, want the dirty sentence", p.CanDeleteBranch, p.DeleteBlockedReason)
	}
	if p.Risks.Uncommitted != (PreviewUncommitted{Files: 2, Added: 3, Deleted: 0}) {
		t.Fatalf("uncommitted = %+v, want 2 files, 3 added", p.Risks.Uncommitted)
	}
	if p.Merged.State != "merged" {
		t.Fatalf("merged = %+v", p.Merged)
	}
}

func forgeMsg(code string) string { return forge.NewPRError(code, "").Message }

func TestArchivePreview_UserBranch(t *testing.T) {
	t.Parallel()
	e := newArchiveEnv(t)
	task := e.userBranchTask(t, "feature/mine")
	e.mergeIntoOrigin(t, task)

	p, err := e.m.ArchivePreview(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Branch.Owned || p.CanDeleteBranch || p.DeleteBlockedReason != "This is your own branch; smind never deletes it." {
		t.Fatalf("branch=%+v canDelete=%v reason=%q", p.Branch, p.CanDeleteBranch, p.DeleteBlockedReason)
	}
}

func TestArchivePreview_SquashMergedByPR(t *testing.T) {
	t.Parallel()
	e := newArchiveEnv(t)
	task := e.task(t, "squashed")
	e.squashIntoOrigin(t, task)

	// Without the PR the branch is not an ancestor of origin/develop.
	p, err := e.m.ArchivePreview(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Merged.State != "unmerged" || p.CanDeleteBranch {
		t.Fatalf("without a PR: merged=%+v canDelete=%v, want unmerged", p.Merged, p.CanDeleteBranch)
	}

	e.withPR("merged")
	p, err = e.m.ArchivePreview(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Merged.State != "merged" || p.Merged.Via != "pr" || !p.CanDeleteBranch {
		t.Fatalf("with the merged PR: merged=%+v canDelete=%v, want merged via pr", p.Merged, p.CanDeleteBranch)
	}
}

func TestArchivePreview_NoNetwork(t *testing.T) {
	t.Parallel()
	e := newArchiveEnv(t)
	task := e.task(t, "offline")
	e.m.fetchBaseFn = func(string, string) { t.Error("the preview fetched") }
	provider := &fakePRProvider{err: errors.New("the preview called the forge")}
	useProvider(e.m, provider)

	if _, err := e.m.ArchivePreview(task.ID); err != nil {
		t.Fatal(err)
	}
	if n := provider.calls.Load(); n != 0 {
		t.Fatalf("forge called %d times by the preview", n)
	}
}

func TestArchive_DeletesMergedOwnedBranch(t *testing.T) {
	t.Parallel()
	e := newArchiveEnv(t)
	task := e.task(t, "delete me")
	e.mergeIntoOrigin(t, task)

	got, err := e.m.ArchiveTaskWith(task.ID, ArchiveOptions{DeleteBranch: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.ArchivedAt == nil || got.BranchDeletedAt == nil {
		t.Fatalf("task = %+v, want archived with BranchDeletedAt set", got)
	}
	if localBranchExists(t, e.repo, *task.Branch) {
		t.Fatal("the branch survived")
	}
	if dirExists(*task.WorktreePath) {
		t.Fatal("the worktree survived")
	}
	if e.fetch != 1 {
		t.Fatalf("base fetches = %d, want exactly 1", e.fetch)
	}
}

func TestArchive_KeepsUnmergedBranch(t *testing.T) {
	t.Parallel()
	e := newArchiveEnv(t)
	task := e.task(t, "keep me")

	got, err := e.m.ArchiveTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ArchivedAt == nil || got.BranchDeletedAt != nil {
		t.Fatalf("task = %+v, want archived, branch kept", got)
	}
	if !localBranchExists(t, e.repo, *task.Branch) {
		t.Fatal("the unmerged branch was deleted")
	}
	if e.fetch != 0 {
		t.Fatalf("a plain archive fetched %d times", e.fetch)
	}
}

func TestArchive_ForceDeletesOwnedUnmerged(t *testing.T) {
	t.Parallel()
	e := newArchiveEnv(t)
	task := e.task(t, "force me")
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "wip.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := e.m.ArchiveTaskWith(task.ID, ArchiveOptions{DeleteBranch: true, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.ArchivedAt == nil || got.BranchDeletedAt == nil || localBranchExists(t, e.repo, *task.Branch) {
		t.Fatalf("task = %+v, want archived with the unmerged, dirty branch deleted", got)
	}
}

func TestArchive_ForceRefusedForUserBranch(t *testing.T) {
	t.Parallel()
	e := newArchiveEnv(t)
	task := e.userBranchTask(t, "feature/keepme")

	_, err := e.m.ArchiveTaskWith(task.ID, ArchiveOptions{DeleteBranch: true, Force: true})
	wantPRCode(t, err, forge.CodeBranchNotOwned)
	if after := getTask(t, e.m, task.ID); after.ArchivedAt != nil || !dirExists(*task.WorktreePath) {
		t.Fatalf("task = %+v, want still active with its worktree", after)
	}
	if !localBranchExists(t, e.repo, "feature/keepme") {
		t.Fatal("the user's branch was deleted")
	}
}

func TestArchive_DirtyMergedKeepsBranchAndCheckpoint(t *testing.T) {
	t.Parallel()
	e := newArchiveEnv(t)
	task := e.task(t, "dirty archive")
	e.mergeIntoOrigin(t, task)
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "wip.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Asked to delete, a dirty branch is refused before anything changes.
	_, err := e.m.ArchiveTaskWith(task.ID, ArchiveOptions{DeleteBranch: true})
	wantPRCode(t, err, forge.CodeBranchDirty)
	if _, err := os.Stat(filepath.Join(*task.WorktreePath, "wip.txt")); err != nil {
		t.Fatalf("the refusal touched the worktree: %v", err)
	}

	// Not asked, it archives: checkpoint commit on the kept branch.
	got, err := e.m.ArchiveTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ArchivedAt == nil || got.BranchDeletedAt != nil {
		t.Fatalf("task = %+v, want archived with the branch kept", got)
	}
	subject := strings.TrimSpace(runGitOutputT(t, e.repo, "log", "-1", "--format=%s", *task.Branch))
	if subject != checkpointCommitMessage {
		t.Fatalf("branch tip = %q, want the checkpoint commit", subject)
	}
}

func TestArchive_DeleteRefusedLeavesTaskActive(t *testing.T) {
	t.Parallel()
	e := newArchiveEnv(t)
	task := e.task(t, "refused")

	_, err := e.m.ArchiveTaskWith(task.ID, ArchiveOptions{DeleteBranch: true})
	pe := wantPRCode(t, err, forge.CodeBranchUnmerged)
	if pe.Message != "The branch is not merged anywhere yet." {
		t.Fatalf("message = %q", pe.Message)
	}
	after := getTask(t, e.m, task.ID)
	if after.ArchivedAt != nil || after.Status != task.Status || !dirExists(*task.WorktreePath) || !localBranchExists(t, e.repo, *task.Branch) {
		t.Fatalf("task = %+v, want untouched", after)
	}
}

func TestArchive_BranchCheckedOutElsewhere(t *testing.T) {
	t.Parallel()
	e := newArchiveEnv(t)
	task := e.task(t, "checked out")
	e.mergeIntoOrigin(t, task)
	other := filepath.Join(t.TempDir(), "other")
	runGitT(t, e.repo, "worktree", "add", "--force", other, *task.Branch)

	_, err := e.m.ArchiveTaskWith(task.ID, ArchiveOptions{DeleteBranch: true})
	wantPRCode(t, err, forge.CodeBranchCheckedOut)
	// Force does not override git's own rule either.
	_, err = e.m.ArchiveTaskWith(task.ID, ArchiveOptions{DeleteBranch: true, Force: true})
	wantPRCode(t, err, forge.CodeBranchCheckedOut)
	if after := getTask(t, e.m, task.ID); after.ArchivedAt != nil || !dirExists(*task.WorktreePath) {
		t.Fatalf("task = %+v, want untouched", after)
	}
}

func TestArchive_SquashMergeUsesForceDelete(t *testing.T) {
	t.Parallel()
	e := newArchiveEnv(t)
	task := e.task(t, "squash delete")
	e.squashIntoOrigin(t, task)
	e.withPR("merged")

	got, err := e.m.ArchiveTaskWith(task.ID, ArchiveOptions{DeleteBranch: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.BranchDeletedAt == nil || localBranchExists(t, e.repo, *task.Branch) {
		t.Fatalf("task = %+v, want the squash-merged branch deleted (git branch -d would refuse it)", got)
	}
}

func TestArchive_NeverDeletesRemoteBranch(t *testing.T) {
	t.Parallel()
	e := newArchiveEnv(t)
	task := e.task(t, "remote stays")
	runGitT(t, e.repo, "push", "origin", *task.Branch)
	e.mergeIntoOrigin(t, task)
	before := remoteBranchSHA(t, e.bare, *task.Branch)
	if before == "" {
		t.Fatal("setup: the branch is not on origin")
	}

	if _, err := e.m.ArchiveTaskWith(task.ID, ArchiveOptions{DeleteBranch: true}); err != nil {
		t.Fatal(err)
	}
	if localBranchExists(t, e.repo, *task.Branch) {
		t.Fatal("the local branch survived")
	}
	if after := remoteBranchSHA(t, e.bare, *task.Branch); after != before {
		t.Fatalf("origin branch = %q, want untouched %q", after, before)
	}
}

func TestArchive_BranchDeleteFailureKeepsArchive(t *testing.T) {
	t.Parallel()
	e := newArchiveEnv(t)
	task := e.task(t, "stuck ref")
	e.mergeIntoOrigin(t, task)
	// A stale lock on the ref makes `git branch -D` fail after the worktree is gone.
	lock := filepath.Join(e.repo, ".git", "refs", "heads", filepath.FromSlash(*task.Branch)+".lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(lock) })

	got, err := e.m.ArchiveTaskWith(task.ID, ArchiveOptions{DeleteBranch: true})
	if err != nil {
		t.Fatalf("error = %v, want the archive to succeed with the branch kept", err)
	}
	if got.ArchivedAt == nil || got.BranchDeletedAt != nil {
		t.Fatalf("task = %+v, want archived, BranchDeletedAt unset", got)
	}
	if !localBranchExists(t, e.repo, *task.Branch) || dirExists(*task.WorktreePath) {
		t.Fatal("want the branch kept and the worktree removed")
	}
}

func TestRestore_FromKeptBranch(t *testing.T) {
	t.Parallel()
	e := newArchiveEnv(t)
	base := fakePorts(e.m)
	e.m.SetWorktreesConfig(WorktreesSettings{HasPorts: true, PortLo: base, PortHi: base + 5})
	task := e.task(t, "restore kept")
	if _, err := e.m.ArchiveTask(task.ID); err != nil {
		t.Fatal(err)
	}

	got, err := e.m.RestoreTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ArchivedAt != nil || got.Status != "created" {
		t.Fatalf("task = %+v, want active", got)
	}
	if *got.WorktreePath != *task.WorktreePath {
		t.Fatalf("worktree = %q, want the original path %q while it is free", *got.WorktreePath, *task.WorktreePath)
	}
	if head := strings.TrimSpace(runGitOutputT(t, *got.WorktreePath, "rev-parse", "--abbrev-ref", "HEAD")); head != *task.Branch {
		t.Fatalf("worktree HEAD = %q, want %q", head, *task.Branch)
	}
	if got.Port == nil || int(*got.Port) < base || int(*got.Port) > base+5 {
		t.Fatalf("port = %v, want one in [%d,%d]", got.Port, base, base+5)
	}
	live, err := e.m.ListTasks(e.ws.ID)
	if err != nil || len(live) != 1 || live[0].ID != task.ID {
		t.Fatalf("ListTasks() = %+v, %v, want the restored task", live, err)
	}
}

func TestRestore_FreshPathWhenOriginalIsTaken(t *testing.T) {
	t.Parallel()
	e := newArchiveEnv(t)
	task := e.task(t, "path taken")
	if _, err := e.m.ArchiveTask(task.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(*task.WorktreePath, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := e.m.RestoreTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if *got.WorktreePath == *task.WorktreePath || !dirExists(*got.WorktreePath) {
		t.Fatalf("worktree = %q, want a fresh stamped path that exists", *got.WorktreePath)
	}
}

func TestRestore_FromOriginOnly(t *testing.T) {
	t.Parallel()
	e := newArchiveEnv(t)
	task := e.task(t, "origin only")
	runGitT(t, e.repo, "push", "origin", *task.Branch)
	e.mergeIntoOrigin(t, task)
	if _, err := e.m.ArchiveTaskWith(task.ID, ArchiveOptions{DeleteBranch: true}); err != nil {
		t.Fatal(err)
	}
	if localBranchExists(t, e.repo, *task.Branch) {
		t.Fatal("setup: the local branch survived")
	}

	got, err := e.m.RestoreTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ArchivedAt != nil || !dirExists(*got.WorktreePath) {
		t.Fatalf("task = %+v, want restored", got)
	}
	up := strings.TrimSpace(runGitOutputT(t, e.repo, "rev-parse", "--abbrev-ref", *task.Branch+"@{upstream}"))
	if up != "origin/"+*task.Branch {
		t.Fatalf("upstream = %q, want a tracking branch of origin/%s", up, *task.Branch)
	}
}

func TestRestore_BranchGone(t *testing.T) {
	t.Parallel()
	e := newArchiveEnv(t)
	task := e.task(t, "gone")
	e.mergeIntoOrigin(t, task)
	if _, err := e.m.ArchiveTaskWith(task.ID, ArchiveOptions{DeleteBranch: true}); err != nil {
		t.Fatal(err)
	}

	_, err := e.m.RestoreTask(task.ID)
	pe := wantPRCode(t, err, forge.CodeBranchGone)
	want := "The branch `" + *task.Branch + "` no longer exists, so there is nothing to restore. It was deleted after it was merged."
	if pe.Message != want {
		t.Fatalf("message = %q, want %q", pe.Message, want)
	}
	if after := getTask(t, e.m, task.ID); after.ArchivedAt == nil || dirExists(*task.WorktreePath) {
		t.Fatalf("task = %+v, want still archived and no directory", after)
	}
}

func TestRestore_NotArchived(t *testing.T) {
	t.Parallel()
	e := newArchiveEnv(t)
	task := e.task(t, "active")

	_, err := e.m.RestoreTask(task.ID)
	wantPRCode(t, err, forge.CodeNotArchived)
}

func TestRestore_UntrustedStaysBlocked(t *testing.T) {
	t.Parallel()
	e := newArchiveEnv(t)
	script := `echo ran > setup.marker`
	if _, err := e.m.store.SetWorkspaceScripts(e.ws.ID, &script, nil); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "fork-wt")
	runGitT(t, e.repo, "worktree", "add", path, "-b", "smind/fork-task")
	branch := "smind/fork-task"
	task, err := e.m.store.CreateTask(store.Task{
		WorkspaceID: e.ws.ID, Title: "fork", Status: "created", WorktreePath: &path, Branch: &branch,
		BranchOwned: true, UntrustedSource: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.ArchiveTask(task.ID); err != nil {
		t.Fatal(err)
	}

	got, err := e.m.RestoreTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SetupStatus != SetupBlocked || !got.UntrustedSource {
		t.Fatalf("setup=%q untrusted=%v, want blocked and still untrusted", got.SetupStatus, got.UntrustedSource)
	}
	if _, err := os.Stat(filepath.Join(*got.WorktreePath, "setup.marker")); err == nil {
		t.Fatal("the setup script of an untrusted task ran on Restore")
	}
}

func TestRestore_RunsSetupAndClaimsPort(t *testing.T) {
	t.Parallel()
	e := newArchiveEnv(t)
	base := fakePorts(e.m)
	e.m.SetWorktreesConfig(WorktreesSettings{HasPorts: true, PortLo: base, PortHi: base + 3})
	script := `echo "port=$SMIND_PORT" > setup.marker`
	if _, err := e.m.store.SetWorkspaceScripts(e.ws.ID, &script, nil); err != nil {
		t.Fatal(err)
	}
	task := e.task(t, "setup again")
	waitSetup(t, e.m, task.ID, SetupOK)
	if _, err := e.m.ArchiveTask(task.ID); err != nil {
		t.Fatal(err)
	}

	got, err := e.m.RestoreTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SetupStatus != SetupRunning && got.SetupStatus != SetupOK {
		t.Fatalf("setup = %q, want running (or already ok)", got.SetupStatus)
	}
	done := waitSetup(t, e.m, task.ID, SetupOK)
	if done.Port == nil {
		t.Fatal("no port claimed")
	}
	marker, err := os.ReadFile(filepath.Join(*done.WorktreePath, "setup.marker"))
	if err != nil || !strings.HasPrefix(string(marker), "port=") || strings.TrimSpace(string(marker)) == "port=" {
		t.Fatalf("setup marker = %q, %v, want the script to have seen SMIND_PORT", marker, err)
	}
}

func TestRestore_WorktreeAddFailureLeavesNoDirectory(t *testing.T) {
	t.Parallel()
	e := newArchiveEnv(t)
	task := e.task(t, "add fails")
	if _, err := e.m.ArchiveTask(task.ID); err != nil {
		t.Fatal(err)
	}
	e.m.SetWorktreeAddHook(func(_, worktreePath, _ string, _ bool) error {
		// git can leave a half-made directory behind when it fails.
		if err := os.MkdirAll(filepath.Join(worktreePath, "half"), 0o755); err != nil {
			return err
		}
		return errors.New("git exploded")
	})

	_, err := e.m.RestoreTask(task.ID)
	if err == nil {
		t.Fatal("Restore succeeded, want the add failure")
	}
	if pathExists(*task.WorktreePath) {
		t.Fatal("a stray directory survived the failed Restore")
	}
	if after := getTask(t, e.m, task.ID); after.ArchivedAt == nil {
		t.Fatalf("task = %+v, want still archived", after)
	}
}
