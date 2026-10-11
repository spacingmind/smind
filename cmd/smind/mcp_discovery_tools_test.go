package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The PR-A discovery tools, each driven through the full MCP round trip
// against the fake-agent-backed test daemon (see mcp_test.go's harness
// doc comment). Every test asserts real returned data, not just "no
// error".

// TestMCPTools_WorkspaceList proves workspace_list returns the workspace
// the harness registered, with the id/title/path fields an orchestrator
// needs before task_new.
func TestMCPTools_WorkspaceList(t *testing.T) {
	env := newMCPSession(t)

	wsID := createTestWorkspace(t, dialTestClient(t, env.srvURL))

	var out struct {
		Workspaces []struct {
			ID    int64  `json:"ID"`
			Title string `json:"Title"`
			Path  string `json:"Path"`
		} `json:"workspaces"`
	}
	if isErr := callMCPTool(t, env.cs, "workspace_list", map[string]any{}, &out); isErr {
		t.Fatal("workspace_list returned an error result")
	}
	if len(out.Workspaces) != 1 {
		t.Fatalf("workspace_list = %+v, want exactly the one registered workspace", out.Workspaces)
	}
	ws := out.Workspaces[0]
	if ws.ID != wsID || ws.Title != "W" || ws.Path == "" {
		t.Fatalf("workspace_list entry = %+v, want id %d, title W, non-empty path", ws, wsID)
	}
}

// TestMCPTools_WorkspaceListEmpty pins the empty-not-null contract: a
// daemon with no workspaces returns {"workspaces": []}, never null.
func TestMCPTools_WorkspaceListEmpty(t *testing.T) {
	env := newMCPSession(t)

	var out struct {
		Workspaces []map[string]any `json:"workspaces"`
	}
	if isErr := callMCPTool(t, env.cs, "workspace_list", map[string]any{}, &out); isErr {
		t.Fatal("workspace_list returned an error result")
	}
	if out.Workspaces == nil {
		t.Fatal("workspace_list workspaces = null, want an empty list")
	}
	if len(out.Workspaces) != 0 {
		t.Fatalf("workspace_list = %+v, want empty", out.Workspaces)
	}
}

// TestMCPTools_TaskGet proves task_get returns one task's real fields,
// including the worktreePath the review step of the orchestration loop
// reads.
func TestMCPTools_TaskGet(t *testing.T) {
	env := newMCPSession(t)

	var created struct {
		Task struct {
			ID           int64   `json:"ID"`
			Title        string  `json:"Title"`
			WorktreePath *string `json:"WorktreePath"`
		} `json:"task"`
	}
	if isErr := callMCPTool(t, env.cs, "task_new", map[string]any{
		"workspaceId": createTestWorkspace(t, dialTestClient(t, env.srvURL)), "title": "Discovery",
	}, &created); isErr {
		t.Fatal("task_new returned an error result")
	}

	var out struct {
		Task struct {
			ID           int64   `json:"ID"`
			Title        string  `json:"Title"`
			Status       string  `json:"Status"`
			WorktreePath *string `json:"WorktreePath"`
		} `json:"task"`
	}
	if isErr := callMCPTool(t, env.cs, "task_get", map[string]any{"id": created.Task.ID}, &out); isErr {
		t.Fatal("task_get returned an error result")
	}
	if out.Task.ID != created.Task.ID || out.Task.Title != "Discovery" {
		t.Fatalf("task_get = %+v, want task %d %q", out.Task, created.Task.ID, "Discovery")
	}
	if out.Task.Status == "" {
		t.Fatal("task_get status = \"\", want a real status")
	}
	if out.Task.WorktreePath == nil || *out.Task.WorktreePath == "" {
		t.Fatal("task_get worktreePath = empty, want the task's worktree path")
	}
}

// TestMCPTools_TaskGetNotFound proves an unknown id surfaces as a clear
// tool error (the daemon's rejection via callWS), not an empty success.
func TestMCPTools_TaskGetNotFound(t *testing.T) {
	env := newMCPSession(t)

	res, err := env.cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "task_get",
		Arguments: map[string]any{"id": 99999},
	})
	if err != nil {
		t.Fatalf("CallTool error = %v, want a result", err)
	}
	if !res.IsError {
		t.Fatal("task_get on an unknown id: IsError = false, want a tool error")
	}
	msg := mcpToolErrorText(res)
	if !strings.Contains(msg, "task_get:") {
		t.Fatalf("error text = %q, want the task_get: prefix", msg)
	}
	if !strings.Contains(msg, "99999") {
		t.Fatalf("error text = %q, want it to reference the unknown id", msg)
	}
}

