package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/wsclient"
)

// registerMCPDiscoveryTools adds PR-A's read-only discovery tools: the
// workspace/task/run/provider/profile listings an orchestrator walks
// before creating anything. Like every other tool here, each is one
// wsapi round trip over the same client the CLI uses, wrapping the
// daemon's result in an object ({"workspaces": [...]}, ...) per the
// output-shape note on taskNewOutput -- never a bare array, and an empty
// list is empty, not null.
func registerMCPDiscoveryTools(srv *mcp.Server, client *wsclient.Client) {
	mcp.AddTool(srv, &mcp.Tool{Name: "workspace_list", Description: "List every workspace (id, title, path) registered with the daemon -- the first call of the orchestration loop, since task_new needs a workspaceId."}, mcpWorkspaceList(client))
	mcp.AddTool(srv, &mcp.Tool{Name: "task_get", Description: "Get one task by id (id, title, status, branch, worktreePath, parentTaskId) -- the worktreePath is where to review a task's work with git. A root task with a human-set tree budget also carries budgetUsd, budgetSpentUsd, budgetPartial, budgetUnknownRuns, budgetRemainingUsd and overBudget (read-only; once over budget, agent launches in the tree are refused)."}, mcpTaskGet(client))
	mcp.AddTool(srv, &mcp.Tool{Name: "run_list", Description: "List runs, newest first (at most 200), optionally filtered by taskId (across all of that task's chats), chatId, or status (e.g. \"running\"). Use this to recover a lost run id after task_send. A failed run with failureKind rate_limited hit a provider quota: resetAt says when it resets; retry on another provider or after resetAt, do not retry immediately."}, mcpRunList(client))
	mcp.AddTool(srv, &mcp.Tool{Name: "provider_list", Description: "List the daemon's supported providers, each with its permission modes (id, autoApproves) and defaultMode. An orchestrator may not pick an auto-approving mode or autoAccept itself -- pass profileId (a human-authored profile) instead. A provider with cooldownUntil (RFC 3339 UTC) is rate limited until then: sends to it by an agent fail fast, so choose another provider."}, mcpProviderList(client))
	mcp.AddTool(srv, &mcp.Tool{Name: "profile_list", Description: "List the human-authored agent profiles (id, name, provider, permissionMode, autoAccept, thinkingLevel, notes, and when set onRateLimit / fallbackProfileId; plus the seat contract: role, orchestrationTools, childPolicy, childProfileIds, skills, hasInstructions -- never the instruction text). Pass a profile's id as task_send's profileId to run with its settings."}, mcpProfileList(client))
}

// workspaceListOutput is workspace_list's structured output: every
// workspace under "workspaces", empty rather than null when none.
type workspaceListOutput struct {
	Workspaces []map[string]any `json:"workspaces"`
	Host       string           `json:"host,omitempty"`
}

// workspaceListInput is workspace_list's argument shape: only the optional host.
type workspaceListInput struct {
	Host string `json:"host,omitempty" jsonschema:"optional alias or id of a connected host (see list_hosts): read that host instead of this daemon. Direct reads only (hop limit 1): a task created by a peer cannot use it. Omit for this daemon"`
}

// mcpWorkspaceList wraps workspace.list (no params): every workspace the
// daemon knows, id/title/path riding the store.Workspace wire shape.
func mcpWorkspaceList(client *wsclient.Client) mcp.ToolHandlerFor[workspaceListInput, workspaceListOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in workspaceListInput) (*mcp.CallToolResult, workspaceListOutput, error) {
		if in.Host != "" {
			out, err := remoteWorkspaceList(ctx, client, in.Host)
			return nil, out, err
		}
		var workspaces []map[string]any
		if err := callWS(ctx, client, "workspace_list", "workspace.list", nil, &workspaces); err != nil {
			return nil, workspaceListOutput{}, err
		}
		if workspaces == nil {
			workspaces = []map[string]any{}
		}
		return nil, workspaceListOutput{Workspaces: workspaces}, nil
	}
}

// taskGetInput is task_get's argument shape (wrapping task.get).
type taskGetInput struct {
	ID   int64  `json:"id" jsonschema:"id of the task to fetch"`
	Host string `json:"host,omitempty" jsonschema:"optional alias or id of a connected host (see list_hosts): read that host instead of this daemon. Direct reads only (hop limit 1): a task created by a peer cannot use it. Omit for this daemon"`
}

