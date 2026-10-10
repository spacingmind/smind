package main

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// task_send tests: the ADR-0019 decision-6 guard (S17 of
// docs/plans/active/provider-native-permission-modes.md, AC11) plus the
// plan's own task_send scenarios (runId returned immediately and the run
// really started, invalid args, chat-scoped send).

// createTestProfile creates an agent profile over the daemon's own
// profile.create RPC, returning its id.
func createTestProfile(t *testing.T, env *mcpTestEnv, provider, mode string, autoAccept bool) int64 {
	t.Helper()
	client := dialTestClient(t, env.srvURL)
	var profile struct {
		ID int64 `json:"ID"`
	}
	params := map[string]any{"name": "p", "provider": provider}
	if mode != "" {
		params["permissionMode"] = mode
	}
	if autoAccept {
		params["autoAccept"] = true
	}
	if err := client.Call(context.Background(), "profile.create", params, &profile); err != nil {
		t.Fatalf("profile.create: %v", err)
	}
	return profile.ID
}

// taskSendErr calls task_send expecting an error, returning its text.
func taskSendErr(t *testing.T, env *mcpTestEnv, args map[string]any) string {
	t.Helper()
	res, err := env.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "task_send", Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(task_send) error = %v", err)
	}
	if !res.IsError {
		t.Fatalf("task_send(%v) succeeded, want the guard's rejection", args)
	}
	return mcpToolErrorText(res)
}

// TestMCPTools_TaskSendRejectsAutoApprovingMode pins ADR-0019 decision 6:
// an orchestrating agent may not pick an auto-approving permission
// configuration itself (bypass mode or autoAccept), while the same
// configuration is fine when it comes from the human-authored profile
// named by profileId. Explicit caller values are never trusted -- also
// when they ride along with a profileId, or contradict it.
func TestMCPTools_TaskSendRejectsAutoApprovingMode(t *testing.T) {
	env := newMCPSession(t)
	client := dialTestClient(t, env.srvURL)
	taskID := newTestRepoTask(t, client)

	// A catalog-listed bypass mode (claude-native's bypassPermissions) is
	// rejected before any run starts.
	msg := taskSendErr(t, env, map[string]any{
		"taskId": taskID, "provider": "claude-native", "prompt": "hi",
		"permissionMode": "bypassPermissions",
	})
	if !strings.Contains(msg, "bypassPermissions") || !strings.Contains(msg, "auto-approves") {
		t.Fatalf("bypass rejection = %q, want it to name the mode and the rule", msg)
	}

	// An ACP bypass-style mode id (glm "bypass_permissions", caught by the
	// acpBypassLike heuristic even undiscovered) is rejected too.
	taskSendErr(t, env, map[string]any{
		"taskId": taskID, "provider": "glm", "prompt": "hi",
		"permissionMode": "bypass_permissions",
	})

	// autoAccept: true is rejected outright.
	msg = taskSendErr(t, env, map[string]any{
		"taskId": taskID, "provider": "glm", "prompt": "hi", "autoAccept": true,
	})
	if !strings.Contains(msg, "autoAccept") {
		t.Fatalf("autoAccept rejection = %q, want it to name autoAccept", msg)
	}

	// The same glm bypass mode via a human-authored profile id is allowed.
	bypassProfile := createTestProfile(t, env, "glm", "bypass_permissions", false)

	// Pin the profile.get wire keys against the real wsapi response
	// (store.AgentProfile has no json tags, so its Go field names should
	// be the JSON keys): the guard reads Provider/PermissionMode/
	// AutoAccept, and a wrong key would silently downgrade a bypass
	// profile to no mode at all.
	var got struct {
		ID             int64  `json:"ID"`
		Provider       string `json:"Provider"`
		PermissionMode string `json:"PermissionMode"`
		AutoAccept     bool   `json:"AutoAccept"`
	}
	if err := client.Call(context.Background(), "profile.get", map[string]any{"id": bypassProfile}, &got); err != nil {
		t.Fatalf("profile.get: %v", err)
	}
	if got.ID != bypassProfile || got.Provider != "glm" || got.PermissionMode != "bypass_permissions" {
		t.Fatalf("profile.get decoded = %+v, want the bypass_permissions glm profile -- wire keys changed?", got)
	}

	var sent taskSendOutput
	if isErr := callMCPTool(t, env.cs, "task_send", map[string]any{
		"taskId": taskID, "prompt": "hi", "profileId": bypassProfile,
	}, &sent); isErr {
		t.Fatal("task_send via a bypass profile was rejected, want it accepted")
	}
	if sent.RunID == "" {
		t.Fatal("task_send via profile returned no runId")
	}

	// profileId plus an explicit auto-approving mode is rejected --
	// explicit caller values are never trusted, even next to a profile.
	taskSendErr(t, env, map[string]any{
		"taskId": taskID, "prompt": "hi", "profileId": bypassProfile,
		"permissionMode": "bypass_permissions",
	})

	// profileId plus a contradicting provider is rejected: a
	// human-approved bypass configuration for one provider must not be
	// applied to another.
	msg = taskSendErr(t, env, map[string]any{
		"taskId": taskID, "prompt": "hi", "profileId": bypassProfile,
		"provider": "claude-native",
	})
	if !strings.Contains(msg, "does not match") {
		t.Fatalf("provider/profile mismatch rejection = %q, want it to name the mismatch", msg)
	}
}

