package gitstate

import "testing"

func TestActions_DisabledReasons_Table(t *testing.T) {
	branch := &Branch{Name: "smind/task-x"}
	known := &Base{Ref: "develop", Sha: "x", Known: true}
	withCommits := &Commits{SinceFork: ip(2)}
	open := &PRInfo{State: "open"}

	tests := []struct {
		name       string
		g          TaskGit
		c          ActionCtx
		createCode string
		createMsg  string
		updateCode string
		updateMsg  string
	}{
		{
			name:       "no worktree",
			g:          TaskGit{Base: known},
			createCode: "task_no_worktree",
			createMsg:  "Create PR isn't available because this task has no worktree. Start a new task from this branch to open a pull request.",
			updateCode: "task_no_worktree",
			updateMsg:  "Update PR isn't available because this task has no worktree. Start a new task from this branch to push changes.",
		},
		{
			name:       "base unknown",
			g:          TaskGit{Branch: branch, Base: &Base{Ref: "develop"}, Commits: withCommits},
			createCode: "base_unknown",
			createMsg:  "Create PR isn't available because this task's base commit is unknown. Set the base branch first.",
			updateCode: "no_pr",
			updateMsg:  "Update PR isn't available because this branch has no pull request. Create one first.",
		},
		{
			name:       "run active",
			g:          TaskGit{Branch: branch, Base: known, Commits: withCommits},
			c:          ActionCtx{RunActive: true},
			createCode: "run_active",
			createMsg:  "Create PR isn't available yet because an agent is still working in this task. Wait for the run to finish, or stop it.",
			updateCode: "no_pr",
			updateMsg:  "Update PR isn't available because this branch has no pull request. Create one first.",
		},
		{
			name:       "no commits",
			g:          TaskGit{Branch: branch, Base: known, Commits: &Commits{SinceFork: ip(0)}},
			createCode: "no_commits",
			createMsg:  "Create PR isn't available yet because this branch has no commits beyond `develop`. Commit your changes first.",
			updateCode: "no_pr",
			updateMsg:  "Update PR isn't available because this branch has no pull request. Create one first.",
		},
		{
			name:       "pr exists",
			g:          TaskGit{Branch: branch, Base: known, Commits: withCommits, PR: open},
			createCode: "pr_exists",
			createMsg:  "A pull request is already open for this branch.",
		},
		{
			name:       "pr exists beats no commits",
			g:          TaskGit{Branch: branch, Base: known, Commits: &Commits{SinceFork: ip(0)}, PR: open},
			createCode: "pr_exists",
			createMsg:  "A pull request is already open for this branch.",
		},
		{
			name:       "pr merged",
			g:          TaskGit{Branch: branch, Base: known, Commits: withCommits, PR: &PRInfo{State: "merged"}},
			updateCode: "pr_not_open",
			updateMsg:  "Update PR isn't available because the pull request is merged. Create a new pull request from this branch instead.",
		},
		{
			name:       "fork pr is read-only",
			g:          TaskGit{Branch: branch, Base: known, Commits: withCommits, PR: &PRInfo{State: "open", FromFork: true}},
			createCode: "pr_exists",
			createMsg:  "A pull request is already open for this branch.",
			updateCode: "fork_pr_readonly",
			updateMsg:  "Update PR isn't available because this pull request comes from a fork, and smind can't push to forks yet.",
		},
		{
			name:       "closed fork pr reports not open first",
			g:          TaskGit{Branch: branch, Base: known, Commits: withCommits, PR: &PRInfo{State: "closed", FromFork: true}},
			updateCode: "pr_not_open",
			updateMsg:  "Update PR isn't available because the pull request is closed. Create a new pull request from this branch instead.",
		},
		{
			name:       "no origin (U7)",
			g:          TaskGit{Branch: branch, Base: known, Commits: withCommits},
			c:          ActionCtx{NoOrigin: true},
			createCode: "forge_no_remote",
			createMsg:  "Create PR isn't available because this workspace has no `origin` remote, so there's nowhere to open a pull request. Add one with `git remote add origin <url>`.",
			updateCode: "no_pr",
			updateMsg:  "Update PR isn't available because this branch has no pull request. Create one first.",
		},
		{
			name:       "origin that is not a forge URL (U7)",
			g:          TaskGit{Branch: branch, Base: known, Commits: withCommits},
			c:          ActionCtx{OriginNotForge: true},
			createCode: "forge_no_remote",
			createMsg:  "Create PR isn't available because this workspace's `origin` remote isn't a GitHub repository URL (it is a local path or another kind of remote), so there's nowhere to open a pull request.",
			updateCode: "no_pr",
			updateMsg:  "Update PR isn't available because this branch has no pull request. Create one first.",
		},
		{
			name:       "run active beats no origin",
			g:          TaskGit{Branch: branch, Base: known, Commits: withCommits},
			c:          ActionCtx{NoOrigin: true, RunActive: true},
			createCode: "run_active",
			createMsg:  "Create PR isn't available yet because an agent is still working in this task. Wait for the run to finish, or stop it.",
			updateCode: "no_pr",
			updateMsg:  "Update PR isn't available because this branch has no pull request. Create one first.",
		},
		{
			name:       "no origin beats pr exists",
			g:          TaskGit{Branch: branch, Base: known, Commits: withCommits, PR: open},
			c:          ActionCtx{NoOrigin: true},
			createCode: "forge_no_remote",
			createMsg:  "Create PR isn't available because this workspace has no `origin` remote, so there's nowhere to open a pull request. Add one with `git remote add origin <url>`.",
		},
		{
			name:       "base unknown beats run active",
			g:          TaskGit{Branch: branch, Base: &Base{Ref: "develop"}, Commits: withCommits},
			c:          ActionCtx{RunActive: true},
			createCode: "base_unknown",
			createMsg:  "Create PR isn't available because this task's base commit is unknown. Set the base branch first.",
			updateCode: "no_pr",
			updateMsg:  "Update PR isn't available because this branch has no pull request. Create one first.",
		},
		{
			name:       "cherry-pick PR cannot be updated",
			g:          TaskGit{Branch: branch, Base: known, Commits: withCommits, PR: &PRInfo{State: "open", HeadRef: "smind/pr-task-12"}},
			createCode: "pr_exists",
			createMsg:  "A pull request is already open for this branch.",
			updateCode: "update_unsupported",
			updateMsg:  "This pull request was opened from a rebased copy (`smind/pr-task-12`), so smind can't update it automatically.",
		},
		{
			name:       "forge not signed in, standard sentence",
			g:          TaskGit{Branch: branch, Base: known, Commits: withCommits, PR: open},
			c:          ActionCtx{ForgeCode: "forge_unauthenticated"},
			createCode: "pr_exists",
			createMsg:  "A pull request is already open for this branch.",
			updateCode: "forge_unauthenticated",
			updateMsg:  "Update PR isn't available because GitHub isn't signed in on the daemon host. Run `gh auth login` there.",
		},
		{
			name:       "gh missing, standard sentence",
			g:          TaskGit{Branch: branch, Base: known, Commits: withCommits},
			c:          ActionCtx{ForgeCode: "forge_cli_missing"},
			createCode: "forge_cli_missing",
			createMsg:  "Create PR isn't available because the GitHub CLI (`gh`) isn't installed on the daemon host. Install it, then run `gh auth login` there.",
			updateCode: "no_pr",
			updateMsg:  "Update PR isn't available because this branch has no pull request. Create one first.",
		},
		{
			name:       "forge passthrough",
			g:          TaskGit{Branch: branch, Base: known, Commits: withCommits},
			c:          ActionCtx{ForgeCode: "forge_unauthenticated", ForgeReason: "GitHub isn't connected. Sign in under Settings → GitHub."},
			createCode: "forge_unauthenticated",
			createMsg:  "GitHub isn't connected. Sign in under Settings → GitHub.",
			updateCode: "no_pr",
			updateMsg:  "Update PR isn't available because this branch has no pull request. Create one first.",
		},
		{
			name: "all clear",
			g:    TaskGit{Branch: branch, Base: known, Commits: withCommits},
			// createPr enabled; updatePr has no PR.
			updateCode: "no_pr",
			updateMsg:  "Update PR isn't available because this branch has no pull request. Create one first.",
		},
		{
			name: "commit count unknown does not block",
			g:    TaskGit{Branch: branch, Base: known, Commits: &Commits{}},
			// enabled: the RPC re-checks.
			updateCode: "no_pr",
			updateMsg:  "Update PR isn't available because this branch has no pull request. Create one first.",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Actions(tc.g, tc.c)
			check := func(key, code, msg string) {
				a := got[key]
				if code == "" {
					if !a.Enabled || a.Code != "" || a.Reason != "" {
						t.Errorf("%s = %+v, want enabled", key, a)
					}
					return
				}
				if a.Enabled || a.Code != code || a.Reason != msg {
					t.Errorf("%s = %+v, want disabled %q %q", key, a, code, msg)
				}
			}
			check("createPr", tc.createCode, tc.createMsg)
			check("updatePr", tc.updateCode, tc.updateMsg)
		})
	}
}

func TestActions_UpdatePrEnabledForOpenPR(t *testing.T) {
	g := TaskGit{Branch: &Branch{}, PR: &PRInfo{State: "open"}}
	if a := Actions(g, ActionCtx{})["updatePr"]; !a.Enabled {
		t.Fatalf("updatePr = %+v, want enabled", a)
	}
}
