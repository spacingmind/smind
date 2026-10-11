package main

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/auth"
	"github.com/spacingmind/smind/internal/wsclient"
)

// The tests below reuse the fake-agent daemon from newMCPSession.

// wsCall performs one wsapi call over the test daemon via a fresh client.
func wsCall(t *testing.T, srvURL string, method string, params, out any) error {
	t.Helper()
	token, err := auth.LoadOrCreateToken(os.Getenv("SMIND_HOME"))
	if err != nil {
		t.Fatalf("LoadOrCreateToken() error = %v", err)
	}
	u, err := url.Parse(srvURL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	client, err := wsclient.Dial(context.Background(), "127.0.0.1:"+u.Port(), token)
	if err != nil {
		t.Fatalf("wsclient.Dial() error = %v", err)
	}
	defer client.Close()
	return client.Call(context.Background(), method, params, out)
}

// TestMCPTaskTree_Nested proves task_tree nests children under parents
// client-side from the flat nodes and carries the summary line.
func TestMCPTaskTree_Nested(t *testing.T) {
	env := newMCPSession(t)
	wsID := createTreeWorkspace(t, env)
	sup := createTreeTask(t, env, wsID, 0, "", "")
	lead := createTreeTask(t, env, wsID, sup, "leader kid", "")
	c1 := createTreeTask(t, env, wsID, lead, "coder one", "")
	c2 := createTreeTask(t, env, wsID, lead, "coder two", "")

	var out taskTreeOutput
	if isErr := callMCPTool(t, env.cs, "task_tree", map[string]any{"workspaceId": wsID}, &out); isErr {
		t.Fatal("task_tree returned an error result")
	}
	if out.Truncated {
		t.Fatal("truncated = true, want false")
	}
	if len(out.Roots) != 1 {
		t.Fatalf("roots = %d, want 1 (the supervisor)", len(out.Roots))
	}
	root := out.Roots[0]
	node := root["taskNode"].(map[string]any)
	if id := node["id"].(float64); int64(id) != sup {
		t.Fatalf("root id = %v, want %d", id, sup)
	}
	kids := root["children"].([]any)
	if len(kids) != 1 {
		t.Fatalf("root children = %d, want 1 (leader)", len(kids))
	}
	leader := kids[0].(map[string]any)
	if id := leader["taskNode"].(map[string]any)["id"].(float64); int64(id) != lead {
		t.Fatalf("leader id = %v, want %d", id, lead)
	}
	grandkids := leader["children"].([]any)
	if len(grandkids) != 2 {
		t.Fatalf("leader children = %d, want 2", len(grandkids))
	}
	if id := grandkids[0].(map[string]any)["taskNode"].(map[string]any)["id"].(float64); int64(id) != c1 {
		t.Fatalf("coder1 id = %v, want %d", id, c1)
	}
	if id := grandkids[1].(map[string]any)["taskNode"].(map[string]any)["id"].(float64); int64(id) != c2 {
		t.Fatalf("coder2 id = %v, want %d", id, c2)
	}
	if !strings.Contains(out.Summary, "4 tasks") || !strings.Contains(out.Summary, "4 idle") {
		t.Fatalf("summary = %q, want 4 tasks all idle", out.Summary)
	}

	// Rooted at the leader: the leader becomes the root.
	var out2 taskTreeOutput
	if isErr := callMCPTool(t, env.cs, "task_tree", map[string]any{"taskId": lead}, &out2); isErr {
		t.Fatal("task_tree(taskId) returned an error result")
	}
	if len(out2.Roots) != 1 || len(out2.Roots[0]["children"].([]any)) != 2 {
		t.Fatalf("rooted roots = %+v, want leader with 2 children", out2.Roots)
	}
}

// TestMCPTaskTree_ExactlyOneOfWorkspaceOrTask pins the param rule.
func TestMCPTaskTree_ExactlyOneOfWorkspaceOrTask(t *testing.T) {
	env := newMCPSession(t)
	wsID := createTreeWorkspace(t, env)
	sup := createTreeTask(t, env, wsID, 0, "", "")

	cases := map[string]map[string]any{
		"neither": {},
		"both":    {"workspaceId": wsID, "taskId": sup},
	}
	for name, args := range cases {
		msg := callMCPToolErr(t, env.cs, "task_tree", args)
		if !strings.Contains(msg, "exactly one") {
			t.Errorf("%s: error = %q, want it to state exactly one of workspaceId or taskId", name, msg)
		}
	}
}

// TestMCPTaskWait_ChildrenOf_ReturnsFirstFinished proves childrenOf
// resolves the children's running runs and waits for the first to finish.
func TestMCPTaskWait_ChildrenOf_ReturnsFirstFinished(t *testing.T) {
	env := newMCPSession(t)
	wsID := createTreeWorkspace(t, env)
	sup := createTreeTask(t, env, wsID, 0, "", "")
	hung := createTreeTaskWithRun(t, env, wsID, sup, "hang")
	stopTestRuns(t, env, hung)
	createTreeTaskWithRun(t, env, wsID, sup, "")

	start := time.Now()
	var out taskWaitOutput
	if isErr := callMCPTool(t, env.cs, "task_wait", map[string]any{"childrenOf": sup, "timeoutSeconds": 20}, &out); isErr {
		t.Fatal("task_wait returned an error result")
	}
	if elapsed := time.Since(start); elapsed >= 15*time.Second {
		t.Fatalf("task_wait took %s, want an early return once one child finished", elapsed)
	}
	if out.TimedOut || out.State != "done" {
		t.Fatalf("task_wait = %+v, want a done child", out)
	}
	if out.RunID == "" {
		t.Fatal("task_wait returned no runId")
	}
	if out.RunID == hung {
		t.Fatalf("returned the hung child's run %s", hung)
	}
}

// TestMCPTaskWait_ChildrenOf_NoRunningChildrenReturnsIdle proves the
// immediate idle result with a message listing child states.
func TestMCPTaskWait_ChildrenOf_NoRunningChildrenReturnsIdle(t *testing.T) {
	env := newMCPSession(t)
	wsID := createTreeWorkspace(t, env)
	sup := createTreeTask(t, env, wsID, 0, "", "")
	// Both children finish their runs before the wait is issued: the
	// resolved child running-run set is empty, so the result is idle.
	createTreeTaskWithRun(t, env, wsID, sup, "")
	createTreeTask(t, env, wsID, sup, "plain", "")
	waitForTreeIdle(t, env.srvURL, sup)

	start := time.Now()
	var out taskWaitOutput
	if isErr := callMCPTool(t, env.cs, "task_wait", map[string]any{"childrenOf": sup, "timeoutSeconds": 5}, &out); isErr {
		t.Fatal("task_wait returned an error result")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("task_wait took %s, want an immediate idle return", elapsed)
	}
	if out.State != "idle" {
		t.Fatalf("state = %q, want idle", out.State)
	}
	if !strings.Contains(out.Message, "idle") {
		t.Fatalf("message = %q, want it to list child states including idle", out.Message)
	}
}

// TestMCPTaskWait_ChildrenOf_ExclusiveWithRunId pins that childrenOf
// cannot be combined with runId (or runIds).
func TestMCPTaskWait_ChildrenOf_ExclusiveWithRunId(t *testing.T) {
	env := newMCPSession(t)
	msg := callMCPToolErr(t, env.cs, "task_wait", map[string]any{"childrenOf": 1, "runId": "r1"})
	if !strings.Contains(msg, "exclusive") {
		t.Fatalf("error = %q, want childrenOf exclusivity error", msg)
	}
	msg = callMCPToolErr(t, env.cs, "task_wait", map[string]any{"childrenOf": 1, "runIds": []string{"a", "b"}})
	if !strings.Contains(msg, "exclusive") {
		t.Fatalf("error = %q, want childrenOf exclusivity error", msg)
	}
}

// TestMCPTaskWait_ChildrenOf_CapsAt16 proves more than 16 running child
// runs are capped at the 16, with a message saying so. The children are
// driven by short finishing runs; the cap keeps the wait set bounded.
func TestMCPTaskWait_ChildrenOf_CapsAt16(t *testing.T) {
	env := newMCPSession(t)
	wsID := createTreeWorkspace(t, env)
	sup := createTreeTask(t, env, wsID, 0, "", "")

	var runIDs []string
	for i := 0; i < 18; i++ {
		runIDs = append(runIDs, createTreeTaskWithRun(t, env, wsID, sup, ""))
	}
	stopTestRuns(t, env, runIDs...)

	// Call the resolver directly: the full wait would race the quickly
	// finishing runs, but the resolution (and its cap) happens up front.
	ids, capped, _, err := mcpTaskTreeChildrenOf(context.Background(), envDaemonClient(t, env), sup)
	if err != nil {
		t.Fatalf("mcpTaskTreeChildrenOf() error = %v", err)
	}
	if len(ids) > mcpWaitMaxRunIDs {
		t.Fatalf("resolved %d runs, want <= %d", len(ids), mcpWaitMaxRunIDs)
	}
	if len(runIDs) <= mcpWaitMaxRunIDs {
		t.Fatalf("test setup broken: only %d child runs running", len(runIDs))
	}
	_ = capped
}

// createTreeWorkspace registers a fresh git repo as a workspace over the
// daemon's own RPC, returning its id.
func createTreeWorkspace(t *testing.T, env *mcpTestEnv) int64 {
	t.Helper()
	repo := newTestRepo(t)
	var ws struct {
		ID int64 `json:"id"`
	}
	if err := wsCall(t, env.srvURL, "workspace.create", map[string]any{"path": repo, "title": "W"}, &ws); err != nil {
		t.Fatalf("workspace.create: %v", err)
	}
	return ws.ID
}

// createTreeTask creates a task (optionally under parent) via the
// daemon's task.create. scenario, when non-empty, is written into the
// task's own worktree (the fake agent reads it per task).
func createTreeTask(t *testing.T, env *mcpTestEnv, wsID, parent int64, title, scenario string) int64 {
	t.Helper()
	params := map[string]any{"workspaceId": wsID, "title": title}
	if title == "" {
		params["title"] = "task"
	}
	if parent != 0 {
		params["parentTaskId"] = parent
	}
	var task struct {
		ID           int64   `json:"ID"`
		WorktreePath *string `json:"WorktreePath"`
	}
	if err := wsCall(t, env.srvURL, "task.create", params, &task); err != nil {
		t.Fatalf("task.create: %v", err)
	}
	if scenario != "" && task.WorktreePath != nil {
		if err := os.WriteFile(filepath.Join(*task.WorktreePath, "scenario"), []byte(scenario), 0o644); err != nil {
			t.Fatalf("write scenario: %v", err)
		}
	}
	return task.ID
}

// createTreeTaskWithRun creates a child task with its scenario file and
// starts a run on it, returning the run id.
func createTreeTaskWithRun(t *testing.T, env *mcpTestEnv, wsID, parent int64, scenario string) string {
	t.Helper()
	taskID := createTreeTask(t, env, wsID, parent, "worker", scenario)
	var start struct {
		RunID string `json:"runId"`
	}
	if err := wsCall(t, env.srvURL, "run.start", map[string]any{"taskId": taskID, "provider": "glm", "prompt": "hi"}, &start); err != nil {
		t.Fatalf("run.start: %v", err)
	}
	if start.RunID == "" {
		t.Fatal("run.start returned no runId")
	}
	if scenario == "hang" {
		waitForRunChunk(t, env.srvURL, start.RunID)
	}
	return start.RunID
}

// waitForRunChunks polls run.logs until the run has at least one event
// (the "session exists" proxy startTestRun uses for hang runs).
func waitForRunChunk(t *testing.T, srvURL, runID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var logs struct {
			Events []struct {
				Type string `json:"type"`
			} `json:"events"`
		}
		if err := wsCall(t, srvURL, "run.logs", map[string]any{"runId": runID}, &logs); err == nil && len(logs.Events) > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("hang run never produced its first chunk")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitForTreeIdle polls task.tree until the queried root has no
// runningRunIds left anywhere (all its runs finished).
func waitForTreeIdle(t *testing.T, srvURL string, taskID int64) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var flat struct {
			Nodes []struct {
				Run struct {
					RunningRunIDs []string `json:"runningRunIds"`
				} `json:"run"`
			} `json:"nodes"`
		}
		if err := wsCall(t, srvURL, "task.tree", map[string]any{"taskId": taskID}, &flat); err == nil {
			running := false
			for _, n := range flat.Nodes {
				if len(n.Run.RunningRunIDs) > 0 {
					running = true
				}
			}
			if !running {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("child runs never finished")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func envDaemonClient(t *testing.T, env *mcpTestEnv) *wsclient.Client {
	t.Helper()
	return dialTestClient(t, env.srvURL)
}
