package workspace

import (
	"os"
	"strings"
)

// BackfillTaskBases is the one-time ADR-0039 migration, run at daemon
// start. For every non-system, non-archived task that has a worktree and no
// base_sha, it computes the base the old way one last time (the oldest
// reflog entry of the task's branch), stores it with base_ref (the
// workspace's default base) and branch_owned, each task in its own
// statement. Idempotent and best effort: a task whose base cannot be
// recovered is left untouched (base_unknown until task.setBase), and it
// returns how many tasks were filled. After this, nothing else reads the
// reflog (TestNoReflogReaders).
func (m *Manager) BackfillTaskBases() int {
	tasks, err := m.store.ListTasksNeedingBase()
	if err != nil {
		return 0
	}
	filled := 0
	for _, t := range tasks {
		if t.WorktreePath == nil || t.Branch == nil {
			continue
		}
		ws, err := m.store.GetWorkspace(t.WorkspaceID)
		if err != nil {
			continue
		}
		dir := *t.WorktreePath
		if !dirExists(dir) {
			dir = ws.Path // the branch's reflog lives in the shared repo
		}
		sha, err := legacyReflogBase(dir, *t.Branch)
		if err != nil || !shaRE.MatchString(sha) {
			continue
		}
		// The branch-name test below is the ONLY heuristic use of the name
		// (ADR-0039 section 2), and only at migration.
		owned := strings.HasPrefix(*t.Branch, "smind/task-") || strings.HasPrefix(*t.Branch, "smind/pr-")
		if ok, err := m.store.BackfillTaskBase(t.ID, workspaceDefaultBase(ws), sha, owned); err == nil && ok {
			filled++
		}
	}
	return filled
}

// legacyReflogBase returns the full commit hash branch pointed at the moment
// it was created, found via branch's own reflog: the oldest entry is the
// fork point `git worktree add -b branch` recorded. It is reachable only
// from BackfillTaskBases; the stored base_sha replaced it everywhere else.
func legacyReflogBase(dir, branch string) (string, error) {
	if _, err := os.Stat(dir); err != nil {
		return "", err
	}
	out, err := runGitOutput(dir, "log", "-g", "--format=%H", "refs/heads/"+branch)
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	return lines[len(lines)-1], nil
}
