package workspace

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/spacingmind/smind/internal/store"
)

// ErrBaseUnknown is returned by diff and PR actions for a task whose
// base_sha is empty (ADR-0039 section 2). The wsapi layer maps it to the
// base_unknown code.
var ErrBaseUnknown error = codedError{"base_unknown"}

// ErrBaseNotFound is returned by SetTaskBase when the named base resolves to
// no branch (neither origin/<ref> nor a local branch) or shares no history
// with the task's branch.
var ErrBaseNotFound error = codedError{"base_not_found"}

// codedError is a sentinel that also carries its wire code (ADR-0039
// section 6): wsapi's error mapper finds RPCCode anywhere in the wrap chain.
type codedError struct{ code string }

func (e codedError) Error() string   { return e.code }
func (e codedError) RPCCode() string { return e.code }

func baseUnknownError() error {
	return fmt.Errorf("%w: This task's base commit is unknown. Set it with task.setBase.", ErrBaseUnknown)
}

// taskBaseSha returns t's stored base commit or the typed base_unknown error.
func taskBaseSha(t store.Task) (string, error) {
	if t.BaseSha == "" {
		return "", baseUnknownError()
	}
	return t.BaseSha, nil
}

const originRemote = "origin"

var shaRE = regexp.MustCompile(`^[0-9a-f]{40}$`)

// DefaultBase returns workspaceID's default base branch: the stored override
// when set (detected=false), else the first match of: origin/HEAD's target,
// develop, main, master (each if it exists locally or on origin), the
// checkout's current branch (detected=true). ref is "" only for a repo with
// no commits or a detached HEAD and none of the usual branches. Detection is
// local git only (ADR-0039 section 2).
func (m *Manager) DefaultBase(workspaceID int64) (ref string, detected bool, err error) {
	ws, err := m.store.GetWorkspace(workspaceID)
	if err != nil {
		return "", false, fmt.Errorf("default base: %w", err)
	}
	if ws.DefaultBase != "" {
		return ws.DefaultBase, false, nil
	}
	return detectDefaultBase(ws.Path), true, nil
}

func detectDefaultBase(dir string) string {
	if out, err := runGitOutput(dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if b := strings.TrimPrefix(strings.TrimSpace(out), originRemote+"/"); b != "" && branchExists(dir, b) {
			return b
		}
	}
	for _, b := range []string{"develop", "main", "master"} {
		if branchExists(dir, b) {
			return b
		}
	}
	if out, err := runGitOutput(dir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		if b := strings.TrimSpace(out); b != "" && b != "HEAD" {
			return b
		}
	}
	return ""
}

// branchExists reports whether branch exists as origin/<branch> or locally.
func branchExists(dir, branch string) bool {
	return gitRefCommitExists(dir, "refs/remotes/"+originRemote+"/"+branch) ||
		gitRefCommitExists(dir, "refs/heads/"+branch)
}

// baseCompareRef returns the full ref a task's base branch is compared
// against: origin/<base> when origin has it, else the local branch, else "".
func baseCompareRef(dir, base string) string {
	if base == "" {
		return ""
	}
	if ref := "refs/remotes/" + originRemote + "/" + base; gitRefCommitExists(dir, ref) {
		return ref
	}
	if ref := "refs/heads/" + base; gitRefCommitExists(dir, ref) {
		return ref
	}
	return ""
}

// gitMergeBase returns the merge base of a and b, or "" when there is none
// (unrelated histories) or git fails. Never an error: callers treat an
// unknown base as empty.
func gitMergeBase(dir, a, b string) string {
	out, err := runGitOutput(dir, "merge-base", a, b)
	if err != nil {
		return ""
	}
	if sha := strings.TrimSpace(out); shaRE.MatchString(sha) {
		return sha
	}
	return ""
}

func gitHeadSha(dir, rev string) string {
	out, err := runGitOutput(dir, "rev-parse", "--verify", "--quiet", "--end-of-options", rev+"^{commit}")
	if err != nil {
		return ""
	}
	if sha := strings.TrimSpace(out); shaRE.MatchString(sha) {
		return sha
	}
	return ""
}

// SetDefaultBase stores branch as workspaceID's default-base override. The
// branch must be a valid branch name that exists locally or as
// origin/<branch>; "" clears the override (back to auto-detect). The updated
// workspace is returned.
func (m *Manager) SetDefaultBase(workspaceID int64, branch string) (store.Workspace, error) {
	ws, err := m.store.GetWorkspace(workspaceID)
	if err != nil {
		return store.Workspace{}, fmt.Errorf("set default base: %w", err)
	}
	branch = strings.TrimSpace(branch)
	if branch != "" {
		branch = strings.TrimPrefix(branch, originRemote+"/")
		if err := validateRefName(ws.Path, branch, true); err != nil {
			return store.Workspace{}, fmt.Errorf("set default base: %w", err)
		}
		if !branchExists(ws.Path, branch) {
			return store.Workspace{}, fmt.Errorf("set default base: branch %q not found locally or as %s/%s", branch, originRemote, branch)
		}
	}
	if err := m.store.SetWorkspaceDefaultBase(workspaceID, branch); err != nil {
		return store.Workspace{}, fmt.Errorf("set default base: %w", err)
	}
	ws.DefaultBase = branch
	return ws, nil
}

