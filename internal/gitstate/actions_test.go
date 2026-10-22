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
			name:       "pr merged",
			g:          TaskGit{Branch: branch, Base: known, Commits: withCommits, PR: &PRInfo{State: "merged"}},
			updateCode: "pr_not_open",
			updateMsg:  "Update PR isn't available because the pull request is merged. Create a new pull request from this branch instead.",
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
