package wsapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/tasktree"
	"github.com/spacingmind/smind/internal/workspace"
)

// Test seams for task.archiveFinished / task.stopTree (PR-H11). Production
// values are the defaults below; tests swap them (non-parallel tests only,
// restoring via t.Cleanup) to make the cap, the shared stop deadline, an
// archive failure and the mid-operation races deterministic.
var (
	// treeArchiveCap bounds how many tasks one task.archiveFinished call
	// archives; the rest are reported in `remaining`.
	treeArchiveCap = 100
	// treeStopDeadline is the one shared deadline for every run of a
	// task.stopTree call to reach a terminal state.
	treeStopDeadline = 10 * time.Second
	// treeArchiveTask archives one task (checkpoint-first); production is
	// workspace.Manager.ArchiveTask.
	treeArchiveTask = func(wm *workspace.Manager, id int64) error {
		_, err := wm.ArchiveTask(id)
		return err
	}
	// treeAwaitRun stops runID and waits for it to be terminal or ctx to
	// end; production is Registry.StopAndWait.
	treeAwaitRun = func(ctx context.Context, reg *runs.Registry, runID string) error {
		return reg.StopAndWait(ctx, runID)
	}
	// treeAfterSelection, when non-nil, runs once after the selection is
	// computed and before anything is archived/stopped -- the window in
	// which a run can start or finish under the operation.
	treeAfterSelection func()
)

// maxTreeStopPasses is the first pass plus at most two rescans for runs a
// still-live parent started mid-operation.
const maxTreeStopPasses = 3

type treeItem struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

type treeSkipped struct {
	ID     int64  `json:"id"`
	Title  string `json:"title"`
	Reason string `json:"reason"`
}

type treeFailed struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Error string `json:"error"`
}

type treeRun struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	RunID string `json:"runId"`
}

type treeRunFailed struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	RunID string `json:"runId"`
	Error string `json:"error"`
}

type archiveFinishedResult struct {
	DryRun    bool          `json:"dryRun"`
	Eligible  []treeItem    `json:"eligible"`
	Archived  []treeItem    `json:"archived"`
	Skipped   []treeSkipped `json:"skipped"`
	Failed    []treeFailed  `json:"failed"`
	Remaining int           `json:"remaining"`
}

type stopTreeResult struct {
	DryRun          bool            `json:"dryRun"`
	Runs            []treeRun       `json:"runs"`
	QueuedPrompts   int             `json:"queuedPrompts"`
	Stopped         []treeRun       `json:"stopped"`
	Failed          []treeRunFailed `json:"failed"`
	QueuedCancelled int             `json:"queuedCancelled"`
}

// loadActionTree validates {taskId} and returns the task's live tree
// (the task first, DFS pre-order), refusing a truncated tree: an action
// over a partial tree could archive an ancestor whose descendants were
// cut off, or miss runs.
func loadActionTree(db *store.Store, reg *runs.Registry, method string, taskID int64) ([]tasktree.Node, error) {
	if taskID == 0 {
		return nil, fmt.Errorf("%s: taskId is required", method)
	}
	t, err := db.GetTask(taskID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%s: unknown task %d", method, taskID)
		}
		return nil, fmt.Errorf("%s: get task %d: %w", method, taskID, err)
	}
	if t.ArchivedAt != nil {
		return nil, fmt.Errorf("%s: task %d is archived", method, taskID)
	}
	nodes, truncated, err := buildTaskTree(db, reg, t.WorkspaceID, tasktree.Root{TaskID: taskID})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	if truncated {
		return nil, fmt.Errorf("%s: tree of task %d has more than %d tasks", method, taskID, tasktree.MaxNodes)
	}
	return nodes, nil
}