// SetTaskBase re-bases task id's recorded base onto baseRef (a branch name;
// an "origin/" prefix is stripped): it resolves origin/<ref> else the local
// branch, computes the merge base with the task's branch, and stores both
// base_ref and base_sha. Fails with ErrBaseNotFound when the ref does not
// resolve or has no merge base with the task's branch.
func (m *Manager) SetTaskBase(taskID int64, baseRef string) (store.Task, error) {
	t, err := m.store.GetTask(taskID)
	if err != nil {
		return store.Task{}, fmt.Errorf("set task base: %w", err)
	}
	if t.Branch == nil {
		return store.Task{}, fmt.Errorf("set task base %d: task has no branch", taskID)
	}
	ws, err := m.store.GetWorkspace(t.WorkspaceID)
	if err != nil {
		return store.Task{}, fmt.Errorf("set task base %d: %w", taskID, err)
	}
	baseRef = strings.TrimPrefix(strings.TrimSpace(baseRef), originRemote+"/")
	if err := validateRefName(ws.Path, baseRef, true); err != nil {
		return store.Task{}, fmt.Errorf("set task base %d: %w", taskID, err)
	}
	cmp := baseCompareRef(ws.Path, baseRef)
	if cmp == "" {
		return store.Task{}, fmt.Errorf("set task base %d: %w: no branch %q locally or on %s", taskID, ErrBaseNotFound, baseRef, originRemote)
	}
	sha := gitMergeBase(ws.Path, "refs/heads/"+*t.Branch, cmp)
	if sha == "" {
		return store.Task{}, fmt.Errorf("set task base %d: %w: branch %q shares no history with %q", taskID, ErrBaseNotFound, *t.Branch, baseRef)
	}
	if err := m.store.SetTaskBase(taskID, baseRef, sha); err != nil {
		return store.Task{}, fmt.Errorf("set task base %d: %w", taskID, err)
	}
	t, err = m.store.GetTask(taskID)
	if err != nil {
		return store.Task{}, fmt.Errorf("set task base %d: %w", taskID, err)
	}
	if n := m.getNotifier(); n != nil {
		n.NotifyTaskUpdated(t)
	}
	m.notifyTask(t)
	return t, nil
}

// workspaceDefaultBase is ws's default base: the stored override, else the
// detected one.
// WorkspaceDefaultBase is ws's default base: the stored override, else the
// detected one.
func (m *Manager) WorkspaceDefaultBase(ws store.Workspace) string { return workspaceDefaultBase(ws) }

func workspaceDefaultBase(ws store.Workspace) string {
	if ws.DefaultBase != "" {
		return ws.DefaultBase
	}
	return detectDefaultBase(ws.Path)
}

// computeTaskBase derives the (base_ref, base_sha) a just-created task is
// recorded with, per ADR-0039 section 2's table. The worktree already exists
// and is checked out on the task branch, so HEAD there is the branch tip.
// Every step is best effort: a base that cannot be resolved is "".
//
// base_sha is the diff base. For the modes that create the branch (default
// and baseRef) it is the commit the branch was created at, so the diff shows
// exactly the task's own work even when the workspace checkout carried
// unrelated unpushed commits. base_ref is only ever a branch name (the PR
// target): a baseRef that is not a branch (a tag, a sha, HEAD~3) falls back
// to the workspace default base. No network is used here.
func computeTaskBase(ws store.Workspace, p createTaskParams, worktreePath string) (ref, sha string) {
	switch {
	case p.baseRef != "":
		ref = baseBranchOf(ws.Path, p.baseRef)
		if ref == "" {
			ref = workspaceDefaultBase(ws)
		}
		return ref, gitHeadSha(worktreePath, "HEAD")
	case p.branch != "":
		// Existing branch: the fork point is unknown, so the base is the
		// merge base with the default base; unrelated histories leave it empty.
		ref = workspaceDefaultBase(ws)
	case p.prSupplied:
		// The PR's target branch, as the forge reported it (validated by
		// CreateTask); the workspace default base when the forge could not.
		ref = p.prBaseRef
		if ref == "" {
			ref = workspaceDefaultBase(ws)
		}
	default:
		return workspaceDefaultBase(ws), gitHeadSha(worktreePath, "HEAD")
	}
	if cmp := baseCompareRef(ws.Path, ref); cmp != "" {
		sha = gitMergeBase(worktreePath, "HEAD", cmp)
	}
	return ref, sha
}

// baseBranchOf names the branch a baseRef refers to: "<remote>/<branch>" for
// a configured remote gives <branch>; a name that is an existing local branch
// gives itself; anything else (tag, sha, relative rev) gives "".
func baseBranchOf(dir, baseRef string) string {
	if remote, b, ok := splitRemoteBranch(baseRef); ok && remoteConfigured(dir, remote) {
		return b
	}
	if gitRefCommitExists(dir, "refs/heads/"+baseRef) {
		return baseRef
	}
	return ""
}
