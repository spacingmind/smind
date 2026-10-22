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
}

// Actions is the createPr/updatePr policy, a pure function of the payload.
// Blockers are checked in order; the first wins. The RPCs re-check the same
// preconditions, so a stale payload can never make an action run.
func Actions(g TaskGit, c ActionCtx) map[string]Action {
	return map[string]Action{
		"createPr": createPrAction(g, c),
		"updatePr": updatePrAction(g, c),
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
