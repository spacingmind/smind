package codex

import (
	"context"
	"encoding/json"
)

// CommandExecutionApprovalRequest is the agent-initiated
// item/commandExecution/requestApproval request: it needs authorization
// before running Command. Codex's own schema
// (refs/codex/codex-rs/app-server-protocol/src/protocol/v2/item.rs) carries
// more structured detail than this package's PermissionPolicy needs to
// decide with; only the fields useful for a human-readable summary are
// decoded here, everything else stays implicit in Raw.
type CommandExecutionApprovalRequest struct {
	Command string          `json:"command"`
	Cwd     string          `json:"cwd"`
	Raw     json.RawMessage `json:"-"`
}

// FileChangeApprovalRequest is the agent-initiated
// item/fileChange/requestApproval request.
type FileChangeApprovalRequest struct {
	Reason string          `json:"reason,omitempty"`
	Raw    json.RawMessage `json:"-"`
}

// PermissionPolicy decides whether to approve a command-execution or
// file-change request the agent raises mid-turn. Unlike internal/acp's
// PermissionPolicy (which picks from an agent-offered options list), Codex's
// wire protocol offers no options list here -- just a request to approve or
// decline (this package implements only the "accept"/"decline" minimal
// subset of Codex's real decision enums; see
// docs/plans/active/provider-codex.md's Decisions for why) -- so this
// interface is a straight accept/reject decision per request kind, closer
// in shape to github.com/spacingmind/claude-agent-sdk-go's
// PermissionPolicy than to internal/acp's.
type PermissionPolicy interface {
	DecideCommandExecution(ctx context.Context, req CommandExecutionApprovalRequest) (accept bool, err error)
	DecideFileChange(ctx context.Context, req FileChangeApprovalRequest) (accept bool, err error)
}

// AutoApprovePolicy always accepts. It's the default PermissionPolicy for
// the same reason internal/acp.AutoApprovePolicy is: with no UI yet to ask
// a human, auto-approving is what makes it possible to exercise this
// package's pipeline end-to-end instead of every command/file-change
// request stalling forever. Callers wiring this into anything resembling
// production use should pass AutoDenyPolicy (or a future human-in-the-loop
// policy) explicitly.
type AutoApprovePolicy struct{}

func (AutoApprovePolicy) DecideCommandExecution(_ context.Context, _ CommandExecutionApprovalRequest) (bool, error) {
	return true, nil
}

func (AutoApprovePolicy) DecideFileChange(_ context.Context, _ FileChangeApprovalRequest) (bool, error) {
	return true, nil
}

// AutoDenyPolicy always declines -- the safer choice for anything
// resembling production use, since it never lets the agent act on the host
// without a human actually deciding.
type AutoDenyPolicy struct{}

func (AutoDenyPolicy) DecideCommandExecution(_ context.Context, _ CommandExecutionApprovalRequest) (bool, error) {
	return false, nil
}

func (AutoDenyPolicy) DecideFileChange(_ context.Context, _ FileChangeApprovalRequest) (bool, error) {
	return false, nil
}

// Wire values for CommandExecutionApprovalDecision/FileChangeApprovalDecision
// (both #[serde(rename_all = "camelCase")] unit variants, confirmed against
// refs/codex/codex-rs/app-server-protocol/src/protocol/v2/item.rs) -- only
// the two this package implements; see PermissionPolicy's doc comment.
const (
	decisionAccept  = "accept"
	decisionDecline = "decline"
)

type approvalDecisionResult struct {
	Decision string `json:"decision"`
}

func (c *Client) handleCommandExecutionApproval(ctx context.Context, raw json.RawMessage) (any, *RPCError) {
	var req CommandExecutionApprovalRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, &RPCError{Code: ErrCodeInternalError, Message: "item/commandExecution/requestApproval: " + err.Error()}
	}
	req.Raw = raw

	accept, err := c.policy.DecideCommandExecution(ctx, req)
	if err != nil {
		return nil, &RPCError{Code: ErrCodeInternalError, Message: "item/commandExecution/requestApproval: " + err.Error()}
	}
	return approvalDecisionResult{Decision: decisionFor(accept)}, nil
}

func (c *Client) handleFileChangeApproval(ctx context.Context, raw json.RawMessage) (any, *RPCError) {
	var req FileChangeApprovalRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, &RPCError{Code: ErrCodeInternalError, Message: "item/fileChange/requestApproval: " + err.Error()}
	}
	req.Raw = raw

	accept, err := c.policy.DecideFileChange(ctx, req)
	if err != nil {
		return nil, &RPCError{Code: ErrCodeInternalError, Message: "item/fileChange/requestApproval: " + err.Error()}
	}
	return approvalDecisionResult{Decision: decisionFor(accept)}, nil
}

func decisionFor(accept bool) string {
	if accept {
		return decisionAccept
	}
	return decisionDecline
}
