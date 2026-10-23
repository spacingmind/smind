package workspace

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/forge"
	"github.com/spacingmind/smind/internal/gitstate"
	"github.com/spacingmind/smind/internal/store"
)

// Archive-time branch cleanup and Restore (ADR-0039 section 9).
//
// The policy: a branch smind created is deleted on archive only when the user
// asked for it and it is merged, its worktree is clean and nothing else has it
// checked out; "force" lets an owned-but-unmerged or dirty branch go too. A
// branch smind did not create is never deleted. Remote branches are never
// touched: only `git branch -D <branch>` runs, in the workspace repository.

// baseFetchTimeout bounds the one fetch that refreshes origin/<base> before an
// archive decides whether the branch is merged.
const baseFetchTimeout = 20 * time.Second

// SetGitFacts registers the source of a task's freshly recomputed git state
// (wsapi wires gitstate.Service.Fresh, which carries the cached PR snapshot).
// Without one the Manager computes the local state itself, with no PR.
func (m *Manager) SetGitFacts(fn func(taskID int64) (gitstate.TaskGit, error)) {
	if m == nil {
		return
	}
	m.archiveMu.Lock()
	m.gitFacts = fn
	m.archiveMu.Unlock()
}

// ArchiveOptions selects what ArchiveTaskWith does beyond archiving.
type ArchiveOptions struct {
	// DeleteBranch deletes the task's local branch after the worktree is
	// gone. It is refused (typed error, nothing archived) unless the policy
	// allows it; Force relaxes the merged and clean requirements for a branch
	// smind owns.
	DeleteBranch bool
	Force        bool
}

// ArchivePreview is the risk summary task.archivePreview returns: local git
// and cached state only, no network.
type ArchivePreview struct {
	Branch              PreviewBranch `json:"branch"`
	Merged              PreviewMerged `json:"merged"`
	Base                PreviewBase   `json:"base"`
	Risks               PreviewRisks  `json:"risks"`
	IgnoredFilesLost    bool          `json:"ignoredFilesLost"`
	HasTeardown         bool          `json:"hasTeardown"`
	CanDeleteBranch     bool          `json:"canDeleteBranch"`
	DeleteBranchDefault bool          `json:"deleteBranchDefault"`
	DeleteBlockedReason string        `json:"deleteBlockedReason,omitempty"`
}

type PreviewBranch struct {
	Name  string `json:"name"`
	Owned bool   `json:"owned"`
}

type PreviewMerged struct {
	State string `json:"state"`
	Via   string `json:"via"`
}

type PreviewBase struct {
	Ref string `json:"ref"`
}

type PreviewRisks struct {
	Uncommitted          PreviewUncommitted `json:"uncommitted"`
	CommitsAheadOfOrigin int                `json:"commitsAheadOfOrigin"`
	CommitsAheadOfBase   int                `json:"commitsAheadOfBase"`
	Published            bool               `json:"published"`
}

type PreviewUncommitted struct {
	Files   int `json:"files"`
	Added   int `json:"added"`
	Deleted int `json:"deleted"`
}