// taskGetOutput is task_get's structured output: the task under "task"
// (id, title, status, branch, worktreePath, parentTaskId -- the
// store.Task wire shape).
type taskGetOutput struct {
	Task map[string]any `json:"task"`
}

// mcpTaskGet wraps task.get: {id} -> the task. An unknown id surfaces as
// the daemon's own "not found" rejection via callWS, so the orchestrator
// sees a clear tool error rather than an empty success.
func mcpTaskGet(client *wsclient.Client) mcp.ToolHandlerFor[taskGetInput, taskGetOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in taskGetInput) (*mcp.CallToolResult, taskGetOutput, error) {
		if in.Host != "" {
			out, err := remoteTaskGet(ctx, client, in)
			return nil, out, err
		}
		var task map[string]any
		if err := callWS(ctx, client, "task_get", "task.get", map[string]any{"id": in.ID}, &task); err != nil {
			return nil, taskGetOutput{}, err
		}
		if budget, ok := task["budgetUsd"].(float64); ok {
			spent, _ := task["budgetSpentUsd"].(float64)
			task["budgetRemainingUsd"] = budgetRemaining(budget, spent)
		}
		return nil, taskGetOutput{Task: task}, nil
	}
}

// runListInput is run_list's argument shape (wrapping run.list): all
// filters optional -- none set means every run, newest first.

// mcpRunListCap bounds run_list's result client-side: the daemon's
// filtered run.list path already caps at 200, but the unfiltered path
// (no taskId/status) is the registry's full retention set -- truncate
// here to the newest mcpRunListCap so the tool's bound holds either way.
const mcpRunListCap = 200

// capRuns empties nil into an empty list (never null) and truncates to
// the newest mcpRunListCap entries; runs must already be newest first
// (index 0 is the newest), which every run.list path guarantees, so the
// cap drops from the tail -- the oldest -- end.
func capRuns(runs []map[string]any) []map[string]any {
	if runs == nil {
		return []map[string]any{}
	}
	if len(runs) > mcpRunListCap {
		runs = runs[:mcpRunListCap]
	}
	for _, row := range runs {
		addFailureFields(row)
	}
	return runs
}

type runListInput struct {
	TaskID *int64 `json:"taskId,omitempty" jsonschema:"optional: restrict to one task's runs, across all of its chats"`
	ChatID *int64 `json:"chatId,omitempty" jsonschema:"optional: restrict to one chat's runs"`
	Status string `json:"status,omitempty" jsonschema:"optional: restrict to runs in exactly this status (\"running\", \"done\", \"error\", \"stopped\")"`
}

// runListOutput is run_list's structured output: the runs under "runs",
// empty rather than null when none match.
type runListOutput struct {
	Runs []map[string]any `json:"runs"`
}

// mcpRunList wraps run.list: {taskId?, chatId?, status?} -> runs, newest
// first (the daemon caps the filtered listing at 200). Recovering a lost
// run id after task_send is this tool's main job: run_list {taskId} then
// read the newest run's id.
func mcpRunList(client *wsclient.Client) mcp.ToolHandlerFor[runListInput, runListOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in runListInput) (*mcp.CallToolResult, runListOutput, error) {
		params := map[string]any{}
		if in.TaskID != nil {
			params["taskId"] = *in.TaskID
		}
		if in.ChatID != nil {
			params["chatId"] = *in.ChatID
		}
		if in.Status != "" {
			params["status"] = in.Status
		}
		var runs []map[string]any
		if err := callWS(ctx, client, "run_list", "run.list", params, &runs); err != nil {
			return nil, runListOutput{}, err
		}
		return nil, runListOutput{Runs: capRuns(runs)}, nil
	}
}

// providerListOutput is provider_list's structured output: the
// providers under "providers", each carrying its own modes (with
// autoApproves per mode) and defaultMode -- the providerInfo wire shape.
// Entries are passed through as-is, so a provider.list entry's
// cooldownUntil (ADR-0041 F1: RFC 3339 UTC, present only while the provider
// is rate limited) reaches the agent unchanged.
type providerListOutput struct {
	Providers []map[string]any `json:"providers"`
}

