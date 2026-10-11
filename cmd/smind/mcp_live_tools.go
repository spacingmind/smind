package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spacingmind/smind/internal/wsclient"
)

// The live-settings tools (PR-C of the orchestration-parity plan): let an
// orchestrating agent change a running sub-agent's permission mode and
// session config options without a human in the loop -- within ADR-0019
// decision 6's limit. Every call carries source "orchestrator", so the
// daemon (not this process) refuses anything that would widen what the
// sub-agent may do unattended: auto-approving modes, autoAccept, and any
// config option outside the thought_level/model categories.

// registerMCPLiveTools adds task_set_mode, task_options and
// task_set_option to the catalog.
func registerMCPLiveTools(srv *mcp.Server, client *wsclient.Client) {
	mcp.AddTool(srv, &mcp.Tool{Name: "task_set_mode", Description: "Switch a run's provider-native permission mode (modeId from the provider's own catalog). An orchestrating agent may NOT pick an auto-approving mode (bypass/yolo/dangerously-skip-permissions style modes) -- the daemon rejects it; ask a human to switch to one."}, mcpTaskSetMode(client))
	mcp.AddTool(srv, &mcp.Tool{Name: "task_options", Description: "List a run's live session config options (configId, name, category, current value, choices). Providers without ACP config options (or runs with no live session) return an empty list with supported: false -- not an error. Only options in the thought_level and model categories can be changed with task_set_option."}, mcpTaskOptions(client))
	mcp.AddTool(srv, &mcp.Tool{Name: "task_set_option", Description: "Set one live session config option on a run (e.g. the thinking level, or the model). Fail-closed: only options whose category is thought_level or model may be set by an orchestrating agent; mode-category, uncategorized and unknown options are rejected -- permission changes go through task_set_mode (which also refuses auto-approving modes) or a human."}, mcpTaskSetOption(client))
}

// taskSetModeInput is task_set_mode's argument shape.
type taskSetModeInput struct {
	RunID  string `json:"runId" jsonschema:"id of the run (returned by task_send)"`
	ModeID string `json:"modeId" jsonschema:"provider-native permission mode id to switch to; must not be an auto-approving mode"`
}

// taskSetModeOutput is task_set_mode's structured output: the run's
// permission settings after the switch.
type taskSetModeOutput struct {
	RunID          string `json:"runId"`
	PermissionMode string `json:"permissionMode"`
	AutoAccept     bool   `json:"autoAccept"`
}

// mcpTaskSetMode wraps run.setPermissionMode with source "orchestrator".
func mcpTaskSetMode(client *wsclient.Client) mcp.ToolHandlerFor[taskSetModeInput, taskSetModeOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in taskSetModeInput) (*mcp.CallToolResult, taskSetModeOutput, error) {
		var result struct {
			PermissionMode string `json:"permissionMode"`
			AutoAccept     bool   `json:"autoAccept"`
		}
		params := map[string]any{"runId": in.RunID, "modeId": in.ModeID, "source": "orchestrator"}
		if err := callWS(ctx, client, "task_set_mode", "run.setPermissionMode", params, &result); err != nil {
			return nil, taskSetModeOutput{}, err
		}
		return nil, taskSetModeOutput{RunID: in.RunID, PermissionMode: result.PermissionMode, AutoAccept: result.AutoAccept}, nil
	}
}

// liveConfigOption is one config option in tool output. Unlike the CLI's
// configOption mirror, CurrentValue is `any`: a json.RawMessage would make
// the SDK infer an array schema and reject the object-shaped value.
type liveConfigOption struct {
	ConfigID     string `json:"configId"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	Category     string `json:"category,omitempty"`
	Type         string `json:"type"`
	CurrentValue any    `json:"currentValue,omitempty"`
	Options      []struct {
		Value       string `json:"value"`
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
	} `json:"options,omitempty"`
}

// taskOptionsOutput is task_options's and task_set_option's structured
// output. Supported is false when the list is empty -- the provider has no
// ACP config options, or the run has no live session -- which is the
// documented "not supported" outcome, a normal result rather than an error.
type taskOptionsOutput struct {
	RunID     string             `json:"runId"`
	Supported bool               `json:"supported"`
	Options   []liveConfigOption `json:"options"`
}

func liveOptionsOutput(runID string, opts []liveConfigOption) taskOptionsOutput {
	if opts == nil {
		opts = []liveConfigOption{}
	}
	return taskOptionsOutput{RunID: runID, Supported: len(opts) > 0, Options: opts}
}

// mcpTaskOptions wraps run.listConfigOptions.
func mcpTaskOptions(client *wsclient.Client) mcp.ToolHandlerFor[runIDInput, taskOptionsOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in runIDInput) (*mcp.CallToolResult, taskOptionsOutput, error) {
		var result struct {
			Options []liveConfigOption `json:"options"`
		}
		if err := callWS(ctx, client, "task_options", "run.listConfigOptions", map[string]any{"runId": in.RunID}, &result); err != nil {
			return nil, taskOptionsOutput{}, err
		}
		return nil, liveOptionsOutput(in.RunID, result.Options), nil
	}
}

// taskSetOptionInput is task_set_option's argument shape.
type taskSetOptionInput struct {
	RunID    string `json:"runId" jsonschema:"id of the run (returned by task_send)"`
	ConfigID string `json:"configId" jsonschema:"config option id from task_options; its category must be thought_level or model"`
	Value    string `json:"value" jsonschema:"new value (one of the option's listed choices)"`
}

// mcpTaskSetOption wraps run.setConfigOption with source "orchestrator".
func mcpTaskSetOption(client *wsclient.Client) mcp.ToolHandlerFor[taskSetOptionInput, taskOptionsOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in taskSetOptionInput) (*mcp.CallToolResult, taskOptionsOutput, error) {
		var result struct {
			Options []liveConfigOption `json:"options"`
		}
		params := map[string]any{"runId": in.RunID, "configId": in.ConfigID, "value": in.Value, "source": "orchestrator"}
		if err := callWS(ctx, client, "task_set_option", "run.setConfigOption", params, &result); err != nil {
			return nil, taskOptionsOutput{}, err
		}
		return nil, liveOptionsOutput(in.RunID, result.Options), nil
	}
}