// ArchivePreview summarizes what archiving task id puts at risk and whether
// the branch could be deleted now without force. It never fetches and never
// calls the forge.
func (m *Manager) ArchivePreview(id int64) (ArchivePreview, error) {
	t, err := m.store.GetTask(id)
	if err != nil {
		return ArchivePreview{}, fmt.Errorf("archive preview: %w", err)
	}
	if t.ArchivedAt != nil {
		return ArchivePreview{}, fmt.Errorf("archive preview: task %d is already archived", id)
	}
	ws, err := m.store.GetWorkspace(t.WorkspaceID)
	if err != nil {
		return ArchivePreview{}, fmt.Errorf("archive preview: %w", err)
	}
	g, gerr := m.freshGit(t, ws)

	p := ArchivePreview{
		Merged:           PreviewMerged{State: gitstate.MergedUnknown},
		IgnoredFilesLost: true,
		HasTeardown:      ws.TeardownScript != "",
	}
	branch := ""
	if t.Branch != nil {
		branch = *t.Branch
	}
	p.Branch = PreviewBranch{Name: branch, Owned: t.BranchOwned}
	p.Base.Ref = t.BaseRef
	if gerr == nil {
		if g.Merged != nil {
			p.Merged = PreviewMerged{State: g.Merged.State, Via: g.Merged.Via}
		}
		if g.Base != nil && g.Base.Ref != "" {
			p.Base.Ref = g.Base.Ref
		}
		if g.Commits != nil && g.Commits.AheadOfBase != nil {
			p.Risks.CommitsAheadOfBase = *g.Commits.AheadOfBase
		}
		if g.Origin != nil {
			p.Risks.Published = g.Origin.Published
		}
	}
	if t.WorktreePath != nil && dirExists(*t.WorktreePath) {
		if f, a, d, err := uncommittedStat(*t.WorktreePath); err == nil {
			p.Risks.Uncommitted = PreviewUncommitted{Files: f, Added: a, Deleted: d}
		}
	}
	if branch != "" {
		p.Risks.CommitsAheadOfOrigin = unpushedCommits(ws.Path, branch)
	}

	own := ""
	if t.WorktreePath != nil {
		own = *t.WorktreePath
	}
	elsewhere := branch != "" && branchCheckedOutElsewhere(ws.Path, branch, own)
	if block := branchDeleteBlock(t, g, gerr, elsewhere, false); block != nil {
		p.DeleteBlockedReason = block.Message
	} else {
		p.CanDeleteBranch = true
		p.DeleteBranchDefault = true
	}
	return p, nil
}

// branchDeleteBlock is the delete-branch policy: nil when the branch may be
// deleted, else the typed refusal. A branch smind did not create and a branch
// checked out elsewhere are refused even with force; the merged and clean
// requirements are what force relaxes. Anything git could not answer counts
// as not merged / not clean.
func branchDeleteBlock(t store.Task, g gitstate.TaskGit, gerr error, elsewhere, force bool) *forge.PRError {
	if t.Branch == nil || *t.Branch == "" || !t.BranchOwned {
		return forge.NewPRError(forge.CodeBranchNotOwned, "")
	}
	if elsewhere {
		return forge.NewPRError(forge.CodeBranchCheckedOut, "")
	}
	if force {
		return nil
	}
	if gerr != nil || g.Merged == nil || g.Merged.State != gitstate.MergedMerged {
		return forge.NewPRError(forge.CodeBranchUnmerged, "")
	}
	worktreeGone := t.WorktreePath == nil || !dirExists(*t.WorktreePath)
	if !worktreeGone && (g.Worktree == nil || g.Worktree.Dirty) {
		return forge.NewPRError(forge.CodeBranchDirty, "")
	}
	return nil
}

// ArchiveTaskWith is ArchiveTask with the branch-cleanup options. The decision
// whether the branch may go is made before anything changes (before the
// checkpoint commit, which would make the branch unmerged), so a refusal
// leaves the task active with its worktree intact. A `git branch -D` that
// fails afterwards leaves the task archived with the branch kept.
func (m *Manager) ArchiveTaskWith(id int64, opt ArchiveOptions) (store.Task, error) {
	if !opt.DeleteBranch {
		return m.ArchiveTask(id)
	}
	t, err := m.store.GetTask(id)
	if err != nil {
		return store.Task{}, fmt.Errorf("archive task: %w", err)
	}
	ws, err := m.store.GetWorkspace(t.WorkspaceID)
	if err != nil {
		return store.Task{}, fmt.Errorf("archive task %d: %w", id, err)
	}
	tip, err := m.decideBranchDelete(t, ws, opt.Force)
	if err != nil {
		return store.Task{}, err
	}

	if err := m.checkpointAndRemoveWorktree(t); err != nil {
		return store.Task{}, fmt.Errorf("archive task %d: %w", id, err)
	}
	deleted := m.deleteBranch(ws, *t.Branch, tip, opt.Force)

	t, err = m.store.ArchiveTask(id)
	if err != nil {
		return store.Task{}, fmt.Errorf("archive task %d: %w", id, err)
	}
	if deleted {
		if t, err = m.store.SetTaskBranchDeleted(id); err != nil {
			return store.Task{}, fmt.Errorf("archive task %d: %w", id, err)
		}
	}
	if n := m.getNotifier(); n != nil {
		n.NotifyTaskArchived(t)
	}
	m.notifyTask(t)
	return t, nil
}

