package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/spacingmind/smind/internal/gate"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spacingmind/smind/internal/auth"
	"github.com/spacingmind/smind/internal/config"
)

// TestMCPTools_NoApprovalToolInCatalog pins ADR-0017 resolved decision 1
// (AC7): the catalog never contains task_approve/task_deny or anything
// that could wrap run.respondPermission -- the human approves via the
// CLI/web UI, not the orchestrating agent.
func TestMCPTools_NoApprovalToolInCatalog(t *testing.T) {
	env := newMCPSession(t)
	for _, name := range mcpToolNames(t, env.cs) {
		if strings.Contains(name, "approve") || strings.Contains(name, "deny") || strings.Contains(name, "respond_permission") || strings.Contains(name, "respondPermission") {
			t.Fatalf("tools/list contains approval tool %q", name)
		}
	}
	want := []string{"task_new", "task_list", "chat_list", "chat_new", "task_send", "task_status", "task_logs", "task_permissions", "task_wait", "task_stop", "workspace_list", "task_get", "run_list", "provider_list", "profile_list", "task_archive", "task_set_mode", "task_options", "task_set_option", "task_diff", "queue_list", "queue_cancel", "usage_summary", "account_usage"}
	got := mcpToolNames(t, env.cs)
	for _, w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("tools/list = %v, want it to contain %q", got, w)
		}
	}
}

// TestMCPTools_NoToolCanEscalatePermissions: `smind task mode` is human-only
// (orchestration-parity D12), so nothing in the MCP catalog may take an
// source, and only task_send takes autoAccept (the daemon rejects true there:
// TestMCPTools_TaskSendRejectsAutoApprovingMode). task_set_mode can only ever
// switch to a mode the daemon accepts from the orchestrator source, and no tool
// can claim "human".
func TestMCPTools_NoToolCanEscalatePermissions(t *testing.T) {
	env := newMCPSession(t)
	res, err := env.cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var in struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(schema, &in); err != nil {
			t.Fatal(err)
		}
		if _, has := in.Properties["source"]; has {
			t.Errorf("MCP tool %s takes a source: an orchestrating agent must not be able to claim one", tool.Name)
		}
		if _, has := in.Properties["autoAccept"]; has && tool.Name != "task_send" {
			t.Errorf("MCP tool %s takes autoAccept: only task_send may (and the daemon refuses true)", tool.Name)
		}
		if tool.Name == "task_mode" || strings.Contains(tool.Name, "auto_accept") {
			t.Errorf("tools/list contains %q, which is human-only", tool.Name)
		}
	}
}

// TestMCPTools_SchemaValidationErrors pins AC4: an invalid call (wrong
// type for a required field) is rejected by the SDK's schema validation
// before the handler runs, surfacing as a tool result with IsError set
// and a schema-shaped message -- not a panic, and not an unlabeled wsapi
// error.
func TestMCPTools_SchemaValidationErrors(t *testing.T) {
	env := newMCPSession(t)
	cs := env.cs

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "task_new",
		// workspaceId must be a number; a string must never reach the
		// daemon, and the handler must never see a zero-value struct.
		Arguments: map[string]any{"workspaceId": "not-a-number", "title": "T"},
	})
	if err != nil {
		t.Fatalf("CallTool error = %v, want a result with IsError", err)
	}
	if !res.IsError {
		t.Fatalf("task_new with invalid args: IsError = false, want true (content: %+v)", res.Content)
	}
	var msg strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			msg.WriteString(tc.Text)
		}
	}
	if !strings.Contains(msg.String(), "workspaceId") && !strings.Contains(msg.String(), "arguments") {
		t.Fatalf("error text = %q, want it to name the offending argument", msg.String())
	}
}

