package taskrunner

import (
	"context"
	"encoding/json"
	"fmt"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
	"github.com/spacingmind/smind/internal/acp"
)

// PermissionOption is one choice offered to a PermissionDecider, unifying
// both providers' genuinely different permission-request shapes (ACP's
// options list, Claude Code's synthesized allow/deny pair -- see
// acpDeciderAdapter/claudeDeciderAdapter) into one provider-agnostic shape.
type PermissionOption struct {
	// ID is opaque to callers of PermissionDecider: it's ACP's own
	// optionId for a GLM (or other ACP) turn, or a synthesized "allow"/
	// "deny" for a Claude Code native turn. Whatever ID a Decide call
	// returns must be one of the IDs it was offered.
	ID string

	// Label is a short human-readable name for this option (ACP's
	// PermissionOption.Name, or a fixed "Allow"/"Deny" for Claude Code).
	Label string

	// Kind categorizes Label per ACP's PermissionOptionKind:
	// "allow_once" | "allow_always" | "reject_once" | "reject_always".
	Kind string
}

// PermissionDecider decides a pending permission request raised mid-turn by
// either provider, blocking until an answer is available -- this is
// precisely what "the agent is waiting for a human" means, so a slow or
// never-returning Decide call is expected, not a bug (see RunPrompt's ctx
// cancellation for how a caller aborts it). Implementations that need to
// know which run's event stream to push the request onto (e.g. a
// human-in-the-loop UI) construct a PermissionDecider per call, closing
// over that context; internal/runs.Registry is today's only such
// implementation.
//
// When no PermissionDecider is supplied to RunPrompt, behavior is
// unchanged from before this type existed: each provider falls back to its
// own Runner-level acp.PermissionPolicy/claudecode.PermissionPolicy default
// (see WithACPPermissionPolicy/WithClaudeCodePermissionPolicy), which in
// turn default to acp.AutoApprovePolicy/claudecode.AutoDenyPolicy if never
// set at all.
type PermissionDecider interface {
	Decide(ctx context.Context, summary string, options []PermissionOption) (optionID string, err error)
}

// acpDeciderAdapter adapts a PermissionDecider to acp.PermissionPolicy: ACP's
// own request/response already is an options-list-in, optionId-out shape,
// so this is a direct translation with no synthesized options needed.
type acpDeciderAdapter struct {
	decider PermissionDecider
}

func (a acpDeciderAdapter) Decide(ctx context.Context, req acp.RequestPermissionParams) (string, error) {
	opts := make([]PermissionOption, len(req.Options))
	for i, o := range req.Options {
		opts[i] = PermissionOption{ID: o.OptionID, Label: o.Name, Kind: string(o.Kind)}
	}
	return a.decider.Decide(ctx, summarizeACPToolCall(req.ToolCall), opts)
}

// summarizeACPToolCall builds a human-readable summary of the tool call a
// session/request_permission is asking about, from ACP's raw ToolCallUpdate
// JSON. It prefers Title (the field agents populate for exactly this
// purpose, per ACP's ToolCallUpdate); ToolCallID is the fallback for an
// agent that omits it, so a request is never presented with no context at
// all.
func summarizeACPToolCall(raw json.RawMessage) string {
	var tc struct {
		Title      string `json:"title"`
		ToolCallID string `json:"toolCallId"`
	}
	if err := json.Unmarshal(raw, &tc); err == nil {
		if tc.Title != "" {
			return tc.Title
		}
		if tc.ToolCallID != "" {
			return fmt.Sprintf("tool call %s", tc.ToolCallID)
		}
	}
	return "tool call"
}

// Synthesized PermissionOption IDs for Claude Code native turns, whose own
// wire protocol has no options list -- just a tool name/input to allow or
// deny (see claudeDeciderAdapter).
const (
	claudeOptionAllow = "allow"
	claudeOptionDeny  = "deny"
)

// claudeFixedDenyMessage is surfaced to the model as the reason for a
// human-denied tool use. A custom per-decision deny message is out of scope
// for this pass (see docs/plans/active/permission-prompts.md's Decisions).
const claudeFixedDenyMessage = "denied by human reviewer"

// claudeDeciderAdapter adapts a PermissionDecider to claudecode.PermissionPolicy.
// Claude Code's can_use_tool request has no options list, only a tool
// name/input to allow or deny, so this synthesizes the two-option shape
// PermissionDecider expects and translates the chosen option back into the
// real (allow, updatedInput, denyMessage, updatedPermissions, interrupt, err)
// tuple Claude Code's protocol expects. updatedPermissions/interrupt are
// always the zero value (nil / false): letting a human edit permission
// rules or interrupt the turn from this UI is out of scope for this pass
// (see docs/plans/completed/permission-prompts.md's Decisions) -- a plain
// allow/deny is all PermissionDecider's two-option shape can express.
type claudeDeciderAdapter struct {
	decider PermissionDecider
}

func (a claudeDeciderAdapter) Decide(ctx context.Context, req claudecode.CanUseToolRequest) (bool, map[string]any, string, []claudecode.PermissionUpdate, bool, error) {
	opts := []PermissionOption{
		{ID: claudeOptionAllow, Label: "Allow", Kind: "allow_once"},
		{ID: claudeOptionDeny, Label: "Deny", Kind: "reject_once"},
	}
	summary := fmt.Sprintf("run %s", req.ToolName)

	optionID, err := a.decider.Decide(ctx, summary, opts)
	if err != nil {
		return false, nil, "", nil, false, err
	}
	if optionID == claudeOptionAllow {
		return true, req.Input, "", nil, false, nil
	}
	return false, nil, claudeFixedDenyMessage, nil, false, nil
}