// decideBranchDelete applies the policy to t and returns the branch tip it was
// decided on, or a typed refusal. Unless forced it first refreshes origin/<base>
// (one bounded fetch, failure tolerated) so a branch merged upstream is seen.
func (m *Manager) decideBranchDelete(t store.Task, ws store.Workspace, force bool) (tip string, err error) {
	own := ""
	if t.WorktreePath != nil {
		own = *t.WorktreePath
	}
	branch := ""
	if t.Branch != nil {
		branch = *t.Branch
	}
	elsewhere := branch != "" && branchCheckedOutElsewhere(ws.Path, branch, own)
	var g gitstate.TaskGit
	var gerr error
	if !force && t.BranchOwned && !elsewhere {
		m.fetchBase(ws, t)
		g, gerr = m.freshGit(t, ws)
	}
	if block := branchDeleteBlock(t, g, gerr, elsewhere, force); block != nil {
		return "", block
	}
	return gitHeadSha(ws.Path, "refs/heads/"+branch), nil
}

// deleteBranch runs `git branch -D` for the archived task's branch and reports
// whether it is gone. Unless forced, a branch whose tip moved since the
// decision (the checkpoint commit, or a concurrent commit) is kept: it is no
// longer the branch that was found merged. A failure is logged, never fatal.
func (m *Manager) deleteBranch(ws store.Workspace, branch, decidedTip string, force bool) bool {
	if !force && decidedTip != "" && gitHeadSha(ws.Path, "refs/heads/"+branch) != decidedTip {
		log.Printf("workspace: keeping branch %q: it changed after it was checked as merged", branch)
		return false
	}
	if err := runWorktreeGit(ws.Path, "branch", "-D", "--", branch); err != nil {
		log.Printf("workspace: archived task but could not delete branch %q: %v", branch, err)
		return false
	}
	return true
}

// fetchBase refreshes origin/<base> with one bounded fetch; a failure (no
// origin, offline, timeout) falls back to the refs already there.
func (m *Manager) fetchBase(ws store.Workspace, t store.Task) {
	base := t.BaseRef
	if base == "" {
		base = workspaceDefaultBase(ws)
	}
	if base == "" || validateRefName(ws.Path, base, true) != nil || !remoteConfigured(ws.Path, originRemote) {
		return
	}
	m.archiveMu.Lock()
	fn := m.fetchBaseFn
	m.archiveMu.Unlock()
	if fn == nil {
		fn = fetchBaseBounded
	}
	fn(ws.Path, base)
}

// fetchBaseBounded is `git fetch --no-tags origin +refs/heads/<base>:refs/remotes/origin/<base>`
// under baseFetchTimeout, outcome discarded.
func fetchBaseBounded(dir, base string) {
	ctx, cancel := context.WithTimeout(context.Background(), baseFetchTimeout)
	defer cancel()
	refspec := fmt.Sprintf("+refs/heads/%s:refs/remotes/%s/%s", base, originRemote, base)
	_, _ = runGitOutputEnvCtx(ctx, dir, nil, "fetch", "--no-tags", originRemote, refspec)
}

// freshGit returns t's recomputed git state: the registered source (the
// service, with its PR snapshot) or, without one, a local computation.
func (m *Manager) freshGit(t store.Task, ws store.Workspace) (gitstate.TaskGit, error) {
	m.archiveMu.Lock()
	fn := m.gitFacts
	m.archiveMu.Unlock()
	if fn != nil {
		return fn(t.ID)
	}
	if t.WorktreePath == nil || t.Branch == nil {
		return gitstate.TaskGit{}, errors.New("task has no worktree")
	}
	in := gitstate.Input{
		TaskID: t.ID, RepoDir: ws.Path, WorktreePath: *t.WorktreePath, Branch: *t.Branch,
		BaseRef: t.BaseRef, BaseSha: t.BaseSha, BranchOwned: t.BranchOwned,
		Untrusted: t.UntrustedSource, Conflicts: gitstate.NewConflictCache(),
	}
	if in.BaseRef == "" {
		in.BaseRef = workspaceDefaultBase(ws)
	}
	return gitstate.Compute(context.Background(), gitstate.DefaultRunner, in), nil
}