// TestMCPTools_RunListByTask proves run_list {taskId} returns that
// task's runs (including their ids -- the lost-run-id recovery path) and
// only that task's, across all of the task's chats.
func TestMCPTools_RunListByTask(t *testing.T) {
	env := newMCPSession(t)
	client := dialTestClient(t, env.srvURL)

	var task struct {
		ID int64 `json:"id"`
	}
	if err := client.Call(context.Background(), "task.create", map[string]any{
		"workspaceId": createTestWorkspace(t, client), "title": "T",
	}, &task); err != nil {
		t.Fatalf("task.create: %v", err)
	}
	var runIDs []string
	for range 2 {
		var start struct {
			RunID string `json:"runId"`
		}
		if err := client.Call(context.Background(), "run.start", map[string]any{
			"taskId": task.ID, "provider": "glm", "prompt": "hi",
		}, &start); err != nil {
			t.Fatalf("run.start: %v", err)
		}
		waitForRunStatus(t, client, start.RunID, "done")
		runIDs = append(runIDs, start.RunID)
	}
	// A run on a different task, so the filter has something to exclude.
	otherRun := startTestRun(t, env.srvURL, "glm")
	waitForRunStatus(t, client, otherRun, "done")

	var out struct {
		Runs []struct {
			ID     string `json:"ID"`
			TaskID int64  `json:"TaskID"`
			Status string `json:"Status"`
		} `json:"runs"`
	}
	if isErr := callMCPTool(t, env.cs, "run_list", map[string]any{"taskId": task.ID}, &out); isErr {
		t.Fatal("run_list returned an error result")
	}
	if len(out.Runs) != 2 {
		t.Fatalf("run_list(taskId) = %+v, want the task's 2 runs", out.Runs)
	}
	got := map[string]bool{}
	for _, r := range out.Runs {
		if r.TaskID != task.ID {
			t.Fatalf("run_list(taskId=%d) returned run of task %d", task.ID, r.TaskID)
		}
		if r.Status != "done" {
			t.Fatalf("run_list status = %q, want done", r.Status)
		}
		got[r.ID] = true
	}
	for _, id := range runIDs {
		if !got[id] {
			t.Fatalf("run_list(taskId=%d) missing run %s", task.ID, id)
		}
	}

	// An unknown task id is an empty list, not an error.
	empty := struct {
		Runs []map[string]any `json:"runs"`
	}{}
	if isErr := callMCPTool(t, env.cs, "run_list", map[string]any{"taskId": 99999}, &empty); isErr {
		t.Fatal("run_list on unknown taskId returned an error result")
	}
	if empty.Runs == nil || len(empty.Runs) != 0 {
		t.Fatalf("run_list(taskId=unknown) = %+v, want an empty (not null) list", empty.Runs)
	}
}

// TestMCPTools_RunListStatusFilter proves run_list {status: "running"}
// returns only live runs: a hanging fake-agent run shows up, and the
// finished ones don't.
func TestMCPTools_RunListStatusFilter(t *testing.T) {
	env := newMCPSession(t)
	client := dialTestClient(t, env.srvURL)

	finished := startTestRun(t, env.srvURL, "glm")
	waitForRunStatus(t, client, finished, "done")
	hanging := startTestRun(t, env.srvURL, "glm", "hang")
	t.Cleanup(func() {
		_ = client.Call(context.Background(), "run.stop", map[string]any{"runId": hanging}, nil)
	})

	// Wait until the hanging run is genuinely StatusRunning in run.list.
	deadline := time.Now().Add(10 * time.Second)
	for {
		var running struct {
			Runs []struct {
				ID string `json:"ID"`
			} `json:"runs"`
		}
		if isErr := callMCPTool(t, env.cs, "run_list", map[string]any{"status": "running"}, &running); isErr {
			t.Fatal("run_list returned an error result")
		}
		if len(running.Runs) == 1 && running.Runs[0].ID == hanging {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run_list(status=running) never showed exactly the hanging run %s", hanging)
		}
		time.Sleep(50 * time.Millisecond)
	}

	var all struct {
		Runs []struct {
			ID string `json:"ID"`
		} `json:"runs"`
	}
	if isErr := callMCPTool(t, env.cs, "run_list", map[string]any{}, &all); isErr {
		t.Fatal("run_list returned an error result")
	}
	if len(all.Runs) < 2 {
		t.Fatalf("run_list() = %+v, want both runs unfiltered", all.Runs)
	}
}

