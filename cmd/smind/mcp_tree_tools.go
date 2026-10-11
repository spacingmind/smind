package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spacingmind/smind/internal/wsclient"
)

// registerMCPTreeTools adds the whole-organization view of the
// task-hierarchy plan (§4.2/§4.3): task_tree (one call for every task
// with status/usage/permissions, nested) and task_wait's childrenOf mode.
func registerMCPTreeTools(srv *mcp.Server, client *wsclient.Client) {
	mcp.AddTool(srv, &mcp.Tool{Name: "task_tree", Description: "See a workspace's whole task hierarchy in one call: every task (or one task and its descendants) as nested nodes -- each carrying id, parentId, title, depth, tier (supervisor/leader/coder), run state, pending permission count, per-descendant status counts, usage rollups (tokens/cost summed over the subtree; null means nothing reported, never 0), and its children. Pass exactly one of workspaceId or taskId."}, mcpTaskTree(client))
}

// taskTreeInput is task_tree's argument shape: exactly one of
// workspaceId (whole tree) or taskId (that task and its descendants).
type taskTreeInput struct {
	WorkspaceID int64 `json:"workspaceId,omitempty" jsonschema:"id of the workspace whose whole task tree to return"`
	TaskID      int64 `json:"taskId,omitempty" jsonschema:"id of a task to return with its descendants (instead of workspaceId)"`
}

// taskTreeFlat mirrors the daemon's task.tree wire result.
type taskTreeFlat struct {
	Nodes     []taskTreeNode `json:"nodes"`
	Truncated bool           `json:"truncated"`
}

// taskTreeNode is one node of the task.tree wire shape (partial view:
// unknown fields are ignored; raw usage numbers ride through as JSON).
type taskTreeNode struct {
	ID          int64           `json:"id"`
	ParentID    *int64          `json:"parentId"`
	WorkspaceID int64           `json:"workspaceId"`
	Title       string          `json:"title"`
	Depth       int             `json:"depth"`
	TaskStatus  string          `json:"taskStatus"`
	Tier        *string         `json:"tier"`
	ChildCount  int             `json:"childCount"`
	Run         taskTreeNodeRun `json:"run"`
	Counts      taskTreeCounts  `json:"counts"`
	Usage       json.RawMessage `json:"usage"`
}

type taskTreeNodeRun struct {
	State              string   `json:"state"`
	RunningRunIDs      []string `json:"runningRunIds"`
	PendingPermissions int      `json:"pendingPermissions"`
}

type taskTreeCounts struct {
	Running           int64 `json:"running"`
	WaitingPermission int64 `json:"waitingPermission"`
	Failed            int64 `json:"failed"`
	Done              int64 `json:"done"`
	Idle              int64 `json:"idle"`
}

// taskTreeOutput is task_tree's structured output: the flat list nested
// client-side into roots (a node whose parentId is not among the returned
// nodes is a root), plus truncation and a one-line summary. Roots is a
// plain map tree rather than a recursive struct because the SDK's output
// schema inference rejects recursive types; each root is the TreeNode's
// own fields (id, parentId, title, depth, taskStatus, tier, childCount,
// run, counts, usage) with `children` beside them (spec §4.2).
type taskTreeOutput struct {
	Roots     []map[string]any `json:"roots"`
	Truncated bool             `json:"truncated"`
	Summary   string           `json:"summary"`
}

// mcpTaskTree wraps task.tree: one wsapi call, nesting computed locally.
func mcpTaskTree(client *wsclient.Client) mcp.ToolHandlerFor[taskTreeInput, taskTreeOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in taskTreeInput) (*mcp.CallToolResult, taskTreeOutput, error) {
		if (in.WorkspaceID == 0) == (in.TaskID == 0) {
			return nil, taskTreeOutput{}, fmt.Errorf("task_tree: pass exactly one of workspaceId or taskId")
		}
		params := map[string]any{}
		if in.TaskID != 0 {
			params["taskId"] = in.TaskID
		} else {
			params["workspaceId"] = in.WorkspaceID
		}
		var flat taskTreeFlat
		if err := callWS(ctx, client, "task_tree", "task.tree", params, &flat); err != nil {
			return nil, taskTreeOutput{}, err
		}
		return nil, nestTaskTree(flat), nil
	}
}

// nestTaskTree builds the nested view and summary from the flat pre-order
// node list. Nodes appear before their children in DFS pre-order, so a
// single pass appending each node to its parent's (already created)
// children slice yields the nesting; a node whose parent is not in the
// returned set (a root, or an orphan whose parent lives outside the set)
// starts a new root.
func nestTaskTree(flat taskTreeFlat) taskTreeOutput {
	byID := make(map[int64]map[string]any, len(flat.Nodes))
	var roots []map[string]any
	// Nodes arrive in DFS pre-order, so each node's parent map (if the
	// parent is in the returned set) already exists when the node does.
	for i := range flat.Nodes {
		n := flat.Nodes[i]
		nested := decodeNodeMap(n)
		nested["children"] = []map[string]any{}
		byID[n.ID] = nested
		if n.ParentID != nil {
			if p, ok := byID[*n.ParentID]; ok {
				p["children"] = append(p["children"].([]map[string]any), nested)
				continue
			}
		}
		roots = append(roots, nested)
	}
	if roots == nil {
		roots = []map[string]any{}
	}
	return taskTreeOutput{Roots: roots, Truncated: flat.Truncated, Summary: treeSummary(flat.Nodes)}
}