// uncommittedStat counts the files and lines in worktreePath's uncommitted
// changes, untracked files included (through the throwaway snapshot index, the
// real index untouched).
func uncommittedStat(worktreePath string) (files, added, deleted int, err error) {
	env, cleanup, err := snapshotIndex(worktreePath)
	if err != nil {
		return 0, 0, 0, err
	}
	defer cleanup()
	out, err := runGitOutputEnv(worktreePath, env, "diff", "--cached", "--numstat", "HEAD")
	if err != nil {
		return 0, 0, 0, err
	}
	for _, line := range strings.Split(out, "\n") {
		f := strings.SplitN(line, "\t", 3)
		if len(f) < 3 {
			continue
		}
		files++
		// Binary files print "-" for both counts.
		if n, err := strconv.Atoi(f[0]); err == nil {
			added += n
		}
		if n, err := strconv.Atoi(f[1]); err == nil {
			deleted += n
		}
	}
	return files, added, deleted, nil
}

// unpushedCommits counts the commits on branch that no remote-tracking ref of
// origin contains: what deleting the branch would leave nowhere else.
func unpushedCommits(repoPath, branch string) int {
	out, err := runGitOutput(repoPath, "rev-list", "--count", "refs/heads/"+branch, "--not", "--remotes="+originRemote)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(out))
	return n
}

// branchCheckedOutElsewhere reports whether any worktree of repoPath other than
// ownWorktree (the task's own, about to be removed; "" for none) has branch
// checked out, the main checkout included.
func branchCheckedOutElsewhere(repoPath, branch, ownWorktree string) bool {
	out, err := runGitOutput(repoPath, "worktree", "list", "--porcelain")
	if err != nil {
		return false
	}
	own := ""
	if ownWorktree != "" {
		own = canonicalPath(ownWorktree)
	}
	var path string
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			path = canonicalPath(strings.TrimPrefix(line, "worktree "))
		case line == "branch refs/heads/"+branch:
			if path != "" && path != own {
				return true
			}
		}
	}
	return false
}

// worktreeRegistered reports whether git has a worktree entry at path.
func worktreeRegistered(repoPath, path string) bool {
	out, err := runGitOutput(repoPath, "worktree", "list", "--porcelain")
	if err != nil {
		return false
	}
	want := canonicalPath(path)
	for _, line := range strings.Split(out, "\n") {
		if p, ok := strings.CutPrefix(line, "worktree "); ok && canonicalPath(p) == want {
			return true
		}
	}
	return false
}

