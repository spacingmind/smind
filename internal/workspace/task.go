package workspace

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/store"
)

// CreateTask creates a task under workspaceID, materializing a real git
// worktree checked out on a new branch before any database row exists.
//
// Worktree layout: worktrees live under
// <config.Dir()>/worktrees/<workspace-id>/<slug>, i.e. under smind's own
// home directory rather than beside the workspace (e.g. workspace.Path +
// "/.."). This keeps all of smind's on-disk state under one root
// regardless of where workspaces happen to live, avoids littering sibling
// directories next to a workspace's own repo checkout where they could
// confuse the workspace owner's tooling, and mirrors how Paseo lays out its
// own agent worktrees in a dedicated directory tree outside the repos they
// check out.
//
// The worktree directory name (and branch name) is derived from the task
// title, not the task's future database id: the worktree must exist before
// the Task row is inserted (a worktree that fails to materialize must not
// leave a DB row behind), so the id isn't known yet at creation time. A
// slug plus a nanosecond timestamp suffix keeps the name readable while
// guaranteeing it won't collide with another task sharing the same title.
func (m *Manager) CreateTask(workspaceID int64, spaceID *int64, title string) (store.Task, error) {
	ws, err := m.store.GetWorkspace(workspaceID)
	if err != nil {
		return store.Task{}, fmt.Errorf("create task: %w", err)
	}

	name := slugify(title) + "-" + strconv.FormatInt(time.Now().UTC().UnixNano(), 36)
	worktreePath := filepath.Join(config.Dir(), "worktrees", strconv.FormatInt(workspaceID, 10), name)
	branch := "smind/task-" + name

	if err := gitWorktreeAdd(ws.Path, worktreePath, branch); err != nil {
		return store.Task{}, fmt.Errorf("create task worktree: %w", err)
	}

	t, err := m.store.CreateTask(store.Task{
		WorkspaceID:  workspaceID,
		SpaceID:      spaceID,
		Title:        title,
		Status:       "created",
		WorktreePath: &worktreePath,
		Branch:       &branch,
	})
	if err != nil {
		return store.Task{}, fmt.Errorf("create task: %w", err)
	}
	return t, nil
}

var slugInvalidRunRE = regexp.MustCompile(`[^a-z0-9]+`)

// slugify lowercases title and collapses any run of non-alphanumeric
// characters into a single hyphen, for use in worktree directory and branch
// names. An empty or all-punctuation title falls back to "task".
func slugify(title string) string {
	slug := strings.Trim(slugInvalidRunRE.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if slug == "" {
		return "task"
	}
	return slug
}

// RunTask transitions a task from "created" to "running". It does not spawn
// an agent — that's a separate ACP integration task — this only flips the
// lifecycle state so that work has a hook point.
func (m *Manager) RunTask(id int64) (store.Task, error) {
	t, err := m.store.GetTask(id)
	if err != nil {
		return store.Task{}, fmt.Errorf("run task: %w", err)
	}
	if t.Status != "created" {
		return store.Task{}, fmt.Errorf("run task %d: status is %q, want \"created\"", id, t.Status)
	}
	t, err = m.store.UpdateTaskStatus(id, "running")
	if err != nil {
		return store.Task{}, fmt.Errorf("run task %d: %w", id, err)
	}
	return t, nil
}

// ArchiveTask removes the task's git worktree (if any) and marks it
// archived. It uses `git worktree remove --force` since a task worktree is
// disposable and archiving is meant to discard it regardless of any
// uncommitted changes.
//
// If the worktree directory no longer exists on disk (e.g. it was already
// removed some other way), that's not treated as a failure: the directory
// being gone is exactly the end state archiving is trying to reach.
func (m *Manager) ArchiveTask(id int64) (store.Task, error) {
	t, err := m.store.GetTask(id)
	if err != nil {
		return store.Task{}, fmt.Errorf("archive task: %w", err)
	}

	if t.WorktreePath != nil && dirExists(*t.WorktreePath) {
		ws, err := m.store.GetWorkspace(t.WorkspaceID)
		if err != nil {
			return store.Task{}, fmt.Errorf("archive task %d: %w", id, err)
		}
		if err := gitWorktreeRemove(ws.Path, *t.WorktreePath); err != nil {
			return store.Task{}, fmt.Errorf("archive task %d: %w", id, err)
		}
	}

	t, err = m.store.ArchiveTask(id)
	if err != nil {
		return store.Task{}, fmt.Errorf("archive task %d: %w", id, err)
	}
	return t, nil
}

// ListTasks returns all tasks for workspaceID, ordered by id.
func (m *Manager) ListTasks(workspaceID int64) ([]store.Task, error) {
	return m.store.ListTasksByWorkspace(workspaceID)
}

// GetTask returns the task with the given id.
func (m *Manager) GetTask(id int64) (store.Task, error) {
	return m.store.GetTask(id)
}

// Diff returns task id's full unified diff: everything changed in its git
// worktree relative to the commit its branch was created from -- see
// taskDiff (git.go) for the exact git invocation and why. A task with no
// changes at all returns an empty string, not an error.
func (m *Manager) Diff(id int64) (string, error) {
	t, err := m.store.GetTask(id)
	if err != nil {
		return "", fmt.Errorf("task diff: %w", err)
	}
	if t.WorktreePath == nil || t.Branch == nil {
		return "", fmt.Errorf("task diff %d: task has no worktree", id)
	}
	diff, err := taskDiff(*t.WorktreePath, *t.Branch)
	if err != nil {
		return "", fmt.Errorf("task diff %d: %w", id, err)
	}
	return diff, nil
}