// decodeNodeMap renders one flat node's own fields into the generic map
// that rides through to MCP output; children are attached by the caller.
func decodeNodeMap(n taskTreeNode) map[string]any {
	m := map[string]any{
		"id": n.ID, "workspaceId": n.WorkspaceID, "title": n.Title,
		"depth": n.Depth, "taskStatus": n.TaskStatus, "childCount": n.ChildCount,
		"run": n.Run, "counts": n.Counts, "usage": n.Usage,
	}
	if n.ParentID != nil {
		m["parentId"] = *n.ParentID
	} else {
		m["parentId"] = nil
	}
	if n.Tier != nil {
		m["tier"] = *n.Tier
	} else {
		m["tier"] = nil
	}
	return m
}

// treeSummary renders the one-line model-facing summary, e.g.
// "3 tasks: 2 running, 1 waiting_permission".
func treeSummary(nodes []taskTreeNode) string {
	var states = map[string]int{
		"running": 0, "waiting_permission": 0, "done": 0, "error": 0, "stopped": 0, "idle": 0,
	}
	for _, n := range nodes {
		if _, ok := states[n.Run.State]; ok {
			states[n.Run.State]++
		}
	}
	order := []string{"running", "waiting_permission", "error", "done", "stopped", "idle"}
	var parts []string
	for _, s := range order {
		if states[s] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", states[s], s))
		}
	}
	if len(parts) == 0 {
		return fmt.Sprintf("%d tasks", len(nodes))
	}
	return fmt.Sprintf("%d tasks: %s", len(nodes), strings.Join(parts, ", "))
}

// mcpTaskTreeChildrenOf resolves the direct live children's running run
// ids of parentTaskID via task.tree {taskId}, oldest-started first,
// capped at 16 (the cap is reported in the returned message). It returns
// the task ids of the children too, for the idle message.
func mcpTaskTreeChildrenOf(ctx context.Context, client *wsclient.Client, parentTaskID int64) (runIDs []string, capped bool, childStates map[int64]string, err error) {
	var flat taskTreeFlat
	if err := callWS(ctx, client, "task_wait", "task.tree", map[string]any{"taskId": parentTaskID}, &flat); err != nil {
		return nil, false, nil, err
	}
	childStates = make(map[int64]string)
	// Direct children of the queried root are the depth-1 nodes.
	var all []string
	for _, n := range flat.Nodes {
		if n.Depth == 1 {
			childStates[n.ID] = n.Run.State
			all = append(all, n.Run.RunningRunIDs...)
		}
	}
	// The daemon already returns runningRunIds oldest-started first per
	// task; children are in id order, which preserves a stable order.
	runIDs = all
	if len(runIDs) > mcpWaitMaxRunIDs {
		runIDs = runIDs[:mcpWaitMaxRunIDs]
		capped = true
	}
	return runIDs, capped, childStates, nil
}

// taskWaitChildrenOf implements task_wait {childrenOf} (§4.3): wait for
// the first of the task's direct live children's running runs, or return
// idle immediately when none is running.
func taskWaitChildrenOf(ctx context.Context, client *wsclient.Client, in taskWaitInput) (taskWaitOutput, error) {
	deadline := time.Now().Add(mcpWaitDefaultTimeout)
	if in.TimeoutSeconds > 0 {
		deadline = time.Now().Add(time.Duration(in.TimeoutSeconds) * time.Second)
	}
	ignore := make(map[string]bool, len(in.IgnorePermissionIDs))
	for _, id := range in.IgnorePermissionIDs {
		ignore[id] = true
	}

	runIDs, capped, childStates, err := mcpTaskTreeChildrenOf(ctx, client, in.ChildrenOf)
	if err != nil {
		return taskWaitOutput{}, err
	}
	if len(runIDs) == 0 {
		var parts []string
		for id, state := range childStates {
			parts = append(parts, fmt.Sprintf("#%d %s", id, state))
		}
		sort.Strings(parts)
		msg := fmt.Sprintf("No running child runs of task %d: children are idle/finished.", in.ChildrenOf)
		if len(parts) > 0 {
			msg = fmt.Sprintf("No running child runs of task %d; child states: %s.", in.ChildrenOf, strings.Join(parts, ", "))
		}
		return taskWaitOutput{State: "idle", Message: msg}, nil
	}
	if capped {
		// Surface the cap in the result's message after the wait.
		out, err := mcpWaitAnyRun(ctx, client, runIDs, deadline, ignore)
		if err != nil {
			return taskWaitOutput{}, err
		}
		out.Message = fmt.Sprintf("More than %d child runs are running; waiting on the first %d. %s", mcpWaitMaxRunIDs, mcpWaitMaxRunIDs, out.Message)
		return out, nil
	}
	if len(runIDs) == 1 {
		return mcpWaitOneRun(ctx, client, runIDs[0], deadline, ignore)
	}
	return mcpWaitAnyRun(ctx, client, runIDs, deadline, ignore)
}
