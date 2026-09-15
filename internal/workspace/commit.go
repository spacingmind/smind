package workspace

import (
	"fmt"
	"strings"
	"sync"
)

// TaskFile is one entry in a task's changed-files list: a path that differs
// between the task's base commit and its worktree, the kind of change it
// represents, and whether the file currently has changes staged in the
// worktree's real index (per git status --porcelain's index column).
type TaskFile struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	Staged bool   `json:"staged"`
}

// CommitResult describes a commit made via Manager.CommitTask: the new
// commit's full SHA, its subject line (first line of the message, before
// any trailers), and how many files it recorded.
type CommitResult struct {
	Commit  string `json:"commit"`
	Subject string `json:"subject"`
	Files   int    `json:"files"`
}

// ErrNothingStaged is returned by CommitTask when the worktree's index has
// nothing staged -- the one commit failure callers must be able to present
// as a clean "stage something first" message rather than a git stderr
// leak. (A staged-but-identical-to-HEAD file is still "nothing" to git
// itself; both cases surface here.)
var ErrNothingStaged = fmt.Errorf("nothing staged to commit")

// TaskFiles returns the task's changed files: one entry per path in the
// same base→worktree diff Manager.Diff computes, cross-referenced with the
// worktree's real index state so each entry says whether it's staged.
// A task with no changes returns an empty slice, not an error.
func (m *Manager) TaskFiles(id int64) ([]TaskFile, error) {
	wt, branch, err := m.taskWorktree(id)
	if err != nil {
		return nil, err
	}
	files, err := taskChangedFiles(wt, branch)
	if err != nil {
		return nil, fmt.Errorf("task files %d: %w", id, err)
	}
	return files, nil
}

// TaskFileDiff returns the unified diff for exactly one path of the task's
// base→worktree diff (the same computation Manager.Diff runs, sliced by
// path -- no new git invocation shape). A path with no changes returns an
// empty string, not an error.
func (m *Manager) TaskFileDiff(id int64, path string) (string, error) {
	wt, branch, err := m.taskWorktree(id)
	if err != nil {
		return "", err
	}
	diff, err := taskFileDiff(wt, branch, path)
	if err != nil {
		return "", fmt.Errorf("task file diff %d %q: %w", id, path, err)
	}
	return diff, nil
}

// TaskStage stages (staged=true, `git add -- <path>`) or unstages
// (staged=false, `git restore --staged -- <path>`) a single file in the
// task worktree's real index. Unlike Diff/TaskFiles/TaskFileDiff's
// throwaway snapshot index, this is a real mutation -- that's the point.
func (m *Manager) TaskStage(id int64, path string, staged bool) error {
	wt, _, err := m.taskWorktree(id)
	if err != nil {
		return err
	}
	if err := taskStageFile(wt, path, staged); err != nil {
		return fmt.Errorf("task stage %d %q: %w", id, path, err)
	}
	return nil
}

// CommitTask commits exactly what is currently staged in the task
// worktree's index -- no -a, no implicit `git add`. The message is written
// by the caller (ADR 0006: the daemon never generates commit messages).
//
// author is "human" (message used verbatim) or "agent" (message gains
// `Smind-Agent: <agent>` and `Smind-Task: <id>` trailers per ADR 0006;
// agent must then be non-empty). Returns ErrNothingStaged when the index
// has nothing staged.
func (m *Manager) CommitTask(id int64, message, author, agent string) (CommitResult, error) {
	wt, _, err := m.taskWorktree(id)
	if err != nil {
		return CommitResult{}, err
	}
	if strings.TrimSpace(message) == "" {
		return CommitResult{}, fmt.Errorf("commit task %d: message is empty", id)
	}

	fullMessage := message
	switch author {
	case "human":
	case "agent":
		if agent == "" {
			return CommitResult{}, fmt.Errorf("commit task %d: agent commits require an agent (provider) name", id)
		}
		fullMessage = message + "\n\nSmind-Agent: " + agent + "\nSmind-Task: " + fmt.Sprint(id)
	default:
		return CommitResult{}, fmt.Errorf("commit task %d: author must be \"human\" or \"agent\", got %q", id, author)
	}

	result, err := gitTaskCommit(wt, fullMessage)
	if err != nil {
		return CommitResult{}, fmt.Errorf("commit task %d: %w", id, err)
	}
	return result, nil
}

// taskWorktree resolves id's worktree path and branch -- the fields every
// git-touching task operation needs.
func (m *Manager) taskWorktree(id int64) (worktreePath, branch string, err error) {
	t, err := m.store.GetTask(id)
	if err != nil {
		return "", "", fmt.Errorf("task %d: %w", id, err)
	}
	if t.WorktreePath == nil || t.Branch == nil {
		return "", "", fmt.Errorf("task %d: task has no worktree", id)
	}
	return *t.WorktreePath, *t.Branch, nil
}

// TaskStat is the sidebar's per-task git signal: the task's branch and the
// size of the same base→worktree diff task.diff/task.files compute,
// reduced to counts. It exists so the sidebar can show every task's diff
// size without fetching every task's file list.
type TaskStat struct {
	TaskID       int64  `json:"taskId"`
	Branch       string `json:"branch"`
	FilesChanged int    `json:"filesChanged"`
	Insertions   int    `json:"insertions"`
	Deletions    int    `json:"deletions"`
}

// taskStatWorkers bounds how many worktrees TaskStats scans at once. Each
// one runs `git add -A` into a throwaway index (snapshotIndex), so this is
// disk-bound work that neither benefits from unbounded fan-out nor should
// be allowed to occupy every core on a workspace with many tasks.
const taskStatWorkers = 4

// TaskStats returns one entry per non-archived task in the workspace that
// has a worktree to diff, for the sidebar's branch and diff-stat signal.
//
// Deliberately partial: a task with no worktree yet (created but never
// run) has nothing to diff and is omitted, and a task whose stat *fails*
// -- a worktree deleted out from under the daemon, a branch with no
// reflog -- is also omitted rather than failing the whole call. One
// broken worktree must not blank every other row's signal, and the UI's
// rule for this surface is that a number it cannot compute is absent, not
// zero (a zeroed stat would read as "no changes", which is a different
// and wrong statement).
func (m *Manager) TaskStats(workspaceID int64) ([]TaskStat, error) {
	tasks, err := m.ListTasks(workspaceID)
	if err != nil {
		return nil, err
	}

	// Index-aligned results, so the output keeps ListTasks' id order
	// regardless of which worker finishes first.
	stats := make([]*TaskStat, len(tasks))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range taskStatWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				t := tasks[i]
				if t.WorktreePath == nil || t.Branch == nil {
					continue
				}
				files, ins, del, err := taskDiffStat(*t.WorktreePath, *t.Branch)
				if err != nil {
					continue
				}
				stats[i] = &TaskStat{
					TaskID:       t.ID,
					Branch:       *t.Branch,
					FilesChanged: files,
					Insertions:   ins,
					Deletions:    del,
				}
			}
		}()
	}
	for i := range tasks {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	out := make([]TaskStat, 0, len(tasks))
	for _, s := range stats {
		if s != nil {
			out = append(out, *s)
		}
	}
	return out, nil
}
