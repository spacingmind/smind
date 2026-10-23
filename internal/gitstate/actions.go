package gitstate

import (
	"fmt"
	"strings"

	"github.com/spacingmind/smind/internal/forge"
)

// ActionCtx carries what the pure policy cannot read from TaskGit.
type ActionCtx struct {
	RunActive bool
	// NoOrigin: the workspace has no usable `origin` remote (U7).
	NoOrigin bool
	// OriginNotForge: `origin` exists but is a local path or another URL no forge hosts.
	OriginNotForge bool
	// ForgeCode passes a forge_* blocker through (the forge CLI is missing or
	// not signed in on the daemon host). ForgeReason overrides the standard
	// sentence for the code, which names the action.
	ForgeCode   string
	ForgeReason string
	// MidOperation is "merge" or "rebase" when the worktree is in the middle
	// of one (MERGE_HEAD, rebase-merge or rebase-apply exists); "" otherwise.
	// It feeds the policy only and is not part of the wire payload.
	MidOperation string
}

// Actions is the createPr/updatePr/push/updateFromBase policy, a pure function of the payload.
// Blockers are checked in order; the first wins. The RPCs re-check the same
// preconditions, so a stale payload can never make an action run.
func Actions(g TaskGit, c ActionCtx) map[string]Action {
	return map[string]Action{
		"createPr":       createPrAction(g, c),
		"updatePr":       updatePrAction(g, c),
		"push":           pushAction(g, c),
		"updateFromBase": updateFromBaseAction(g, c),
	}
}

func disabled(code, reason string) Action { return Action{Code: code, Reason: reason} }

func baseName(g TaskGit) string {
	if g.Base != nil && g.Base.Ref != "" {
		return "`" + g.Base.Ref + "`"
	}
	return "its base"
}

func createPrAction(g TaskGit, c ActionCtx) Action {
	switch {
	case g.Branch == nil:
		return disabled("task_no_worktree", "Create PR isn't available because this task has no worktree. Start a new task from this branch to open a pull request.")
	case g.Base == nil || !g.Base.Known:
		return disabled("base_unknown", "Create PR isn't available because this task's base commit is unknown. Set the base branch first.")
	case c.RunActive:
		return disabled("run_active", "Create PR isn't available yet because an agent is still working in this task. Wait for the run to finish, or stop it.")
	case c.NoOrigin:
		return disabled(forge.CodeNoRemote, "Create PR isn't available because this workspace has no `origin` remote, so there's nowhere to open a pull request. Add one with `git remote add origin <url>`.")
	case c.OriginNotForge:
		return disabled(forge.CodeNoRemote, "Create PR isn't available because this workspace's `origin` remote isn't a GitHub repository URL (it is a local path or another kind of remote), so there's nowhere to open a pull request.")
	case g.PR != nil && g.PR.State == "open":
		return disabled("pr_exists", "A pull request is already open for this branch.")
	case g.Commits != nil && g.Commits.SinceFork != nil && *g.Commits.SinceFork == 0:
		return disabled("no_commits", fmt.Sprintf("Create PR isn't available yet because this branch has no commits beyond %s. Commit your changes first.", baseName(g)))
	case c.ForgeCode != "":
		return disabled(c.ForgeCode, forgeReason("Create PR", c))
	}
	return Action{Enabled: true}
}

// forgeReason is the sentence for a forge_* blocker, naming the action.
func forgeReason(action string, c ActionCtx) string {
	if c.ForgeReason != "" {
		return c.ForgeReason
	}
	switch c.ForgeCode {
	case forge.CodeUnauthenticated:
		return action + " isn't available because GitHub isn't signed in on the daemon host. Run `gh auth login` there."
	case forge.CodeCLIMissing:
		return action + " isn't available because the GitHub CLI (`gh`) isn't installed on the daemon host. Install it, then run `gh auth login` there."
	case forge.CodeNoRemote:
		return action + " isn't available because this workspace's `origin` isn't a GitHub remote."
	}
	return action + " isn't available because the forge can't be reached from the daemon host."
}

// cherryPickPR reports whether pr was opened from the throwaway
// smind/pr-task-<id> branch (Update PR cannot follow a rebased copy).
func cherryPickPR(pr *PRInfo) bool {
	return pr != nil && strings.HasPrefix(pr.HeadRef, forge.CherryPickBranchPrefix)
}

