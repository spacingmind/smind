package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// callMCPToolErr calls name expecting a tool-level error and returns its
// text.
func callMCPToolErr(t *testing.T, cs *mcp.ClientSession, name string, args any) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s) error = %v", name, err)
	}
	if !res.IsError {
		t.Fatalf("%s succeeded, want a tool error", name)
	}
	return mcpToolErrorText(res)
}

func findLiveOption(opts []liveConfigOption, id string) *liveConfigOption {
	for i := range opts {
		if opts[i].ConfigID == id {
			return &opts[i]
		}
	}
	return nil
}

// TestMCPTools_TaskSetMode proves task_set_mode switches a live run to a
// non-auto-approving mode and the daemon's run state reflects it.
func TestMCPTools_TaskSetMode(t *testing.T) {
	env := newMCPSessionWithAgentArgs(t, "modes:session")
	runID := startTestRun(t, env.srvURL, "glm", "hang")

	var out taskSetModeOutput
	if isErr := callMCPTool(t, env.cs, "task_set_mode", map[string]any{"runId": runID, "modeId": "accept_edits"}, &out); isErr {
		t.Fatal("task_set_mode returned an error result")
	}
	if out.RunID != runID || out.PermissionMode != "accept_edits" || out.AutoAccept {
		t.Fatalf("task_set_mode = %+v, want accept_edits, autoAccept false", out)
	}
}

// TestMCPTools_TaskSetModeRejectsAutoApproving proves an auto-approving
// mode is refused (ADR-0019 #6) and the run's mode is left unchanged.
func TestMCPTools_TaskSetModeRejectsAutoApproving(t *testing.T) {
	env := newMCPSessionWithAgentArgs(t, "modes:session")
	runID := startTestRun(t, env.srvURL, "glm", "hang")

	msg := callMCPToolErr(t, env.cs, "task_set_mode", map[string]any{"runId": runID, "modeId": "bypass_permissions"})
	if !strings.Contains(msg, "auto-approving") {
		t.Fatalf("error = %q, want the auto-approving rejection", msg)
	}

	// A human-source no-op call reads back the run's current mode.
	client := dialTestClient(t, env.srvURL)
	var state taskSetModeOutput
	if err := client.Call(context.Background(), "run.setPermissionMode", map[string]any{"runId": runID, "autoAccept": false}, &state); err != nil {
		t.Fatalf("run.setPermissionMode (read-back): %v", err)
	}
	if state.PermissionMode == "bypass_permissions" {
		t.Fatalf("run permissionMode = %q after rejected switch", state.PermissionMode)
	}
}

// TestMCPTools_TaskOptionsNotSupportedProvider proves a non-ACP provider
// yields the documented "not supported" outcome as a normal result.
func TestMCPTools_TaskOptionsNotSupportedProvider(t *testing.T) {
	env := newMCPSession(t)
	runID := startTestRun(t, env.srvURL, "claude-native")

	var out taskOptionsOutput
	if isErr := callMCPTool(t, env.cs, "task_options", map[string]any{"runId": runID}, &out); isErr {
		t.Fatal("task_options returned an error result for a non-ACP provider")
	}
	if out.Supported || len(out.Options) != 0 {
		t.Fatalf("task_options = %+v, want supported false and no options", out)
	}
}

// TestMCPTools_TaskSetOptionThoughtLevel proves a thought_level option can
// be set and the change shows in a later task_options.
func TestMCPTools_TaskSetOptionThoughtLevel(t *testing.T) {
	env := newMCPSessionWithAgentArgs(t, "live-opts", "modes:config")
	runID := startTestRun(t, env.srvURL, "glm", "hang")

	var before taskOptionsOutput
	callMCPTool(t, env.cs, "task_options", map[string]any{"runId": runID}, &before)
	if o := findLiveOption(before.Options, "thinking-level"); !before.Supported || o == nil || o.Category != "thought_level" {
		t.Fatalf("task_options = %+v, want a thought_level thinking-level option", before)
	}

	var set taskOptionsOutput
	if isErr := callMCPTool(t, env.cs, "task_set_option", map[string]any{"runId": runID, "configId": "thinking-level", "value": "high"}, &set); isErr {
		t.Fatal("task_set_option returned an error result")
	}

	var after taskOptionsOutput
	callMCPTool(t, env.cs, "task_options", map[string]any{"runId": runID}, &after)
	o := findLiveOption(after.Options, "thinking-level")
	if o == nil || !strings.Contains(toJSON(t, o.CurrentValue), `"high"`) {
		t.Fatalf("task_options after set = %+v, want thinking-level high", after)
	}
}

// TestMCPTools_TaskSetOptionRejectsModeCategory proves a mode-category
// option is refused and left unchanged.
func TestMCPTools_TaskSetOptionRejectsModeCategory(t *testing.T) {
	env := newMCPSessionWithAgentArgs(t, "live-opts", "modes:config")
	runID := startTestRun(t, env.srvURL, "glm", "hang")

	msg := callMCPToolErr(t, env.cs, "task_set_option", map[string]any{"runId": runID, "configId": "mode", "value": "accept_edits"})
	if !strings.Contains(msg, "thought_level or model") {
		t.Fatalf("error = %q, want the category allowlist rejection", msg)
	}
	var after taskOptionsOutput
	callMCPTool(t, env.cs, "task_options", map[string]any{"runId": runID}, &after)
	if o := findLiveOption(after.Options, "mode"); o == nil || toJSON(t, o.CurrentValue) != `"default"` {
		t.Fatalf("mode option after rejected set = %+v, want unchanged default", o)
	}
}

// TestMCPTools_TaskSetOptionRejectsUncategorized proves an option with no
// category, and an unknown one, are refused (fail-closed).
func TestMCPTools_TaskSetOptionRejectsUncategorized(t *testing.T) {
	env := newMCPSessionWithAgentArgs(t, "live-opts", "modes:config")
	runID := startTestRun(t, env.srvURL, "glm", "hang")

	for configID, want := range map[string]string{"web-search": "thought_level or model", "no-such-option": "unknown configId"} {
		msg := callMCPToolErr(t, env.cs, "task_set_option", map[string]any{"runId": runID, "configId": configID, "value": "false"})
		if !strings.Contains(msg, want) {
			t.Errorf("%s: error = %q, want containing %q", configID, msg, want)
		}
	}
	var after taskOptionsOutput
	callMCPTool(t, env.cs, "task_options", map[string]any{"runId": runID}, &after)
	if o := findLiveOption(after.Options, "web-search"); o == nil || toJSON(t, o.CurrentValue) != "true" {
		t.Fatalf("web-search after rejected set = %+v, want unchanged true", o)
	}
}

func toJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}
