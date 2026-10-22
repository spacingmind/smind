package taskrunner

import (
	"context"
	"encoding/json"
	"fmt"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
	"github.com/spacingmind/smind/internal/acp"
	"github.com/spacingmind/smind/internal/codex"
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
// any provider, blocking until an answer is available -- this is
// precisely what "the agent is waiting for a human" means, so a slow or
// never-returning Decide call is expected, not a bug (see RunPrompt's ctx
// cancellation for how a caller aborts it). Implementations that need to
// know which run's event stream to push the request onto (e.g. a
// human-in-the-loop UI) construct a PermissionDecider per call, closing
// over that context; internal/runs.Registry is today's only such
// implementation.
//
// smind never decides on its own that a request is safe (ADR-0019): what
// reaches a PermissionDecider is exactly what the provider's own permission
// mode chose to escalate.
//
// When no PermissionDecider is supplied to RunPrompt, each provider falls
// back to its own Runner-level acp.PermissionPolicy/claudecode.PermissionPolicy
// default (see WithACPPermissionPolicy/WithClaudeCodePermissionPolicy),
// which in turn default to acp.AutoApprovePolicy/claudecode.AutoDenyPolicy
// if never set at all.
type PermissionDecider interface {
	Decide(ctx context.Context, summary string, options []PermissionOption) (optionID string, err error)
}

// acpDeciderAdapter adapts a PermissionDecider to acp.PermissionPolicy: ACP's
// own request/response already is an options-list-in, optionId-out shape,
// so this is a direct translation with no synthesized options needed.
type acpDeciderAdapter struct {
	decider PermissionDecider
	// cwd is the run's worktree, used to resolve a relative write path when
	// summarizing a file write; empty falls back to the process cwd.
	cwd string
}

func (a acpDeciderAdapter) Decide(ctx context.Context, req acp.RequestPermissionParams) (string, error) {
	opts := make([]PermissionOption, len(req.Options))
	for i, o := range req.Options {
		opts[i] = PermissionOption{ID: o.OptionID, Label: o.Name, Kind: string(o.Kind)}
	}
	summary := summarizeACPToolCallIn(a.cwd, req.ToolCall)
	// detail is the structured counterpart of the summary's write
	// description (path, sizes, overwrite flags, capped diff/preview).
	detail := BuildPermissionDetail(a.cwd, req.ToolCall)
	if dd, ok := a.decider.(DetailedPermissionDecider); ok {
		return dd.DecideDetailed(ctx, summary, detail, opts)
	}
	return a.decider.Decide(ctx, summary, opts)
}

// DetailedPermissionDecider is an optional extension of PermissionDecider: a
// decider that implements it also receives the PermissionDetail derived from
// the tool call (nil when none). Only the ACP adapter supplies detail; the
// Claude/Codex adapters call plain Decide.
type DetailedPermissionDecider interface {
	DecideDetailed(ctx context.Context, summary string, detail *PermissionDetail, options []PermissionOption) (string, error)
}

// summarizeACPToolCall builds a human-readable summary of the tool call a
// session/request_permission is asking about, from ACP's raw ToolCallUpdate
// JSON. It prefers Title (the field agents populate for exactly this
// purpose, per ACP's ToolCallUpdate); ToolCallID is the fallback for an
// agent that omits it, so a request is never presented with no context at
// all.
func summarizeACPToolCall(raw json.RawMessage) string {
	return summarizeACPToolCallIn("", raw)
}

// summarizeACPToolCallIn is summarizeACPToolCall plus, for a file write,
// its size and old -> new line counts (see describeWrite). The write detail
// comes from an ACP diff content block when the agent sent one, else from a
// rawInput carrying the path and new content.
func summarizeACPToolCallIn(cwd string, raw json.RawMessage) string {
	var tc struct {
		Title      string `json:"title"`
		ToolCallID string `json:"toolCallId"`
		Content    []struct {
			Type    string  `json:"type"`
			Path    string  `json:"path"`
			OldText *string `json:"oldText"`
			NewText string  `json:"newText"`
		} `json:"content"`
		RawInput map[string]any `json:"rawInput"`
	}
	if err := json.Unmarshal(raw, &tc); err != nil {
		return "tool call"
	}
	summary := ""
	switch {
	case tc.Title != "":
		summary = tc.Title
	case tc.ToolCallID != "":
		summary = fmt.Sprintf("tool call %s", tc.ToolCallID)
	default:
		return "tool call"
	}
	for _, c := range tc.Content {
		if c.Type == "diff" {
			old := ""
			if c.OldText != nil {
				old = *c.OldText
			}
			return fmt.Sprintf("%s (%s)", summary, describeWrite(cwd, c.Path, c.NewText, c.OldText != nil, old))
		}
	}
	if content, ok := tc.RawInput["content"].(string); ok {
		return fmt.Sprintf("%s (%s)", summary, describeWrite(cwd, writeInputPath(tc.RawInput), content, false, ""))
	}
	return summary
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
	// cwd is the run's worktree, used to resolve a relative Write path.
	cwd string
}

func (a claudeDeciderAdapter) Decide(ctx context.Context, req claudecode.CanUseToolRequest) (bool, map[string]any, string, []claudecode.PermissionUpdate, bool, error) {
	opts := []PermissionOption{
		{ID: claudeOptionAllow, Label: "Allow", Kind: "allow_once"},
		{ID: claudeOptionDeny, Label: "Deny", Kind: "reject_once"},
	}
	summary := fmt.Sprintf("run %s", req.ToolName)
	if content, ok := req.Input["content"].(string); ok && req.ToolName == "Write" {
		path := writeInputPath(req.Input)
		summary = fmt.Sprintf("run Write %s (%s)", path, describeWrite(a.cwd, path, content, false, ""))
	}

	var optionID string
	var err error
	if detail := claudeBashDetail(req); detail != nil {
		// The command is what a human approving a Bash call must see; the
		// summary alone says only "run Bash".
		if dd, ok := a.decider.(DetailedPermissionDecider); ok {
			optionID, err = dd.DecideDetailed(ctx, summary, detail, opts)
		} else {
			optionID, err = a.decider.Decide(ctx, summary, opts)
		}
	} else {
		optionID, err = a.decider.Decide(ctx, summary, opts)
	}
	if err != nil {
		return false, nil, "", nil, false, err
	}
	if optionID == claudeOptionAllow {
		return true, req.Input, "", nil, false, nil
	}
	return false, nil, claudeFixedDenyMessage, nil, false, nil
}

// claudeBashDetail is the structured detail of a Bash can_use_tool request:
// the same execute shape ACP execute tool calls produce (command, capped), so
// the CLI, MCP and web render it the way they do for GLM. nil for any other
// tool or a request without a command string.
func claudeBashDetail(req claudecode.CanUseToolRequest) *PermissionDetail {
	cmd, ok := req.Input["command"].(string)
	if !ok || req.ToolName != "Bash" {
		return nil
	}
	d := &PermissionDetail{Kind: "execute"}
	d.Command, d.Truncated = capBytes(cmd, detailCommandMaxBytes)
	return d
}

// Synthesized PermissionOption IDs for Codex-native turns, whose wire
// protocol (like Claude Code's) has no options list -- just a request to
// accept or decline (see codex.PermissionPolicy's doc comment).
const (
	codexOptionAccept  = "accept"
	codexOptionDecline = "decline"
)

// codexDeciderAdapter adapts a PermissionDecider to codex.PermissionPolicy.
// Codex's two approval-request kinds (command execution, file change) each
// have no options list, only a request to accept or decline, so this
// synthesizes the same two-option shape claudeDeciderAdapter does for
// Claude Code's can_use_tool request.
type codexDeciderAdapter struct {
	decider PermissionDecider
}

func (a codexDeciderAdapter) DecideCommandExecution(ctx context.Context, req codex.CommandExecutionApprovalRequest) (bool, error) {
	return a.decide(ctx, fmt.Sprintf("run %s", req.Command))
}

func (a codexDeciderAdapter) DecideFileChange(ctx context.Context, req codex.FileChangeApprovalRequest) (bool, error) {
	summary := "change files"
	if req.Reason != "" {
		summary = req.Reason
	}
	return a.decide(ctx, summary)
}

func (a codexDeciderAdapter) decide(ctx context.Context, summary string) (bool, error) {
	opts := []PermissionOption{
		{ID: codexOptionAccept, Label: "Accept", Kind: "allow_once"},
		{ID: codexOptionDecline, Label: "Decline", Kind: "reject_once"},
	}
	optionID, err := a.decider.Decide(ctx, summary, opts)
	if err != nil {
		return false, err
	}
	return optionID == codexOptionAccept, nil
}