// TestMCPTools_TaskSendReturnsRunIdImmediately is the plan's happy-path
// task_send scenario: run.start's runId comes back at once (the tool never
// blocks on the run), and a run.logs call confirms the run really started.
func TestMCPTools_TaskSendReturnsRunIdImmediately(t *testing.T) {
	env := newMCPSession(t)
	client := dialTestClient(t, env.srvURL)
	taskID := newTestRepoTask(t, client)

	var sent taskSendOutput
	if isErr := callMCPTool(t, env.cs, "task_send", map[string]any{
		"taskId": taskID, "provider": "glm", "prompt": "hello there",
	}, &sent); isErr {
		t.Fatal("task_send returned an error result")
	}
	if sent.RunID == "" {
		t.Fatal("task_send returned no runId")
	}

	// The run is registered (run.logs answers with its id) and, driven by
	// the fake agent, actually completes.
	var logs struct {
		RunID  string `json:"runId"`
		Status string `json:"status"`
	}
	if err := client.Call(context.Background(), "run.logs", map[string]any{"runId": sent.RunID}, &logs); err != nil {
		t.Fatalf("run.logs(%s): %v", sent.RunID, err)
	}
	if logs.RunID != sent.RunID {
		t.Fatalf("run.logs runId = %q, want %q", logs.RunID, sent.RunID)
	}
	waitForRunStatus(t, client, sent.RunID, "done")
}

// TestMCPTools_TaskSendInvalidArgs covers the plan's invalid-tool-args
// scenario for task_send: a blank prompt and a missing taskId are tool
// errors (not panics), and a type-invalid taskId is caught by the SDK's
// schema validation before the handler runs.
func TestMCPTools_TaskSendInvalidArgs(t *testing.T) {
	env := newMCPSession(t)
	client := dialTestClient(t, env.srvURL)
	taskID := newTestRepoTask(t, client)

	if msg := taskSendErr(t, env, map[string]any{"taskId": taskID, "provider": "glm", "prompt": "   "}); !strings.Contains(msg, "prompt is required") {
		t.Fatalf("blank prompt error = %q", msg)
	}
	// A missing taskId is caught by the SDK's schema validation (the
	// field is required), which names it in the error.
	if msg := taskSendErr(t, env, map[string]any{"provider": "glm", "prompt": "hi"}); !strings.Contains(msg, "taskId") {
		t.Fatalf("missing taskId error = %q, want it to name taskId", msg)
	}

	res, err := env.cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "task_send",
		Arguments: map[string]any{"taskId": "not-a-number", "provider": "glm", "prompt": "hi"},
	})
	if err != nil {
		t.Fatalf("CallTool(task_send, bad type) error = %v, want a result with IsError", err)
	}
	if !res.IsError {
		t.Fatal("task_send with a string taskId: IsError = false, want schema validation to reject it")
	}
	if msg := mcpToolErrorText(res); !strings.Contains(msg, "taskId") && !strings.Contains(msg, "arguments") {
		t.Fatalf("error text = %q, want it to name the offending argument", msg)
	}
}

// TestMCPTools_TaskSendChatScoped is the plan's chat-scoped scenario: a
// task_send with an explicit chatId (from chat_new) lands its run under
// that chat, verified via run.list's chat filter.
func TestMCPTools_TaskSendChatScoped(t *testing.T) {
	env := newMCPSession(t)
	client := dialTestClient(t, env.srvURL)
	taskID := newTestRepoTask(t, client)

	var created struct {
		Chat chatForMCPTest `json:"chat"`
	}
	if isErr := callMCPTool(t, env.cs, "chat_new", map[string]any{"taskId": taskID, "title": "Side thread"}, &created); isErr {
		t.Fatal("chat_new returned an error result")
	}

	var sent taskSendOutput
	if isErr := callMCPTool(t, env.cs, "task_send", map[string]any{
		"taskId": taskID, "chatId": created.Chat.ID, "provider": "glm", "prompt": "hi",
	}, &sent); isErr {
		t.Fatal("chat-scoped task_send returned an error result")
	}
	waitForRunStatus(t, client, sent.RunID, "done")

	// run.list returns a bare array of RunSummary wire objects.
	var listed []struct {
		ID string `json:"ID"`
	}
	if err := client.Call(context.Background(), "run.list", map[string]any{"chatId": created.Chat.ID}, &listed); err != nil {
		t.Fatalf("run.list(chatId): %v", err)
	}
	if len(listed) != 1 || listed[0].ID != sent.RunID {
		t.Fatalf("run.list(chatId=%d) = %+v, want exactly [%s]", created.Chat.ID, listed, sent.RunID)
	}

	// An unbound chat pins no provider; without one (and no profile)
	// task_send says so rather than starting a provider-less run.
	msg := taskSendErr(t, env, map[string]any{
		"taskId": taskID, "chatId": created.Chat.ID + 999999, "prompt": "hi",
	})
	if !strings.Contains(msg, "does not belong") {
		t.Fatalf("foreign chatId error = %q, want it to name the chat/task mismatch", msg)
	}
}
