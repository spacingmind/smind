package taskrunner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
// either provider, blocking until an answer is available -- this is
// precisely what "the agent is waiting for a human" means, so a slow or
// never-returning Decide call is expected, not a bug (see RunPrompt's ctx
// cancellation for how a caller aborts it). Implementations that need to
// know which run's event stream to push the request onto (e.g. a
// human-in-the-loop UI) construct a PermissionDecider per call, closing
// over that context; internal/runs.Registry is today's only such
// implementation, and the only one that inspects command at all (to decide
// whether ApprovalPolicyAutoSafe can auto-allow this request without ever
// asking a human -- see AllowlistedCommand).
//
// command is the literal shell command this request is asking to run, when
// the calling adapter can confirm one -- Claude Code's Bash tool
// (claudeDeciderAdapter, from its Input["command"]) and Codex's
// command-execution approval (codexDeciderAdapter, from its own Command
// field) both can; ACP's tool-call schema (acpDeciderAdapter) exposes no
// field this package has confirmed carries one, so it always passes "".
// command is empty for any request that isn't shaped like a shell command
// at all (a file-change approval, an ACP tool call of unknown kind, ...).
// A PermissionDecider must never treat a non-empty command as anything
// more than a hint -- ApprovalPolicyAutoSafe's AllowlistedCommand check is
// the only thing that should ever turn it into an auto-allow decision, and
// only for its own conservative allowlist.
//
// When no PermissionDecider is supplied to RunPrompt, behavior is
// unchanged from before this type existed: each provider falls back to its
// own Runner-level acp.PermissionPolicy/claudecode.PermissionPolicy default
// (see WithACPPermissionPolicy/WithClaudeCodePermissionPolicy), which in
// turn default to acp.AutoApprovePolicy/claudecode.AutoDenyPolicy if never
// set at all.
type PermissionDecider interface {
	Decide(ctx context.Context, summary, command string, options []PermissionOption) (optionID string, err error)
}

// acpDeciderAdapter adapts a PermissionDecider to acp.PermissionPolicy: ACP's
// own request/response already is an options-list-in, optionId-out shape,
// so this is a direct translation with no synthesized options needed.
type acpDeciderAdapter struct {
	decider PermissionDecider
	// worktreePath/approvalPolicy carry the run's auto-safe edit policy
	// down to this adapter -- see autoAllowACPFileEdit.
	worktreePath   string
	approvalPolicy ApprovalPolicy
}

func (a acpDeciderAdapter) Decide(ctx context.Context, req acp.RequestPermissionParams) (string, error) {
	opts := make([]PermissionOption, len(req.Options))
	for i, o := range req.Options {
		opts[i] = PermissionOption{ID: o.OptionID, Label: o.Name, Kind: string(o.Kind)}
	}

	// ApprovalPolicyAutoSafe's file-edit analog for ACP: claude-native
	// runs get the CLI's own acceptEdits mode for this exact purpose, but
	// ACP has no permission-mode concept -- every sensitive tool call
	// arrives as a session/request_permission. Without this, a headless
	// auto-safe GLM/Kimi run can never write a single file (every edit
	// times out to auto-deny), which is the ACP twin of the 2026-09-11
	// acceptEdits failure. Scoped to edits (kind edit/move/delete) whose
	// every reported location stays inside the task's own worktree --
	// see autoAllowACPFileEdit for why that boundary is the safe one.
	if a.approvalPolicy == ApprovalPolicyAutoSafe && autoAllowACPFileEdit(req.ToolCall, a.worktreePath) {
		if optionID, ok := firstOptionByKind(opts, "allow_once", "allow_always"); ok {
			return optionID, nil
		}
	}

	// command is always "" here: ACP's ToolCallUpdate (req.ToolCall) has no
	// field this package has confirmed reliably carries the literal command
	// being executed, unlike Claude Code's Input["command"] or Codex's
	// Command below. Passing "" means ApprovalPolicyAutoSafe's
	// AllowlistedCommand check can never match an ACP-driven (GLM/Kimi)
	// request, so it always falls back to a human decision for this
	// provider -- the correct, conservative behavior for a command this
	// package can't actually identify, not a gap to silently paper over
	// with a guessed schema.
	return a.decider.Decide(ctx, summarizeACPToolCall(req.ToolCall), "", opts)
}

