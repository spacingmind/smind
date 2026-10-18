package wsapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
)

// treeResult is a partial decode of task.tree's result for assertions.
type treeResult struct {
	Nodes     []treeNode `json:"nodes"`
	Truncated bool       `json:"truncated"`
}

// treeNode is a partial decode of one task.tree node (the same
// partial-view convention the rest of this package's tests use).
type treeNode struct {
	ID          int64          `json:"id"`
	ParentID    *int64         `json:"parentId"`
	WorkspaceID int64          `json:"workspaceId"`
	Title       string         `json:"title"`
	Depth       int            `json:"depth"`
	TaskStatus  string         `json:"taskStatus"`
	Tier        *string        `json:"tier"`
	ChildCount  int            `json:"childCount"`
	Run         treeNodeRun    `json:"run"`
	Counts      tasktreeCounts `json:"counts"`
	Usage       treeNodeUsage  `json:"usage"`
}

type treeNodeRun struct {
	State              string   `json:"state"`
	RunningRunIDs      []string `json:"runningRunIds"`
	PendingPermissions int      `json:"pendingPermissions"`
}

type tasktreeCounts struct {
	Running           int64 `json:"running"`
	WaitingPermission int64 `json:"waitingPermission"`
	Failed            int64 `json:"failed"`
	Done              int64 `json:"done"`
	Idle              int64 `json:"idle"`
}

type treeNodeUsage struct {
	Own  treeNodeUsageTotals `json:"own"`
	Subt treeNodeUsageTotals `json:"subtree"`
	Arch int64               `json:"archivedDescendants"`
}

type treeNodeUsageTotals struct {
	Runs         int64    `json:"runs"`
	InputTokens  *int64   `json:"inputTokens"`
	OutputTokens *int64   `json:"outputTokens"`
	CostUSD      *float64 `json:"costUsd"`
}

func callTaskTree(t *testing.T, ws *websocket.Conn, id string, params map[string]any) (treeResult, *rpcError) {
	t.Helper()
	sendRequest(t, ws, id, "task.tree", params)
	resp := readEnvelopeFor(t, ws, id, 10*time.Second)
	if resp.Error != nil {
		return treeResult{}, resp.Error
	}
	var result treeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode task.tree result: %v", err)
	}
	return result, nil
}

// treeSetup builds a server + workspace + supervisor/leader/coder
// hierarchy via the daemon's own RPCs and returns the connection and the
// created ids.
func treeSetup(t *testing.T) (*websocket.Conn, workspace1, []int64) {
	t.Helper()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")
	repo := newTestRepo(t)
	sendRequest(t, ws, "ws", "workspace.create", map[string]any{"path": repo, "title": "W", "routingPolicy": "hard"})
	resp := readEnvelopeFor(t, ws, "ws", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("workspace.create error = %v", resp.Error.Message)
	}
	var w struct {
		ID int64 `json:"ID"`
	}
	if err := json.Unmarshal(resp.Result, &w); err != nil {
		t.Fatalf("decode workspace: %v", err)
	}

	var ids []int64
	create := func(id string, params map[string]any) int64 {
		sendRequest(t, ws, id, "task.create", params)
		resp := readEnvelopeFor(t, ws, id, 5*time.Second)
		if resp.Error != nil {
			t.Fatalf("task.create error = %v", resp.Error.Message)
		}
		var task struct {
			ID int64 `json:"ID"`
		}
		if err := json.Unmarshal(resp.Result, &task); err != nil {
			t.Fatalf("decode task: %v", err)
		}
		ids = append(ids, task.ID)
		return task.ID
	}
	sup := create("t1", map[string]any{"workspaceId": w.ID, "title": "sup"})
	lead := create("t2", map[string]any{"workspaceId": w.ID, "title": "lead", "parentTaskId": sup})
	create("t3", map[string]any{"workspaceId": w.ID, "title": "coder", "parentTaskId": lead})
	return ws, workspace1{ID: w.ID, Sup: sup, Lead: lead}, ids
}

type workspace1 struct {
	ID   int64
	Sup  int64
	Lead int64
}

