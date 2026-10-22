package gitstate

import "fmt"

// ActionCtx carries what the pure policy cannot read from TaskGit.
type ActionCtx struct {
	RunActive bool
	// ForgeCode/ForgeReason pass a forge_* blocker through (empty in PR 3).
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
	case g.Commits != nil && g.Commits.SinceFork != nil && *g.Commits.SinceFork == 0:
		return disabled("no_commits", fmt.Sprintf("Create PR isn't available yet because this branch has no commits beyond %s. Commit your changes first.", baseName(g)))
	case g.PR != nil && g.PR.State == "open":
		return disabled("pr_exists", "A pull request is already open for this branch.")
	case c.ForgeCode != "":
		return disabled(c.ForgeCode, c.ForgeReason)
	}
	return Action{Enabled: true}
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
	case c.ForgeCode != "":
		return disabled(c.ForgeCode, c.ForgeReason)
	}
	return Action{Enabled: true}
}