// autoAllowACPFileEdit reports whether an ACP permission request is a
// file edit confined to the task's own worktree, making it safe to
// auto-allow under ApprovalPolicyAutoSafe: a write that can't escape the
// throwaway task worktree is no more dangerous than the local git
// commits that policy already allows, and the diff is human-reviewable
// afterward (task.diff). Kind execute (shell commands) is deliberately
// NOT covered here -- those requests keep going to the decider's
// AllowlistedCommand path, which can't identify an ACP command string
// anyway (see Decide above) and therefore stays manual. Anything
// unparsable or incomplete fails closed (false).
//
// The path is identified two ways, because real agents populate
// ToolCallUpdate inconsistently: the schema's structured fields first
// (kind edit/move/delete + locations[].path), then -- only when the
// agent filled in neither -- a conservative title fallback
// ("<verb> file: <path>", glm-acp-agent's actual shape when it omits
// locations). Either route's path may be relative: live glm-acp-agent
// traffic (2026-09-14) showed kind "edit" with locations[].path set to
// the plain relative path too, not an absolute one, despite the ACP
// schema not requiring that. A relative path's base is known regardless
// of route -- the session's own cwd is worktreePath (set via
// NewSession) -- so it's resolved against that before the same
// inside-worktree check. A title that names no recognizable file-edit
// verb, or any path (relative or absolute) that resolves outside the
// worktree, fails closed.
func autoAllowACPFileEdit(raw json.RawMessage, worktreePath string) bool {
	if worktreePath == "" {
		return false
	}
	var tc struct {
		Kind      string `json:"kind"`
		Title     string `json:"title"`
		Locations []struct {
			Path string `json:"path"`
		} `json:"locations"`
	}
	if err := json.Unmarshal(raw, &tc); err != nil {
		return false
	}

	var paths []string
	switch tc.Kind {
	case "edit", "move", "delete":
		for _, loc := range tc.Locations {
			paths = append(paths, loc.Path)
		}
	case "", "execute", "read", "search", "other", "think", "fetch":
		// Structured fields absent or not an edit kind: try the title
		// fallback only when the agent provided no kind at all -- a tool
		// call that *declared* a non-edit kind is not second-guessed from
		// its title.
		if tc.Kind == "" {
			if p, ok := fileEditPathFromTitle(tc.Title); ok {
				paths = append(paths, p)
			}
		}
	}
	if len(paths) == 0 {
		return false
	}

	root := filepath.Clean(worktreePath)
	rootSep := root + string(os.PathSeparator)
	for _, p := range paths {
		if p == "" {
			return false
		}
		if !filepath.IsAbs(p) {
			// filepath.Join cleans the result, so a traversal like
			// "../../etc/passwd" resolves to its real absolute location
			// before the containment check below, same as any other path.
			p = filepath.Join(root, p)
		}
		cleaned := filepath.Clean(p)
		// The root itself is rejected along with everything outside it: a
		// "location" equal to the worktree isn't a file this edit targets,
		// it's at best a malformed request -- fail closed.
		if cleaned == root || !strings.HasPrefix(cleaned+string(os.PathSeparator), rootSep) {
			return false
		}
	}
	return true
}

// fileEditPathFromTitle extracts the path (absolute or relative -- the
// caller resolves relative ones against the worktree root) from a
// file-edit permission title of the form "<verb> file: <path>" (verbs
// observed in the wild: glm-acp-agent's "Write file: docs/x.md").
func fileEditPathFromTitle(title string) (string, bool) {
	idx := strings.LastIndex(title, " file: ")
	if idx < 0 {
		return "", false
	}
	verb := title[:idx]
	switch verb {
	case "Write", "Edit", "Create", "Move", "Rename", "Delete", "Remove", "Overwrite":
	default:
		return "", false
	}
	return title[idx+len(" file: "):], true
}

// firstOptionByKind mirrors internal/runs's unexported helper of the same
// name, for this package's adapters' own auto-allow paths (the runs-side
// helper is not importable from here).
func firstOptionByKind(options []PermissionOption, kinds ...string) (string, bool) {
	for _, o := range options {
		for _, k := range kinds {
			if o.Kind == k {
				return o.ID, true
			}
		}
	}
	return "", false
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

	optionID, err := a.decider.Decide(ctx, summary, bashCommand(req), opts)
	if err != nil {
		return false, nil, "", nil, false, err
	}
	if optionID == claudeOptionAllow {
		return true, req.Input, "", nil, false, nil
	}
	return false, nil, claudeFixedDenyMessage, nil, false, nil
}

// bashCommand extracts the literal shell command from req, if req is a
// Bash tool-use request -- Claude Code's built-in Bash tool's Input schema
// is {"command": "...", ...}, confirmed against the fake CLI's own
// "streaming_and_permission"-style scenarios in taskrunner_test.go. Any
// other ToolName (Read, Edit, Write, a custom MCP tool, ...), or a Bash
// request whose Input for some reason doesn't carry a string "command"
// (shouldn't happen for a real Claude Code turn, but this is user-influenced
// wire data, not something to trust blindly), returns "" -- see
// PermissionDecider's doc comment on why an empty command is always safe
// (never auto-allowed).
func bashCommand(req claudecode.CanUseToolRequest) string {
	if req.ToolName != "Bash" {
		return ""
	}
	cmd, _ := req.Input["command"].(string)
	return cmd
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
	summary := fmt.Sprintf("run %s", req.Command)
	// req.Command is Codex's own decoded field for exactly this request
	// kind (unlike Claude Code's Input, there's no tool-name check needed
	// here -- a CommandExecutionApprovalRequest is always a shell command).
	return a.decide(ctx, summary, req.Command)
}

func (a codexDeciderAdapter) DecideFileChange(ctx context.Context, req codex.FileChangeApprovalRequest) (bool, error) {
	summary := "change files"
	if req.Reason != "" {
		summary = req.Reason
	}
	// A file-change approval carries no shell command at all -- "" here
	// means ApprovalPolicyAutoSafe can never auto-allow one, which is
	// correct: this pass's allowlist only ever covers read-only shell
	// verification commands, never a file modification.
	return a.decide(ctx, summary, "")
}

func (a codexDeciderAdapter) decide(ctx context.Context, summary, command string) (bool, error) {
	opts := []PermissionOption{
		{ID: codexOptionAccept, Label: "Accept", Kind: "allow_once"},
		{ID: codexOptionDecline, Label: "Decline", Kind: "reject_once"},
	}
	optionID, err := a.decider.Decide(ctx, summary, command, opts)
	if err != nil {
		return false, err
	}
	return optionID == codexOptionAccept, nil
}