// canonicalPath makes p comparable: cleaned, native separators and, when it
// exists, with symlinks resolved (macOS /var is /private/var).
func canonicalPath(p string) string {
	p = filepath.Clean(filepath.FromSlash(p))
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// ListArchivedTasks returns workspaceID's archived tasks, newest archive first.
func (m *Manager) ListArchivedTasks(workspaceID int64) ([]store.Task, error) {
	return m.store.ListArchivedTasks(workspaceID)
}

// SetWorktreeAddHook replaces the `git worktree add` Restore runs (tests only):
// fn gets the repository, the target path, the branch and whether the branch
// has to be created as a tracking branch of origin/<branch>.
func (m *Manager) SetWorktreeAddHook(fn func(repoPath, worktreePath, branch string, trackOrigin bool) error) {
	m.archiveMu.Lock()
	m.worktreeAddFn = fn
	m.archiveMu.Unlock()
}

// RestoreTask recreates an archived task's worktree from its kept branch (the
// local branch, else origin/<branch> as a tracking branch), claims a port,
// clears archived_at and re-runs the workspace setup script as for a new task
// (an untrusted task's setup stays blocked). The original worktree path is
// reused when it is free. With no branch anywhere it fails with branch_gone;
// nothing is changed on any failure.
func (m *Manager) RestoreTask(id int64) (store.Task, error) {
	t, err := m.store.GetTask(id)
	if err != nil {
		return store.Task{}, fmt.Errorf("restore task: %w", err)
	}
	if t.ArchivedAt == nil {
		return store.Task{}, forge.NewPRError(forge.CodeNotArchived, "")
	}
	if t.Branch == nil || *t.Branch == "" {
		return store.Task{}, fmt.Errorf("restore task %d: task has no branch", id)
	}
	branch := *t.Branch
	ws, err := m.store.GetWorkspace(t.WorkspaceID)
	if err != nil {
		return store.Task{}, fmt.Errorf("restore task %d: %w", id, err)
	}
	if err := m.checkSetupSupported(ws.SetupScript); err != nil {
		return store.Task{}, fmt.Errorf("restore task %d: %w", id, err)
	}

	trackOrigin := false
	switch {
	case gitRefCommitExists(ws.Path, "refs/heads/"+branch):
	case gitRefCommitExists(ws.Path, "refs/remotes/"+originRemote+"/"+branch):
		trackOrigin = true
	default:
		return store.Task{}, forge.BranchGone(branch)
	}
	if branchCheckedOutElsewhere(ws.Path, branch, "") {
		return store.Task{}, forge.RestoreCheckedOut()
	}

	path := ""
	if t.WorktreePath != nil && *t.WorktreePath != "" {
		path = *t.WorktreePath
	}
	if path == "" || pathExists(path) || worktreeRegistered(ws.Path, path) {
		name := slugify(t.Title) + "-" + strconv.FormatInt(taskStamp(), 36)
		path = filepath.Join(config.Dir(), "worktrees", strconv.FormatInt(ws.ID, 10), name)
	}

	m.archiveMu.Lock()
	add := m.worktreeAddFn
	m.archiveMu.Unlock()
	if add == nil {
		add = addRestoredWorktree
	}
	if err := add(ws.Path, path, branch, trackOrigin); err != nil {
		removeLeftoverWorktree(ws.Path, path)
		return store.Task{}, fmt.Errorf("restore task %d worktree: %w", id, err)
	}
	undo := func() { removeLeftoverWorktree(ws.Path, path) }

	if err := m.store.ClearTaskPort(id); err != nil {
		undo()
		return store.Task{}, fmt.Errorf("restore task %d: %w", id, err)
	}
	if _, err := m.claimPort(id); err != nil {
		undo()
		return store.Task{}, fmt.Errorf("restore task %d: %w", id, err)
	}
	t, err = m.store.RestoreTask(id, path)
	if err != nil {
		undo()
		return store.Task{}, fmt.Errorf("restore task %d: %w", id, err)
	}

	runSetup := ws.SetupScript != "" && !t.UntrustedSource
	status := ""
	switch {
	case ws.SetupScript == "":
	case t.UntrustedSource:
		status = SetupBlocked
	default:
		status = SetupRunning
	}
	if _, err := m.store.UpdateTaskSetup(id, status, "", ""); err != nil {
		return store.Task{}, fmt.Errorf("restore task %d: %w", id, err)
	}
	if t, err = m.store.GetTask(id); err != nil {
		return store.Task{}, fmt.Errorf("restore task %d: %w", id, err)
	}
	// task.updated, not task.created: the tree upserts the row, and nothing
	// that reacts to a brand-new task (it is not one) fires.
	if n := m.getNotifier(); n != nil {
		n.NotifyTaskUpdated(t)
	}
	m.notifyTask(t)
	if runSetup {
		m.startSetup(t, ws)
	}
	return t, nil
}

// addRestoredWorktree runs the real `git worktree add` for RestoreTask.
func addRestoredWorktree(repoPath, worktreePath, branch string, trackOrigin bool) error {
	if trackOrigin {
		return runWorktreeGit(repoPath, "worktree", "add", "--track", "-b", branch, worktreePath, originRemote+"/"+branch)
	}
	return runWorktreeGit(repoPath, "worktree", "add", worktreePath, branch)
}

// removeLeftoverWorktree makes sure a failed Restore leaves no directory (and
// no git worktree entry) at path. The caller checked the path was free first.
func removeLeftoverWorktree(repoPath, path string) {
	if worktreeRegistered(repoPath, path) {
		_ = gitWorktreeRemove(repoPath, path)
	}
	if pathExists(path) {
		_ = os.RemoveAll(path)
	}
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
