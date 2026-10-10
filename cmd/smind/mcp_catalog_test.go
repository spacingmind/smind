package main

import (
	"bytes"
	"context"
	"encoding/json"
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
	want := []string{"task_new", "task_list", "chat_list", "chat_new", "task_send", "task_status", "task_logs", "task_permissions", "task_wait", "task_stop"}
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
