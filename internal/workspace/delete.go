package workspace

import (
	"fmt"

	"github.com/spacingmind/smind/internal/store"
)

// DeleteSummary reports how many tasks and spaces a DeleteTask/DeleteSpace/
// DeleteWorkspace call actually removed, so a caller (the wsapi handler) can
// hand back an accurate "removed N tasks, M spaces" confirmation without a
// second round trip.
type DeleteSummary struct {
	TasksRemoved  int
	SpacesRemoved int
}

// checkpointAndRemoveWorktree checkpoints t's git worktree (if any) onto its
// branch and then removes the worktree directory -- the shared git-cleanup
// step ArchiveTask and DeleteTask/DeleteSpace/DeleteWorkspace all need
// before they may touch the store layer, extracted here rather than
// duplicated across them.
//
// The checkpoint (gitWorktreeCheckpoint) commits everything currently
// uncommitted in the worktree -- including untracked files -- so the branch
// alone retains all work even after the worktree directory is gone; nothing
// reviewable is destroyed by the subsequent `git worktree remove --force`.
// A checkpoint failure returns before any removal happens: losing the
// worktree with the checkpoint still unwritten is exactly the data-loss bug
// this ordering guards against.
//
// If the worktree directory no longer exists on disk (e.g. it was already
// removed by a prior archive, or is externally gone), that's not treated as
// a failure and nothing runs: the directory being gone is exactly the end
// state this is trying to reach.
func (m *Manager) checkpointAndRemoveWorktree(t store.Task) error {
	if t.WorktreePath == nil || !dirExists(*t.WorktreePath) {
		return nil
	}
	if err := gitWorktreeCheckpoint(*t.WorktreePath); err != nil {
		return fmt.Errorf("checkpoint task %d worktree: %w", t.ID, err)
	}

	ws, err := m.store.GetWorkspace(t.WorkspaceID)
	if err != nil {
		return fmt.Errorf("checkpoint task %d worktree: %w", t.ID, err)
	}
	if err := gitWorktreeRemove(ws.Path, *t.WorktreePath); err != nil {
		return fmt.Errorf("checkpoint task %d worktree: %w", t.ID, err)
	}
	return nil
}

// DeleteTask checkpoints and removes id's git worktree (if any, tolerating
// one that's already gone -- see checkpointAndRemoveWorktree), then deletes
// its store row (and everything scoped to it: runs, run_events, terminal
// sessions -- see store.DeleteTask). A checkpoint failure aborts before the
// store row is touched, leaving both the task and its worktree intact.
//
// This never touches anything on disk outside smind's own worktree
// directory for the task: the workspace's actual repo path is read-only
// from smind's perspective, and the task row itself is only smind's own
// tracking of the task, not the underlying work.
func (m *Manager) DeleteTask(id int64) (DeleteSummary, error) {
	t, err := m.store.GetTask(id)
	if err != nil {
		return DeleteSummary{}, fmt.Errorf("delete task: %w", err)
	}
	if err := m.checkpointAndRemoveWorktree(t); err != nil {
		return DeleteSummary{}, fmt.Errorf("delete task %d: %w", id, err)
	}
	if err := m.store.DeleteTask(id); err != nil {
		return DeleteSummary{}, fmt.Errorf("delete task %d: %w", id, err)
	}
	return DeleteSummary{TasksRemoved: 1}, nil
}

// DeleteSpace checkpoints and removes the git worktree of every task in
// spaceID (all of them, not just the first, and tolerating any that are
// already archived/worktree-less), then deletes the space and its tasks
// from the store in one call (store.DeleteSpace) -- only once every task's
// git cleanup has succeeded. A checkpoint failure on any task aborts before
// the store is touched at all, leaving the database exactly as it was.
func (m *Manager) DeleteSpace(id int64) (DeleteSummary, error) {
	if _, err := m.store.GetSpace(id); err != nil {
		return DeleteSummary{}, fmt.Errorf("delete space: %w", err)
	}
	tasks, err := m.store.ListTasksBySpace(id)
	if err != nil {
		return DeleteSummary{}, fmt.Errorf("delete space %d: %w", id, err)
	}
	for _, t := range tasks {
		if err := m.checkpointAndRemoveWorktree(t); err != nil {
			return DeleteSummary{}, fmt.Errorf("delete space %d: %w", id, err)
		}
	}

	if err := m.store.DeleteSpace(id); err != nil {
		return DeleteSummary{}, fmt.Errorf("delete space %d: %w", id, err)
	}
	return DeleteSummary{TasksRemoved: len(tasks), SpacesRemoved: 1}, nil
}

// DeleteWorkspace checkpoints and removes the git worktree of every task the
// workspace's cascade sweeps up -- every task in every space, plus every
// ungrouped task -- before deleting anything from the store. Like
// DeleteSpace, all git-level cleanup for the whole cascade must succeed
// before the single store.DeleteWorkspace call runs (which itself cascades
// to workspace_accounts, every space and its tasks, and every ungrouped
// task at the database layer): a checkpoint failure partway through leaves
// the database completely untouched, not a partially-deleted tree.
func (m *Manager) DeleteWorkspace(id int64) (DeleteSummary, error) {
	if _, err := m.store.GetWorkspace(id); err != nil {
		return DeleteSummary{}, fmt.Errorf("delete workspace: %w", err)
	}
	spaces, err := m.store.ListSpacesByWorkspace(id)
	if err != nil {
		return DeleteSummary{}, fmt.Errorf("delete workspace %d: %w", id, err)
	}

	var tasks []store.Task
	for _, sp := range spaces {
		spaceTasks, err := m.store.ListTasksBySpace(sp.ID)
		if err != nil {
			return DeleteSummary{}, fmt.Errorf("delete workspace %d: %w", id, err)
		}
		tasks = append(tasks, spaceTasks...)
	}
	ungrouped, err := m.store.ListUngroupedTasksByWorkspace(id)
	if err != nil {
		return DeleteSummary{}, fmt.Errorf("delete workspace %d: %w", id, err)
	}
	tasks = append(tasks, ungrouped...)

	for _, t := range tasks {
		if err := m.checkpointAndRemoveWorktree(t); err != nil {
			return DeleteSummary{}, fmt.Errorf("delete workspace %d: %w", id, err)
		}
	}

	if err := m.store.DeleteWorkspace(id); err != nil {
		return DeleteSummary{}, fmt.Errorf("delete workspace %d: %w", id, err)
	}
	return DeleteSummary{TasksRemoved: len(tasks), SpacesRemoved: len(spaces)}, nil
}