func updatePrAction(g TaskGit, c ActionCtx) Action {
	switch {
	case g.Branch == nil:
		return disabled("task_no_worktree", "Update PR isn't available because this task has no worktree. Start a new task from this branch to push changes.")
	case g.PR == nil:
		return disabled("no_pr", "Update PR isn't available because this branch has no pull request. Create one first.")
	case g.PR.State != "open":
		return disabled("pr_not_open", fmt.Sprintf("Update PR isn't available because the pull request is %s. Create a new pull request from this branch instead.", g.PR.State))
	case g.PR.FromFork:
		return disabled("fork_pr_readonly", "Update PR isn't available because this pull request comes from a fork, and smind can't push to forks yet.")
	case cherryPickPR(g.PR):
		return disabled("update_unsupported", fmt.Sprintf("This pull request was opened from a rebased copy (`%s`), so smind can't update it automatically.", g.PR.HeadRef))
	case c.ForgeCode != "":
		return disabled(c.ForgeCode, forgeReason("Update PR", c))
	}
	return Action{Enabled: true}
}

// CodeUpToDate is updateFromBase's advisory code: the branch already holds
// everything its base has. The RPC does not refuse on it (it fetches first).
const CodeUpToDate = "up_to_date"

// CodeStatusUnknown means the facts the action needs have not been read yet.
const CodeStatusUnknown = "status_unknown"

func pushAction(g TaskGit, c ActionCtx) Action {
	switch {
	case g.Branch == nil:
		return disabled(forge.CodeTaskNoWorktree, "Push isn't available because this task has no worktree. Start a new task from this branch to push changes.")
	case c.NoOrigin:
		return disabled(forge.CodeNoRemote, "Push isn't available because this workspace has no `origin` remote, so there's nowhere to push. Add one with `git remote add origin <url>`.")
	case g.Origin == nil:
		return disabled(CodeStatusUnknown, "Checking this branch's remote state…")
	case g.Origin.Published && g.Origin.Behind != nil && *g.Origin.Behind > 0:
		return disabled(forge.CodeNonFastForward, "Push isn't available because origin has newer changes on this branch. Bring those changes in first (pull in the worktree, or ask the agent).")
	case g.Origin.Published && g.Origin.Ahead != nil && *g.Origin.Ahead == 0:
		return disabled(forge.CodeNothingToPush, "Nothing to push.")
	case !g.Origin.Published && g.Commits != nil && g.Commits.SinceFork != nil && *g.Commits.SinceFork == 0:
		return disabled(forge.CodeNothingToPush, "Nothing to push. Commit your changes first.")
	}
	return Action{Enabled: true}
}

func updateFromBaseAction(g TaskGit, c ActionCtx) Action {
	switch {
	case g.Branch == nil:
		return disabled(forge.CodeTaskNoWorktree, "Update from base isn't available because this task has no worktree. Start a new task from this branch to keep it current.")
	case g.Base == nil || !g.Base.Known:
		return disabled(forge.CodeBaseUnknown, "Update from base isn't available because this task's base commit is unknown. Set the base branch first.")
	case g.Base.Tip == "":
		return disabled(forge.CodeBaseNotFound, fmt.Sprintf("Update from base isn't available because %s wasn't found. Pick an existing base branch.", baseName(g)))
	case c.RunActive:
		return disabled(forge.CodeRunActive, "Update from base isn't available yet because an agent is still working in this task. Wait for the run to finish, or stop it.")
	case c.MidOperation != "":
		return disabled(forge.CodeMergeInProgress, fmt.Sprintf("Update from base isn't available because this worktree is in the middle of a %s. Finish it, or undo it with `git %s --abort` in the worktree.", c.MidOperation, c.MidOperation))
	case g.Worktree != nil && g.Worktree.Dirty:
		return disabled(forge.CodeWorktreeDirty, "Update from base isn't available because this worktree has uncommitted changes. Commit or stash them first.")
	case g.Commits != nil && g.Commits.BehindBase != nil && *g.Commits.BehindBase == 0:
		return disabled(CodeUpToDate, fmt.Sprintf("Already up to date with %s.", baseName(g)))
	}
	return Action{Enabled: true}
}