// mcpProviderList wraps provider.list (no params): every supported
// provider with its permission-mode catalog. The mode entries'
// autoApproves flags exist so an orchestrator can *avoid* those modes
// (ADR-0019 decision 6): selecting one requires a human-authored profile.
func mcpProviderList(client *wsclient.Client) mcp.ToolHandlerFor[struct{}, providerListOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, providerListOutput, error) {
		var result struct {
			Providers []map[string]any `json:"providers"`
		}
		if err := callWS(ctx, client, "provider_list", "provider.list", nil, &result); err != nil {
			return nil, providerListOutput{}, err
		}
		if result.Providers == nil {
			result.Providers = []map[string]any{}
		}
		return nil, providerListOutput{Providers: result.Providers}, nil
	}
}

// profileListOutput is profile_list's structured output: the
// human-authored profiles under "profiles", empty rather than null.
type profileListOutput struct {
	Profiles []map[string]any `json:"profiles"`
}

// mcpProfileList wraps profile.list (no params): every agent profile
// (id, name, provider, permissionMode, autoAccept, thinkingLevel,
// notes). The notes field is the human's own guidance for when the
// profile applies -- an orchestrator choosing between profiles should
// read it.
func mcpProfileList(client *wsclient.Client) mcp.ToolHandlerFor[struct{}, profileListOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, profileListOutput, error) {
		var raw json.RawMessage
		if err := callWS(ctx, client, "profile_list", "profile.list", nil, &raw); err != nil {
			return nil, profileListOutput{}, err
		}
		var profiles []map[string]any
		var typed []store.AgentProfile
		if err := json.Unmarshal(raw, &profiles); err != nil {
			return nil, profileListOutput{}, fmt.Errorf("profile_list: decode: %w", err)
		}
		if err := json.Unmarshal(raw, &typed); err != nil {
			return nil, profileListOutput{}, fmt.Errorf("profile_list: decode: %w", err)
		}
		if profiles == nil {
			profiles = []map[string]any{}
		}
		for i, p := range profiles {
			addProfileFailoverFields(p)
			addProfileContractFields(p, typed[i])
		}
		return nil, profileListOutput{Profiles: profiles}, nil
	}
}

// addFailureFields adds the camelCase failureKind, window and resetAt keys
// (ADR-0041) to a run.list row that carries a typed failure; resetAt is
// RFC 3339 UTC and absent when the reset time is unknown. Rows without a
// failure are left untouched.
func addFailureFields(row map[string]any) {
	kind, _ := row["FailureKind"].(string)
	if kind == "" {
		return
	}
	row["failureKind"] = kind
	if w, _ := row["FailureWindow"].(string); w != "" {
		row["window"] = w
	}
	if s, _ := row["FailureResetAt"].(string); s != "" {
		if ts, err := time.Parse(time.RFC3339Nano, s); err == nil {
			row["resetAt"] = ts.UTC().Format(time.RFC3339)
		}
	}
	if inferred, _ := row["FailureResetInferred"].(bool); inferred {
		row["resetInferred"] = true
	}
}

// addProfileContractFields adds the camelCase ADR-0041 seat-contract keys
// to one decoded profile: orchestrationTools (omitted when unmanaged), role,
// childPolicy, childProfileIds and skills (omitted when empty). The
// instructions text is never returned -- role text can be long and is for the
// run, not for browsing -- so the Go-named Instructions key is removed and
// hasInstructions reports whether there is any.
func addProfileContractFields(p map[string]any, t store.AgentProfile) {
	if t.OrchestrationTools != nil {
		p["orchestrationTools"] = *t.OrchestrationTools
	}
	p["role"] = profileRole(t)
	p["childPolicy"] = t.ChildPolicy
	ids := t.ChildProfileIDs
	if ids == nil {
		ids = []int64{}
	}
	p["childProfileIds"] = ids
	if len(t.Skills) > 0 {
		p["skills"] = t.Skills
	}
	delete(p, "Instructions")
	if t.Instructions != "" {
		p["hasInstructions"] = true
	}
}

// addProfileFailoverFields adds the camelCase onRateLimit /
// fallbackProfileId keys (ADR-0041 F2) to one decoded profile, omitting
// them when the policy is "none" / there is no fallback. The profile's
// Go-named keys (OnRateLimit, FallbackProfileID) are left as they are.
func addProfileFailoverFields(p map[string]any) {
	if v, ok := p["OnRateLimit"].(string); ok && v != "" && v != "none" {
		p["onRateLimit"] = v
	}
	if v, ok := p["FallbackProfileID"]; ok && v != nil {
		p["fallbackProfileId"] = v
	}
}
