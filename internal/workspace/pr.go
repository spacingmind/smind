package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// defaultBaseBranch is the base branch Manager.CreatePR opens a PR against
// when the caller doesn't specify one. This repo's own CONTRIBUTING.md
// documents "develop" as the integration branch every feature/fix branch
// targets; workspace has no per-workspace base-branch config field yet
// (store.Workspace carries no such column), so this is a fixed default
// rather than something read per-workspace -- see
// docs/plans/active/task-permission-ux.md's Item 5 Validation note.
const defaultBaseBranch = "develop"

// PRResult is the result of Manager.CreatePR.
type PRResult struct {
	URL string
}

// CreatePR opens a pull request for task id's branch against baseBranch (or
// defaultBaseBranch if empty), via the gh CLI.
//
// The task's branch was forked (by CreateTask) from whatever commit the
// workspace repo's checkout had as HEAD at the time -- not necessarily an
// ancestor of the base branch. If the workspace checkout itself was ahead
// of the base with unrelated, not-yet-pushed commits when the task's
// worktree was created (exactly the gap task-permission-ux.md's Item 5
// documents, from this project's own Session 1), pushing the task branch
// as-is and opening a PR against the base would stuff that unrelated
// history into the PR. So CreatePR always checks first: if the task
// branch's fork point is an ancestor of the (freshly-fetched) base branch,
// it pushes the task branch directly and opens the PR from it; otherwise
// it cherry-picks just the task's own commits (fork point..branch) onto a
// clean smind/pr-<id> branch forked from the base, pushes that instead, and
// opens the PR from it.
//
// Requires the task to have at least one commit beyond its fork point --
// uncommitted worktree changes are not swept in; commit them first (via
// task.commit) before calling this.
func (m *Manager) CreatePR(id int64, baseBranch string) (PRResult, error) {
	if strings.TrimSpace(baseBranch) == "" {
		baseBranch = defaultBaseBranch
	}

	t, err := m.store.GetTask(id)
	if err != nil {
		return PRResult{}, fmt.Errorf("create pr: %w", err)
	}
	if t.WorktreePath == nil || t.Branch == nil {
		return PRResult{}, fmt.Errorf("create pr %d: task has no worktree", id)
	}
	ws, err := m.store.GetWorkspace(t.WorkspaceID)
	if err != nil {
		return PRResult{}, fmt.Errorf("create pr %d: %w", id, err)
	}

	title := strings.TrimSpace(t.Title)
	if title == "" {
		title = fmt.Sprintf("smind task %d", id)
	}
	body := fmt.Sprintf("Opened from smind task #%d via task.createPr.", id)

	url, err := createPR(ws.Path, *t.WorktreePath, *t.Branch, baseBranch, id, title, body)
	if err != nil {
		return PRResult{}, fmt.Errorf("create pr %d: %w", id, err)
	}
	return PRResult{URL: url}, nil
}

// createPR is CreatePR's git/gh implementation, split out so Manager.CreatePR
// stays a thin store-lookup-then-delegate wrapper matching every other
// Manager method in this package.
func createPR(repoPath, worktreePath, branch, baseBranch string, taskID int64, title, body string) (string, error) {
	const remote = "origin"

	if err := gitFetchBranch(worktreePath, remote, baseBranch); err != nil {
		return "", err
	}
	remoteBase := remote + "/" + baseBranch

	forkPoint, err := taskDiffBase(worktreePath, branch)
	if err != nil {
		return "", fmt.Errorf("resolve base commit: %w", err)
	}

	commits, err := gitRevListReverse(worktreePath, forkPoint+".."+branch)
	if err != nil {
		return "", err
	}
	if len(commits) == 0 {
		return "", fmt.Errorf("task has no commits beyond its base; commit your changes (task.commit) before opening a PR")
	}

	clean, err := gitIsAncestor(worktreePath, forkPoint, remoteBase)
	if err != nil {
		return "", fmt.Errorf("check whether task branch's base has diverged: %w", err)
	}

	if clean {
		// Happy path: the task branch's fork point is already reachable
		// from the base, so its own history is exactly what the PR should
		// contain -- push it as-is and open the PR from the worktree it
		// already lives in.
		if err := gitPushBranch(worktreePath, remote, branch); err != nil {
			return "", err
		}
		return openGhPR(worktreePath, baseBranch, branch, title, body)
	}

	// Diverged base: the task branch carries history the base doesn't have,
	// from before the task's own work even started. Replay only the task's
	// own commits onto a clean branch forked from the base, in a throwaway
	// worktree, and open the PR from there instead.
	return createCleanPRBranchAndOpen(repoPath, remote, remoteBase, baseBranch, taskID, commits, title, body)
}

// openGhPR runs `gh pr create` in dir (a worktree/repo checkout with the
// right git remote configured) and returns the created PR's URL -- gh
// prints exactly that to stdout on success when given explicit
// title/body/base/head (no prompts to answer).
func openGhPR(dir, baseBranch, head, title, body string) (string, error) {
	out, err := runGH(dir, "pr", "create", "--base", baseBranch, "--head", head, "--title", title, "--body", body)
	if err != nil {
		return "", fmt.Errorf("gh pr create: %w", err)
	}
	url := strings.TrimSpace(out)
	if url == "" {
		return "", fmt.Errorf("gh pr create: no PR URL in output")
	}
	return url, nil
}

// createCleanPRBranchAndOpen materializes a throwaway worktree checked out
// on a fresh smind/pr-<taskID> branch forked from remoteBase, cherry-picks
// commits (oldest first) onto it, pushes it to remote, and opens the PR
// from it -- the diverged-base path createPR falls back to. The throwaway
// worktree is always cleaned up before returning, success or failure; the
// gh pr create call must happen before that cleanup, while the worktree
// (and the branch's remote push) are still in place.
func createCleanPRBranchAndOpen(repoPath, remote, remoteBase, baseBranch string, taskID int64, commits []string, title, body string) (url string, err error) {
	prBranch := "smind/pr-" + strconv.FormatInt(taskID, 10)

	container, err := os.MkdirTemp("", "smind-pr-")
	if err != nil {
		return "", fmt.Errorf("create throwaway pr worktree dir: %w", err)
	}
	wt := filepath.Join(container, "wt")
	defer func() {
		if rmErr := gitWorktreeRemove(repoPath, wt); rmErr != nil && err == nil {
			err = fmt.Errorf("clean up throwaway pr worktree: %w", rmErr)
		}
		_ = os.RemoveAll(container)
	}()

	if err := gitWorktreeAddFrom(repoPath, wt, prBranch, remoteBase); err != nil {
		return "", fmt.Errorf("create clean %s branch from %s: %w", prBranch, baseBranch, err)
	}
	for _, sha := range commits {
		if err := gitCherryPick(wt, sha); err != nil {
			return "", fmt.Errorf("replay task commits onto clean %s branch: %w", prBranch, err)
		}
	}
	if err := gitPushBranch(wt, remote, prBranch); err != nil {
		return "", err
	}
	return openGhPR(wt, baseBranch, prBranch, title, body)
}
