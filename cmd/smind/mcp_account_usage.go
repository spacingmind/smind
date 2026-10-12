package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spacingmind/smind/internal/wsclient"
)

// accountUsageInput deliberately has no refresh/mode input of any kind
// (AC53): the MCP tool is touch-only, so an orchestrating agent can never
// force a provider call -- the daemon-side poller budget is the only
// refresher, and account.usage is always called with mode "touch".
type accountUsageInput struct {
	AccountIDs []int64 `json:"accountIds,omitempty" jsonschema:"optional account ids to limit the report to; omit for every account"`
}

// accountUsageOutput is the daemon's account.usage result verbatim
// (internal/wsapi's AccountUsage wire shape rides along as-is under
// "reports").
type accountUsageOutput struct {
	Enabled    bool             `json:"enabled"`
	TTLSeconds int              `json:"ttlSeconds"`
	Reports    []map[string]any `json:"reports"`
}

// mcpAccountUsage wraps account.usage with a hard-coded mode "touch": the
// daemon's cache is refreshed at most once per TTL per account, and the
// tool itself can never ask for more.
func mcpAccountUsage(client *wsclient.Client) mcp.ToolHandlerFor[accountUsageInput, accountUsageOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in accountUsageInput) (*mcp.CallToolResult, accountUsageOutput, error) {
		params := map[string]any{"mode": "touch"}
		if in.AccountIDs != nil {
			params["accountIds"] = in.AccountIDs
		}
		var out accountUsageOutput
		if err := callWS(ctx, client, "account_usage", "account.usage", params, &out); err != nil {
			return nil, accountUsageOutput{}, err
		}
		if out.Reports == nil {
			out.Reports = []map[string]any{}
		}
		return nil, out, nil
	}
}

// registerMCPAccountUsageTool adds the read-only account_usage tool
// (plan usage windows) to the catalog, with ReadOnlyHint set.
func registerMCPAccountUsageTool(srv *mcp.Server, client *wsclient.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "account_usage",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
		Description: "Read-only plan usage (5-hour rolling and weekly limit windows) per account, distinct from usage_summary's token/cost counts. Served from a daemon-side cache that refreshes at most once per 5 minutes per account; the tool itself can never force a provider call (there is no refresh input). Optional accountIds limits the report to those accounts.",
	}, mcpAccountUsage(client))
}