// TestTaskTree_Workspace: the whole three-tier tree in DFS pre-order with
// tiers, depths and child counts, plus the flat {nodes, truncated} shape.
func TestTaskTree_Workspace(t *testing.T) {
	t.Parallel()
	ws, w, ids := treeSetup(t)

	result, errObj := callTaskTree(t, ws, "tree", map[string]any{"workspaceId": w.ID})
	if errObj != nil {
		t.Fatalf("task.tree error = %v", errObj.Message)
	}
	if result.Truncated {
		t.Fatal("truncated = true, want false")
	}
	if len(result.Nodes) != 3 {
		t.Fatalf("nodes = %+v, want 3", result.Nodes)
	}
	sup, lead, coder := result.Nodes[0], result.Nodes[1], result.Nodes[2]
	if sup.ID != ids[0] || lead.ID != ids[1] || coder.ID != ids[2] {
		t.Fatalf("order = %d,%d,%d want %v", sup.ID, lead.ID, coder.ID, ids)
	}
	if sup.Depth != 0 || lead.Depth != 1 || coder.Depth != 2 {
		t.Fatalf("depths = %d,%d,%d", sup.Depth, lead.Depth, coder.Depth)
	}
	if sup.Tier == nil || *sup.Tier != "supervisor" || lead.Tier == nil || *lead.Tier != "leader" || coder.Tier == nil || *coder.Tier != "coder" {
		t.Fatalf("tiers = %v,%v,%v", sup.Tier, lead.Tier, coder.Tier)
	}
	if sup.ChildCount != 1 || lead.ChildCount != 1 || coder.ChildCount != 0 {
		t.Fatalf("childCounts = %d,%d,%d", sup.ChildCount, lead.ChildCount, coder.ChildCount)
	}
	if lead.ParentID == nil || *lead.ParentID != sup.ID {
		t.Fatalf("lead.ParentID = %v", lead.ParentID)
	}
	if coder.Run.State != "idle" {
		t.Fatalf("coder state = %q, want idle (no runs)", coder.Run.State)
	}
	if coder.Usage.Own.Runs != 0 {
		t.Fatalf("coder usage.runs = %d, want 0 (never reported anything)", coder.Usage.Own.Runs)
	}
	if coder.Usage.Own.InputTokens != nil {
		t.Fatalf("coder usage.inputTokens = %v, want null", coder.Usage.Own.InputTokens)
	}
}

// TestTaskTree_RootedAtTask: {taskId} returns that task and its live
// descendants only, depth restarting at 0.
func TestTaskTree_RootedAtTask(t *testing.T) {
	t.Parallel()
	ws, w, ids := treeSetup(t)

	result, errObj := callTaskTree(t, ws, "tree", map[string]any{"taskId": w.Lead})
	if errObj != nil {
		t.Fatalf("task.tree error = %v", errObj.Message)
	}
	if len(result.Nodes) != 2 || result.Nodes[0].ID != w.Lead || result.Nodes[1].ID != ids[2] {
		t.Fatalf("nodes = %+v, want [lead coder]", result.Nodes)
	}
	if result.Nodes[0].Depth != 0 || result.Nodes[1].Depth != 1 {
		t.Fatalf("depths = %d,%d", result.Nodes[0].Depth, result.Nodes[1].Depth)
	}
}

// TestTaskTree_ParamsExactlyOne: neither or both of workspaceId/taskId is
// a clear error.
func TestTaskTree_ParamsExactlyOne(t *testing.T) {
	t.Parallel()
	ws, w, _ := treeSetup(t)
	if _, errObj := callTaskTree(t, ws, "none", map[string]any{}); errObj == nil {
		t.Fatal("no params: error = nil, want an error")
	}
	if _, errObj := callTaskTree(t, ws, "both", map[string]any{"workspaceId": w.ID, "taskId": w.Sup}); errObj == nil {
		t.Fatal("both params: error = nil, want an error")
	}
}

