package wsapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/tasktree"
)

// taskTreeResult is the result of task.tree: the flat DFS pre-order node
// list (tasktree.Node, the wire shape of docs/plans/active/
// task-hierarchy-tiers.md §4.1) plus truncation.
type taskTreeResult struct {
	Nodes     []tasktree.Node `json:"nodes"`
	Truncated bool            `json:"truncated"`
}

// handleTaskTree is the task.tree read model (plan §4.1): exactly one of
// {workspaceId} (every live root and orphan of the workspace, subtrees
// included) or {taskId} (that task and its live descendants; the task
// need not be be a root). One store pass (ListTasksByWorkspace, archived
// included -- orphans and usage rollups need the archived rows), one
// usage query (SumRunUsageByTask), and one reg.TaskRunFacts() call, then
// the pure tasktree.Build -- never a per-node query.
func handleTaskTree(db *store.Store, reg *runs.Registry, stalled *stalledSource) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			WorkspaceID int64 `json:"workspaceId"`
			TaskID      int64 `json:"taskId"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("task.tree: invalid params: %w", err)
		}
		if (p.WorkspaceID == 0) == (p.TaskID == 0) {
			return nil, fmt.Errorf("task.tree: pass exactly one of workspaceId or taskId")
		}

		root := tasktree.Root{WorkspaceID: p.WorkspaceID}
		if p.TaskID != 0 {
			// The workspace comes from the task itself, so a cross-workspace
			// id is not an issue; an unknown or archived id is a clear error
			// rather than an empty result.
			t, err := db.GetTask(p.TaskID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return nil, fmt.Errorf("task.tree: unknown task %d", p.TaskID)
				}
				return nil, fmt.Errorf("task.tree: get task %d: %w", p.TaskID, err)
			}
			if t.ArchivedAt != nil {
				return nil, fmt.Errorf("task.tree: task %d is archived", p.TaskID)
			}
			root = tasktree.Root{TaskID: p.TaskID}
			p.WorkspaceID = t.WorkspaceID
		}

		nodes, truncated, err := buildTaskTree(db, reg, p.WorkspaceID, root)
		if err != nil {
			return nil, fmt.Errorf("task.tree: %w", err)
		}
		if err := annotateTreeBudgets(db, nodes); err != nil {
			return nil, fmt.Errorf("task.tree: %w", err)
		}
		annotateStalled(nodes, stalled.current())
		return taskTreeResult{Nodes: nodes, Truncated: truncated}, nil
	}
}

// stalledSource lets the daemon hand task.tree the currently stalled seats
// (internal/stall) after the API is built. A nil source or unset func means
// no seat is ever marked.
type stalledSource struct {
	fn atomic.Pointer[func() map[int64]time.Time]
}

func (s *stalledSource) current() map[int64]time.Time {
	if s == nil {
		return nil
	}
	if f := s.fn.Load(); f != nil {
		return (*f)()
	}
	return nil
}

// SetStalledSource wires the stalled-tree detector's Current into task.tree:
// each returned task id (stalledSince as the value) gets stalled=true and
// stalledSince. nil clears it. It is not applied in buildTaskTree, which
// archiveFinished and stopTree share.
func (a *API) SetStalledSource(fn func() map[int64]time.Time) {
	if fn == nil {
		a.stalled.fn.Store(nil)
		return
	}
	a.stalled.fn.Store(&fn)
}

func annotateStalled(nodes []tasktree.Node, since map[int64]time.Time) {
	if len(since) == 0 {
		return
	}
	for i := range nodes {
		if t, ok := since[nodes[i].ID]; ok {
			nodes[i].Stalled = true
			nodes[i].StalledSince = t.UTC().Format(time.RFC3339)
		}
	}
}

// PublishTaskStalled emits the task.stalled bus event (the stall detector's
// alert); the push tap and connected clients both consume it.
func (a *API) PublishTaskStalled(taskID int64, title string, minutes int, since time.Time) {
	a.bus.Publish(Event{Topic: TopicTaskStalled, Payload: stalledPayload{
		TaskID: taskID, Title: title, Minutes: minutes, Since: since.UTC().Format(time.RFC3339),
	}})
}

// buildTaskTree is task.tree's read pass (one store pass, one usage
// query, one reg.TaskRunFacts call, then the pure tasktree.Build), shared
// with task.archiveFinished / task.stopTree so their selection is exactly
// what task.tree shows.
func buildTaskTree(db *store.Store, reg *runs.Registry, workspaceID int64, root tasktree.Root) ([]tasktree.Node, bool, error) {
	tasks, err := db.ListTasksByWorkspace(workspaceID)
	if err != nil {
		return nil, false, fmt.Errorf("list tasks: %w", err)
	}
	usage, err := db.SumRunUsageByTask(workspaceID)
	if err != nil {
		return nil, false, fmt.Errorf("sum usage: %w", err)
	}
	nodes, truncated := tasktree.Build(toTreeTasks(tasks), toTreeFacts(reg.TaskRunFacts()), toTreeUsage(usage), root)
	return nodes, truncated, nil
}

func toTreeTasks(tasks []store.Task) []tasktree.Task {
	out := make([]tasktree.Task, len(tasks))
	for i, t := range tasks {
		out[i] = tasktree.Task{
			ID: t.ID, ParentID: t.ParentTaskID, WorkspaceID: t.WorkspaceID,
			Title: t.Title, TaskStatus: t.Status, Archived: t.ArchivedAt != nil, BudgetUSD: t.BudgetUSD,
		}
	}
	return out
}

func toTreeFacts(facts map[int64]runs.TaskRunFact) map[int64]tasktree.RunFact {
	out := make(map[int64]tasktree.RunFact, len(facts))
	for id, f := range facts {
		rf := tasktree.RunFact{
			State: f.State, RunningRunIDs: f.RunningRunIDs, PendingPermissions: f.PendingPermissions, Disposition: f.Disposition,
			FailureKind: f.FailureKind, Window: f.FailureWindow, ResetInferred: f.FailureResetInferred,
		}
		if f.FailureResetAt != nil {
			rf.ResetAt = f.FailureResetAt.UTC().Format(time.RFC3339)
		}
		out[id] = rf
	}
	return out
}

func toTreeUsage(sums map[int64]store.TaskUsageSums) map[int64]tasktree.UsageTotals {
	out := make(map[int64]tasktree.UsageTotals, len(sums))
	for id, u := range sums {
		out[id] = tasktree.UsageTotals{
			Runs: u.Runs, InputTokens: u.InputTokens, OutputTokens: u.OutputTokens,
			CacheReadTokens: u.CacheReadTokens, CacheWriteTokens: u.CacheWriteTokens,
			ReasoningTokens: u.ReasoningTokens, CostUSD: u.CostUSD,
		}
	}
	return out
}