func parseTreeActionParams(method string, raw json.RawMessage) (taskID int64, dryRun bool, ids map[int64]bool, err error) {
	var p struct {
		TaskID int64   `json:"taskId"`
		DryRun bool    `json:"dryRun"`
		IDs    []int64 `json:"ids"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return 0, false, nil, fmt.Errorf("%s: invalid params: %w", method, err)
	}
	if p.IDs != nil {
		ids = make(map[int64]bool, len(p.IDs))
		for _, id := range p.IDs {
			ids[id] = true
		}
	}
	return p.TaskID, p.DryRun, ids, nil
}

// queuedItemIDs returns the ids of every still-queued prompt across all
// chats of taskID.
func queuedItemIDs(db *store.Store, taskID int64) ([]int64, error) {
	chats, err := db.ListChatsByTask(taskID, true)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for _, c := range chats {
		items, err := db.ListChatQueue(c.ID)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			if it.Status == store.ChatQueueStatusQueued {
				ids = append(ids, it.ID)
			}
		}
	}
	return ids, nil
}

func isFinishedState(s tasktree.State) bool {
	return s == tasktree.StateDone || s == tasktree.StateError || s == tasktree.StateStopped
}

// handleTaskArchiveFinished is task.archiveFinished (PR-H11): archive the
// finished descendants of a task, deepest first, never the task itself and
// never anything running, idle or with a queued prompt. See
// docs/plans/active/task-hierarchy-tiers.md PR-H11.
func handleTaskArchiveFinished(wm *workspace.Manager, db *store.Store, reg *runs.Registry) handlerFunc {
	const method = "task.archiveFinished"
	return func(ctx context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		taskID, dryRun, ids, err := parseTreeActionParams(method, raw)
		if err != nil {
			return nil, err
		}
		nodes, err := loadActionTree(db, reg, method, taskID)
		if err != nil {
			return nil, err
		}

		// finished[id]: terminal state and no queued prompt.
		finished := make(map[int64]bool, len(nodes))
		for _, n := range nodes {
			if !isFinishedState(n.Run.State) {
				continue
			}
			_, queued, active, err := reg.ActiveRunForTask(n.ID)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", method, err)
			}
			finished[n.ID] = !active && !queued
		}
		// Children before parents: reverse DFS pre-order puts every
		// descendant ahead of its ancestor, so a prefix of it is closed
		// under "descendants of".
		children := make(map[int64][]int64, len(nodes))
		for _, n := range nodes {
			if n.ParentID != nil {
				children[*n.ParentID] = append(children[*n.ParentID], n.ID)
			}
		}
		eligible := make(map[int64]bool, len(nodes))
		res := archiveFinishedResult{
			DryRun: dryRun, Eligible: []treeItem{}, Archived: []treeItem{},
			Skipped: []treeSkipped{}, Failed: []treeFailed{},
		}
		var selection []tasktree.Node // post-order, root excluded
		for i := len(nodes) - 1; i >= 0; i-- {
			n := nodes[i]
			if n.ID == taskID || !finished[n.ID] {
				continue
			}
			ok := true
			for _, c := range children[n.ID] {
				if !eligible[c] {
					ok = false
					break
				}
			}
			if !ok {
				res.Skipped = append(res.Skipped, treeSkipped{ID: n.ID, Title: n.Title, Reason: "has unfinished descendants"})
				continue
			}
			eligible[n.ID] = true
			selection = append(selection, n)
		}
		if ids != nil {
			// The confirm is pinned to what the user saw in the dry run: a
			// task that finished while the dialog was open is not archived.
			pinned := selection[:0:0]
			for _, n := range selection {
				if ids[n.ID] {
					pinned = append(pinned, n)
				}
			}
			selection = pinned
		}
		if len(selection) > treeArchiveCap {
			res.Remaining = len(selection) - treeArchiveCap
			selection = selection[:treeArchiveCap]
		}
		for _, n := range selection {
			res.Eligible = append(res.Eligible, treeItem{ID: n.ID, Title: n.Title})
		}
		if dryRun {
			return res, nil
		}

		if treeAfterSelection != nil {
			treeAfterSelection()
		}

		// blockedBy[parent] = why a parent can no longer be archived.
		blockedBy := map[int64]string{}
		block := func(n tasktree.Node, reason string) {
			if n.ParentID != nil {
				blockedBy[*n.ParentID] = reason
			}
		}
		for _, n := range selection {
			if ctx.Err() != nil {
				// The caller went away: keep what was archived so far in the
				// result rather than hiding it behind an error.
				break
			}
			if reason, ok := blockedBy[n.ID]; ok {
				res.Skipped = append(res.Skipped, treeSkipped{ID: n.ID, Title: n.Title, Reason: reason})
				block(n, reason)
				continue
			}
			outcome, detail := archiveOne(wm, db, reg, n.ID)
			switch outcome {
			case archiveDone:
				res.Archived = append(res.Archived, treeItem{ID: n.ID, Title: n.Title})
			case archiveAlready:
				res.Skipped = append(res.Skipped, treeSkipped{ID: n.ID, Title: n.Title, Reason: "already archived"})
			case archiveRefused:
				res.Skipped = append(res.Skipped, treeSkipped{ID: n.ID, Title: n.Title, Reason: detail})
				block(n, "has unfinished descendants")
			case archiveFailed:
				res.Failed = append(res.Failed, treeFailed{ID: n.ID, Title: n.Title, Error: detail})
				block(n, "descendant failed to archive")
			}
		}
		return res, nil
	}
}

type archiveOutcome int

const (
	archiveDone archiveOutcome = iota
	archiveAlready
	archiveRefused
	archiveFailed
)

// archiveOne archives one task under the registry's archive latch,
// re-checking it under the latch: TryBeginArchive refuses a task that
// gained a run or queued prompt since selection, and a task another
// caller already archived is reported, not retried.
func archiveOne(wm *workspace.Manager, db *store.Store, reg *runs.Registry, id int64) (archiveOutcome, string) {
	release, err := reg.TryBeginArchive(id)
	if err != nil {
		return archiveRefused, err.Error()
	}
	defer release()
	t, err := db.GetTask(id)
	if err != nil {
		return archiveFailed, err.Error()
	}
	if t.ArchivedAt != nil {
		return archiveAlready, ""
	}
	if err := treeArchiveTask(wm, id); err != nil {
		return archiveFailed, err.Error()
	}
	return archiveDone, ""
}

// handleTaskStopTree is task.stopTree (PR-H11): cancel every queued prompt
// in the task's tree, then stop every running run (the task itself
// included), parents first, waiting for each under one shared deadline.
func handleTaskStopTree(db *store.Store, reg *runs.Registry) handlerFunc {
	const method = "task.stopTree"
	return func(ctx context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		taskID, dryRun, _, err := parseTreeActionParams(method, raw)
		if err != nil {
			return nil, err
		}
		res := stopTreeResult{
			DryRun: dryRun, Runs: []treeRun{}, Stopped: []treeRun{}, Failed: []treeRunFailed{},
		}

		// Detached from the request: a reload or dropped connection must not
		// abandon a half-done stop (the rescan passes are what catch runs a
		// live parent spawned meanwhile); the shared deadline still bounds it.
		deadlineCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), treeStopDeadline)
		defer cancel()
		seen := map[string]bool{}
		for pass := 0; pass < maxTreeStopPasses; pass++ {
			nodes, err := loadActionTree(db, reg, method, taskID)
			if err != nil {
				if pass > 0 {
					// Something changed under us (the root was archived, ...):
					// report what the earlier passes did.
					return res, nil
				}
				return nil, err
			}

			// (1) queued prompts first, so a freed per-tree slot is never
			// refilled by queue delivery.
			queued := 0
			for _, n := range nodes {
				ids, err := queuedItemIDs(db, n.ID)
				if err != nil {
					return nil, fmt.Errorf("%s: queue of task %d: %w", method, n.ID, err)
				}
				queued += len(ids)
				if dryRun {
					continue
				}
				for _, id := range ids {
					// A prompt delivered meanwhile is not queued any more
					// (and its run is caught by the next pass): not an error.
					if reg.CancelQueuedItem(id) == nil {
						res.QueuedCancelled++
					}
				}
			}

			// (2) running runs, parents before children (DFS pre-order).
			var targets []treeRun
			for _, n := range nodes {
				for _, runID := range n.Run.RunningRunIDs {
					if !seen[runID] {
						targets = append(targets, treeRun{ID: n.ID, Title: n.Title, RunID: runID})
					}
				}
			}
			if dryRun {
				res.QueuedPrompts = queued
				res.Runs = append(res.Runs, targets...)
				return res, nil
			}
			if pass == 0 && treeAfterSelection != nil {
				treeAfterSelection()
			}
			if len(targets) == 0 {
				break
			}
			// Cancel in order first so no slow parent delays a child's
			// cancel, then wait on all of them under the shared deadline.
			for _, tr := range targets {
				seen[tr.RunID] = true
				_ = reg.Stop(tr.RunID)
			}
			for _, tr := range targets {
				// Already terminal (finished on its own) is nil: not an error.
				if err := treeAwaitRun(deadlineCtx, reg, tr.RunID); err != nil {
					msg := err.Error()
					if errors.Is(err, context.DeadlineExceeded) {
						msg = fmt.Sprintf("still running %s after the stop request", treeStopDeadline)
					}
					res.Failed = append(res.Failed, treeRunFailed{ID: tr.ID, Title: tr.Title, RunID: tr.RunID, Error: msg})
					continue
				}
				res.Stopped = append(res.Stopped, tr)
			}
			if deadlineCtx.Err() != nil {
				break
			}
		}
		return res, nil
	}
}