// TestTaskTree_WaitingPermissionState: a task whose run has a pending
// permission reports waiting_permission (via reg.TaskRunFacts), for the
// node itself and its ancestors' counts.
func TestTaskTree_WaitingPermissionState(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")
	sup := newTestTask(t, wm, "")
	child, err := wm.CreateTask(sup.WorkspaceID, nil, "coder", workspace.WithParentTask(sup.ID))
	if err != nil {
		t.Fatalf("CreateTask(child) error = %v", err)
	}
	// The child's worktree needs its own scenario file (per-worktree).
	if child.WorktreePath != nil {
		if err := os.WriteFile(filepath.Join(*child.WorktreePath, "scenario"), []byte("permission"), 0o644); err != nil {
			t.Fatalf("write scenario: %v", err)
		}
	}

	sendRequest(t, ws, "start", "run.start", map[string]any{
		"taskId": child.ID, "provider": "glm", "prompt": "hi",
	})
	deadline := time.Now().Add(10 * time.Second)
	var result treeResult
	for {
		var errObj *rpcError
		result, errObj = callTaskTree(t, ws, "tree", map[string]any{"taskId": sup.ID})
		if errObj != nil {
			t.Fatalf("task.tree error = %v", errObj.Message)
		}
		if len(result.Nodes) == 2 && result.Nodes[1].Run.State == "waiting_permission" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("child never reached waiting_permission: %+v", result.Nodes)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if result.Nodes[1].Run.PendingPermissions < 1 {
		t.Fatalf("pendingPermissions = %d, want >= 1", result.Nodes[1].Run.PendingPermissions)
	}
	if result.Nodes[0].Counts.WaitingPermission != 1 {
		t.Fatalf("sup counts = %+v, want waitingPermission 1", result.Nodes[0].Counts)
	}
}

// TestTaskTree_UnknownTaskIsError: an unknown taskId is a clear error,
// never an empty success.
func TestTaskTree_UnknownTaskIsError(t *testing.T) {
	t.Parallel()
	ws, w, _ := treeSetup(t)
	_ = w
	if _, errObj := callTaskTree(t, ws, "unknown", map[string]any{"taskId": 999999}); errObj == nil {
		t.Fatal("unknown taskId: error = nil, want an error")
	}
}

// TestTaskTree_WideTree200ChildrenIsOneStorePass: 200 children under one
// root (created directly in the store, no worktrees) return in one call;
// with no per-node query the whole RPC stays well under the timeout.
func TestTaskTree_WideTree200ChildrenIsOneStorePass(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")
	sup := newTestTask(t, wm, "")

	for i := 0; i < 200; i++ {
		parent := sup.ID
		if _, err := db.CreateTask(store.Task{WorkspaceID: sup.WorkspaceID, Title: "coder", Status: "created", ParentTaskID: &parent}); err != nil {
			t.Fatalf("CreateTask(child %d) error = %v", i, err)
		}
	}

	start := time.Now()
	result, errObj := callTaskTree(t, ws, "tree", map[string]any{"workspaceId": sup.WorkspaceID})
	if errObj != nil {
		t.Fatalf("task.tree error = %v", errObj.Message)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("task.tree took %s on 201 nodes, want < 2s (one store pass)", elapsed)
	}
	if result.Truncated || len(result.Nodes) != 201 {
		t.Fatalf("nodes = %d truncated = %v, want 201 false", len(result.Nodes), result.Truncated)
	}
	if result.Nodes[0].ChildCount != 200 {
		t.Fatalf("childCount = %d, want 200", result.Nodes[0].ChildCount)
	}
}

// TestTaskTree_ArchivedTaskIsError: {taskId} of an archived task is a
// clear error, not an empty success.
func TestTaskTree_ArchivedTaskIsError(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")
	task := newTestTask(t, wm, "")
	if _, err := wm.ArchiveTask(task.ID); err != nil {
		t.Fatalf("ArchiveTask() error = %v", err)
	}
	if _, errObj := callTaskTree(t, ws, "arch", map[string]any{"taskId": task.ID}); errObj == nil {
		t.Fatal("archived taskId: error = nil, want an error")
	}
}

// TestTaskTree_CrossWorkspaceTaskResolvesItsOwnWorkspace: a taskId from
// another workspace is not an error -- the handler takes the workspace
// FROM the task, returning that task's own subtree and never mixing
// workspaces.
func TestTaskTree_CrossWorkspaceTaskResolvesItsOwnWorkspace(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")
	a := newTestTask(t, wm, "")
	b := newTestTask(t, wm, "")
	if a.WorkspaceID == b.WorkspaceID {
		t.Fatalf("newTestTask returned the same workspace (%d) for two tasks", a.WorkspaceID)
	}

	mkChild := func(parent store.Task, title string) store.Task {
		p := parent.ID
		child, err := db.CreateTask(store.Task{WorkspaceID: parent.WorkspaceID, Title: title, Status: "created", ParentTaskID: &p})
		if err != nil {
			t.Fatalf("CreateTask(%q) error = %v", title, err)
		}
		return child
	}
	childA := mkChild(a, "child of a")
	childB := mkChild(b, "child of b")

	for _, tc := range []struct {
		taskID       int64
		wantParent   store.Task
		wantChild    store.Task
		notParent    store.Task
		notOtherNode int64
	}{
		{taskID: b.ID, wantParent: b, wantChild: childB, notParent: a, notOtherNode: childA.ID},
		{taskID: a.ID, wantParent: a, wantChild: childA, notParent: b, notOtherNode: childB.ID},
	} {
		result, errObj := callTaskTree(t, ws, "tree", map[string]any{"taskId": tc.taskID})
		if errObj != nil {
			t.Fatalf("task.tree(taskId %d) error = %v, want nil", tc.taskID, errObj.Message)
		}
		if len(result.Nodes) != 2 {
			t.Fatalf("task.tree(taskId %d) nodes = %+v, want exactly 2", tc.taskID, result.Nodes)
		}
		for _, n := range result.Nodes {
			if n.WorkspaceID != tc.wantParent.WorkspaceID {
				t.Fatalf("node %d workspaceId = %d, want %d (the task's own workspace)", n.ID, n.WorkspaceID, tc.wantParent.WorkspaceID)
			}
			if n.ID == tc.notParent.ID || n.ID == tc.notOtherNode {
				t.Fatalf("task.tree(taskId %d) returned foreign node %d (workspace %d)", tc.taskID, n.ID, tc.notParent.WorkspaceID)
			}
		}
		if result.Nodes[0].ID != tc.wantParent.ID || result.Nodes[0].Depth != 0 {
			t.Fatalf("first node = {id %d, depth %d}, want {id %d, depth 0}", result.Nodes[0].ID, result.Nodes[0].Depth, tc.wantParent.ID)
		}
		if result.Nodes[1].ID != tc.wantChild.ID || result.Nodes[1].Depth != 1 {
			t.Fatalf("second node = {id %d, depth %d}, want {id %d, depth 1}", result.Nodes[1].ID, result.Nodes[1].Depth, tc.wantChild.ID)
		}
	}
}
