package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestWorkspaceSetScripts_NoMCPTool pins ADR-0024 / PR-E AC7: no MCP tool
// can read or write a workspace's setup/teardown script text, and the
// outputs of the task tools never contain it. task_get/workspace_list are
// not on this base; the exposure check covers task_new and task_list.
func TestWorkspaceSetScripts_NoMCPTool(t *testing.T) {
	env := newMCPSession(t)

	res, err := env.cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	for _, tool := range res.Tools {
		// Names and input property names only: descriptions are prose.
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		raw, _ := json.Marshal(tool.InputSchema)
		_ = json.Unmarshal(raw, &schema)
		names := []string{tool.Name}
		for prop := range schema.Properties {
			names = append(names, prop)
		}
		for _, n := range names {
			for _, banned := range []string{"setup", "teardown", "script"} {
				if strings.Contains(strings.ToLower(n), banned) {
					t.Fatalf("tool %q has name/property %q: no MCP tool may set or read worktree scripts", tool.Name, n)
				}
			}
		}
	}

	client := dialTestClient(t, env.srvURL)
	wsID := createTestWorkspace(t, client)
	const canary = "echo CANARY_SCRIPT_TEXT"
	if err := client.Call(context.Background(), "workspace.setScripts", map[string]any{
		"workspaceId": wsID, "teardownScript": canary,
	}, nil); err != nil {
		t.Fatalf("workspace.setScripts: %v", err)
	}

	for _, call := range []struct {
		tool string
		args map[string]any
	}{
		{"task_new", map[string]any{"workspaceId": wsID, "title": "T"}},
		{"task_list", map[string]any{"workspaceId": wsID}},
	} {
		out, err := env.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: call.tool, Arguments: call.args})
		if err != nil || out.IsError {
			t.Fatalf("%s: err=%v isError=%v", call.tool, err, out != nil && out.IsError)
		}
		raw, _ := json.Marshal(out)
		if strings.Contains(string(raw), "CANARY_SCRIPT_TEXT") {
			t.Fatalf("%s output leaks script text: %s", call.tool, raw)
		}
	}
}
