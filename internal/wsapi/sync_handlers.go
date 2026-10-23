package wsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spacingmind/smind/internal/forge"
	"github.com/spacingmind/smind/internal/gitstate"
	"github.com/spacingmind/smind/internal/workspace"
)

// task.push and task.updateFromBase (ADR-0039 sections 3-6). Both are
// human-only: a run-scoped caller is refused, and no MCP tool exists for them.
// They share Create/Update PR's one-in-flight guard and re-check their
// gitstate action on a fresh snapshot, so the card and the RPC never disagree.
// The client never names a remote, ref or refspec.

// taskPushResult is the result of task.push.
type taskPushResult struct {
	Branch          string `json:"branch"`
	Remote          string `json:"remote"`
	UpstreamCreated bool   `json:"upstreamCreated"`
	Pushed          int    `json:"pushed"`
}

// taskUpdateFromBaseResult is the result of task.updateFromBase.
type taskUpdateFromBaseResult struct {
	Result  string `json:"result"`
	Commits int    `json:"commits"`
}

func (h *prHost) push(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
	if err := refuseRunScoped("task.push", rc); err != nil {
		return nil, err
	}
	var p struct {
		TaskID int64 `json:"taskId"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("task.push: invalid params: %w", err)
	}
	end, ok := h.begin(p.TaskID)
	if !ok {
		return nil, forge.NewPRError(forge.CodePRBusy, "")
	}
	defer end()
	// A push only publishes commits, so a running agent does not block it; the
	// latch still keeps a run from starting mid-push.
	release, err := h.latch(p.TaskID, false)
	if err != nil {
		return nil, err
	}
	defer release()
	// A push cut off half way by the client going away would leave the
	// branch state unknown; run it to its own deadline.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), prActionTimeout)
	defer cancel()

	g, err := h.svc.Fresh(p.TaskID)
	if err != nil {
		return nil, fmt.Errorf("task.push: task %d not found or archived", p.TaskID)
	}
	if a := g.Actions["push"]; !a.Enabled {
		return nil, blocked(a, g)
	}
	h.checked()
	res, err := h.wm.Push(ctx, p.TaskID)
	if err != nil {
		return nil, err
	}
	h.publishGit(p.TaskID)
	return taskPushResult{Branch: res.Branch, Remote: res.Remote, UpstreamCreated: res.UpstreamCreated, Pushed: res.Pushed}, nil
}

func (h *prHost) updateFromBase(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
	if err := refuseRunScoped("task.updateFromBase", rc); err != nil {
		return nil, err
	}
	var p struct {
		TaskID   int64  `json:"taskId"`
		Strategy string `json:"strategy"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("task.updateFromBase: invalid params: %w", err)
	}
	strategy := strings.TrimSpace(p.Strategy)
	if strategy != "" && strategy != workspace.SyncStrategyMerge && strategy != workspace.SyncStrategyRebase {
		return nil, forge.NewPRError(forge.CodeInvalidStrategy, "")
	}
	end, ok := h.begin(p.TaskID)
	if !ok {
		return nil, forge.NewPRError(forge.CodePRBusy, "")
	}
	defer end()
	// Update from base rewrites the worktree: no run may be active, or start,
	// until it is done.
	release, err := h.latch(p.TaskID, true)
	if err != nil {
		return nil, err
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), prActionTimeout)
	defer cancel()

	g, err := h.svc.Fresh(p.TaskID)
	if err != nil {
		return nil, fmt.Errorf("task.updateFromBase: task %d not found or archived", p.TaskID)
	}
	// up_to_date is advisory: the local base ref may be stale, so the RPC
	// fetches and answers instead of refusing. merge_in_progress is left to
	// the Manager, whose error also names the operation.
	if a := g.Actions["updateFromBase"]; !a.Enabled && a.Code != gitstate.CodeUpToDate && a.Code != forge.CodeMergeInProgress {
		return nil, blocked(a, g)
	}
	// The worktree changes whatever the outcome (a conflict leaves it
	// mid-merge), so task.git is recomputed on every exit past this point.
	defer h.publishGit(p.TaskID)
	h.checked()
	res, err := h.wm.UpdateFromBase(ctx, p.TaskID, strategy)
	if err != nil {
		return nil, err
	}
	return taskUpdateFromBaseResult{Result: res.Result, Commits: res.Commits}, nil
}

// publishGit recomputes the task's snapshot, which publishes task.git when it
// changed (the same path Update PR takes after its push).
func (h *prHost) publishGit(taskID int64) { _, _ = h.svc.Fresh(taskID) }