// TestMCPTools_ToolErrorsDoNotLeakToken pins AC5's no-echo half: a tool
// call that really fails against the daemon (task_status on a runId the
// registry has never seen -> an RPC error, not an empty success) returns
// the tool-prefixed RPC error and never the auth token, in either text
// or structured content.
func TestMCPTools_ToolErrorsDoNotLeakToken(t *testing.T) {
	env := newMCPSession(t)
	cs := env.cs
	token := testDaemonToken(t)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "task_status",
		Arguments: map[string]any{"runId": "no-such-run"},
	})
	if err != nil {
		t.Fatalf("CallTool error = %v, want a result", err)
	}
	if !res.IsError {
		t.Fatal("task_status on a bogus runId: IsError = false, want the RPC failure surfaced")
	}

	var buf bytes.Buffer
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			buf.WriteString(tc.Text)
		}
	}
	if res.StructuredContent != nil {
		if data, err := json.Marshal(res.StructuredContent); err == nil {
			buf.Write(data)
		}
	}
	if bytes.Contains(buf.Bytes(), []byte(token)) {
		t.Fatalf("tool error output contains the auth token: %q", buf.String())
	}
	if want := "task_status:"; !bytes.Contains(buf.Bytes(), []byte(want)) {
		t.Fatalf("error text = %q, want the %q prefix", buf.String(), want)
	}
}

// testDaemonToken reads the token of the running test daemon, for the
// leak assertion above -- the same file dialDaemon reads via
// auth.LoadOrCreateToken(config.Dir()).
func testDaemonToken(t *testing.T) string {
	t.Helper()
	token, err := auth.LoadOrCreateToken(config.Dir())
	if err != nil {
		t.Fatalf("LoadOrCreateToken() error = %v", err)
	}
	return token
}

// TestMCPServer_InstructionsNameEveryTool pins PR-A AC6: the initialize
// result's instructions are non-empty and name every registered tool, so
// an orchestrator's guide can never drift behind the catalog -- adding a
// tool without documenting it in mcpInstructions fails here.
func TestMCPServer_InstructionsNameEveryTool(t *testing.T) {
	env := newMCPSession(t)

	init := env.cs.InitializeResult()
	if init == nil || init.Instructions == "" {
		t.Fatal("initialize result instructions = empty, want the orchestration guide")
	}
	for _, name := range mcpToolNames(t, env.cs) {
		if !strings.Contains(init.Instructions, name) {
			t.Fatalf("instructions do not mention tool %q", name)
		}
	}
}

// TestMCPServer_InstructionsExplainWaitLoopAndModes pins the dogfood G4/G2
// wording: a pending permission is not completion (loop: wait, human
// approves, wait again), and the way to cut prompts is a human-authored
// profile or a non-auto-approving mode -- never an auto-approving one.
func TestMCPServer_InstructionsExplainWaitLoopAndModes(t *testing.T) {
	env := newMCPSession(t)
	init := env.cs.InitializeResult()
	if init == nil {
		t.Fatal("no initialize result")
	}
	for _, want := range []string{
		"waiting_permission",
		"NOT completion",
		"task_wait again",
		"stopNote",
		"largeShrink",
		"profileId",
		"accept_edits",
		"autoApproves",
	} {
		if !strings.Contains(init.Instructions, want) {
			t.Errorf("instructions do not mention %q", want)
		}
	}
}

// TestValidationRPCs_MCPCatalogueHasNoVerdictToolForLead pins ADR-0045: the
// catalogue a Lead sees has no tool that writes, edits or waives a verdict
// or merges. (The validator's own entry, added with the seat, is separate.)
func TestValidationRPCs_MCPCatalogueHasNoVerdictToolForLead(t *testing.T) {
	env := newMCPSession(t)
	for _, name := range mcpToolNames(t, env.cs) {
		if strings.Contains(name, "verdict") || strings.Contains(name, "waive") || strings.Contains(name, "merge") {
			t.Fatalf("tools/list contains %q", name)
		}
	}
}

// TestMCPInstructions_ListEveryGateReason keeps the orchestrator's guide in
// step with the picker gate vocabulary (internal/gate).
func TestMCPInstructions_ListEveryGateReason(t *testing.T) {
	for _, reason := range []string{gate.ReasonNotSignedIn, gate.ReasonOutOfPlan, gate.ReasonCoolingDown, gate.ReasonUnsupportedByHarness, gate.ReasonModelNotInPlan} {
		if !strings.Contains(mcpInstructions, reason) {
			t.Fatalf("mcpInstructions does not mention gate reason %q", reason)
		}
	}
}