// TestMCPTools_ProviderListMarksAutoApprovingModes proves provider_list
// carries each provider's modes with the autoApproves flag (AC4): the
// claude-native bypassPermissions mode must be marked auto-approving and
// acceptEdits not -- the distinction an orchestrator needs so it can
// avoid picking an auto-approving mode itself.
func TestMCPTools_ProviderListMarksAutoApprovingModes(t *testing.T) {
	env := newMCPSession(t)

	var out struct {
		Providers []struct {
			ID          string `json:"id"`
			DefaultMode string `json:"defaultMode"`
			Modes       []struct {
				ID           string `json:"id"`
				AutoApproves bool   `json:"autoApproves"`
			} `json:"modes"`
		} `json:"providers"`
	}
	if isErr := callMCPTool(t, env.cs, "provider_list", map[string]any{}, &out); isErr {
		t.Fatal("provider_list returned an error result")
	}
	if len(out.Providers) == 0 {
		t.Fatal("provider_list = [], want the daemon's providers")
	}

	var claude *struct {
		ID          string `json:"id"`
		DefaultMode string `json:"defaultMode"`
		Modes       []struct {
			ID           string `json:"id"`
			AutoApproves bool   `json:"autoApproves"`
		} `json:"modes"`
	}
	for i := range out.Providers {
		if out.Providers[i].ID == "claude-native" {
			claude = &out.Providers[i]
		}
	}
	if claude == nil {
		t.Fatalf("provider_list = %+v, want a claude-native entry", out.Providers)
	}
	if claude.DefaultMode == "" {
		t.Fatal("claude-native defaultMode = \"\", want the provider's default mode id")
	}
	var bypass, acceptEdits *struct {
		ID           string `json:"id"`
		AutoApproves bool   `json:"autoApproves"`
	}
	for i := range claude.Modes {
		switch claude.Modes[i].ID {
		case "bypassPermissions":
			bypass = &claude.Modes[i]
		case "acceptEdits":
			acceptEdits = &claude.Modes[i]
		}
	}
	if bypass == nil {
		t.Fatalf("claude-native modes = %+v, want bypassPermissions", claude.Modes)
	}
	if !bypass.AutoApproves {
		t.Fatal("bypassPermissions autoApproves = false, want true")
	}
	if acceptEdits == nil {
		t.Fatalf("claude-native modes = %+v, want acceptEdits", claude.Modes)
	}
	if acceptEdits.AutoApproves {
		t.Fatal("acceptEdits autoApproves = true, want false")
	}
}

// TestMCPTools_ProfileListIncludesNotes proves profile_list returns the
// human-authored profiles with every field the orchestrator needs to
// pick one, notes included (AC5).
func TestMCPTools_ProfileListIncludesNotes(t *testing.T) {
	env := newMCPSession(t)
	client := dialTestClient(t, env.srvURL)

	var created struct {
		ID int64 `json:"ID"`
	}
	if err := client.Call(context.Background(), "profile.create", map[string]any{
		"name": "Careful", "provider": "glm", "permissionMode": "default",
		"thinkingLevel": "extended", "notes": "for risky refactors",
	}, &created); err != nil {
		t.Fatalf("profile.create: %v", err)
	}

	var out struct {
		Profiles []struct {
			ID             int64  `json:"ID"`
			Name           string `json:"Name"`
			Provider       string `json:"Provider"`
			PermissionMode string `json:"PermissionMode"`
			AutoAccept     bool   `json:"AutoAccept"`
			ThinkingLevel  string `json:"ThinkingLevel"`
			Notes          string `json:"Notes"`
		} `json:"profiles"`
	}
	if isErr := callMCPTool(t, env.cs, "profile_list", map[string]any{}, &out); isErr {
		t.Fatal("profile_list returned an error result")
	}
	if len(out.Profiles) != 1 {
		t.Fatalf("profile_list = %+v, want the one created profile", out.Profiles)
	}
	p := out.Profiles[0]
	if p.ID != created.ID || p.Name != "Careful" || p.Provider != "glm" || p.PermissionMode != "default" {
		t.Fatalf("profile_list entry = %+v, want id %d / Careful / glm / default", p, created.ID)
	}
	if p.AutoAccept {
		t.Fatal("profile autoAccept = true, want false as created")
	}
	if p.ThinkingLevel != "extended" {
		t.Fatalf("profile thinkingLevel = %q, want extended", p.ThinkingLevel)
	}
	if p.Notes != "for risky refactors" {
		t.Fatalf("profile notes = %q, want the created notes", p.Notes)
	}
}

// TestMCPTools_RunListUnknownStatusRejected proves run_list's MCP
// surface inherits the daemon's status validation: an unrecognized
// status is a clear tool error, not a silently empty list.
func TestMCPTools_RunListUnknownStatusRejected(t *testing.T) {
	env := newMCPSession(t)

	res, err := env.cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "run_list",
		Arguments: map[string]any{"status": "bogus"},
	})
	if err != nil {
		t.Fatalf("CallTool error = %v, want a result", err)
	}
	if !res.IsError {
		t.Fatal("run_list status=bogus: IsError = false, want a tool error")
	}
	msg := mcpToolErrorText(res)
	for _, want := range []string{"run_list:", "bogus", "running", "interrupted"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error text = %q, want it to mention %q", msg, want)
		}
	}
}
