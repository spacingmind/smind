package acp

import (
	"context"
	"encoding/json"
	"fmt"
)

// PermissionOptionKind categorizes a PermissionOption, per ACP's
// PermissionOptionKind.
type PermissionOptionKind string

const (
	PermissionAllowOnce    PermissionOptionKind = "allow_once"
	PermissionAllowAlways  PermissionOptionKind = "allow_always"
	PermissionRejectOnce   PermissionOptionKind = "reject_once"
	PermissionRejectAlways PermissionOptionKind = "reject_always"
)

// PermissionOption is one choice offered to a RequestPermissionParams,
// per ACP's PermissionOption.
type PermissionOption struct {
	OptionID string               `json:"optionId"`
	Name     string               `json:"name"`
	Kind     PermissionOptionKind `json:"kind"`
}

// RequestPermissionParams is the agent-initiated session/request_permission
// request: it needs authorization before running toolCall, and offers
// options to choose from. ToolCall is kept as raw JSON (ACP's
// ToolCallUpdate) since deciding which option to pick rarely needs more
// than its name/kind, already surfaced via Options.
type RequestPermissionParams struct {
	SessionID string             `json:"sessionId"`
	ToolCall  json.RawMessage    `json:"toolCall"`
	Options   []PermissionOption `json:"options"`
}

type requestPermissionResult struct {
	Outcome permissionOutcome `json:"outcome"`
}

type permissionOutcome struct {
	Outcome  string `json:"outcome"`
	OptionID string `json:"optionId,omitempty"`
}

// PermissionPolicy decides which PermissionOption to select when the agent
// calls session/request_permission. Implementations that need to prompt a
// human (a future UI) plug in here instead of AutoApprovePolicy/
// AutoDenyPolicy.
type PermissionPolicy interface {
	Decide(ctx context.Context, req RequestPermissionParams) (optionID string, err error)
}

// AutoApprovePolicy always selects the first allow_once or allow_always
// option offered. It's the default PermissionPolicy: with no UI yet to ask
// a human, auto-approving is what makes it possible to actually exercise
// this package's ACP pipeline end-to-end (e.g. against real GLM) instead of
// every tool call stalling forever. Callers wiring this into anything
// resembling production use should pass WithPermissionPolicy(AutoDenyPolicy{})
// (or a future human-in-the-loop policy) explicitly.
type AutoApprovePolicy struct{}

func (AutoApprovePolicy) Decide(_ context.Context, req RequestPermissionParams) (string, error) {
	for _, opt := range req.Options {
		if opt.Kind == PermissionAllowOnce || opt.Kind == PermissionAllowAlways {
			return opt.OptionID, nil
		}
	}
	return "", fmt.Errorf("acp: no allow option offered: %+v", req.Options)
}

// AutoDenyPolicy always selects the first reject_once or reject_always
// option offered. It's the safer choice for anything resembling production
// use, since it never lets the agent act on the host without a human
// actually deciding.
type AutoDenyPolicy struct{}

func (AutoDenyPolicy) Decide(_ context.Context, req RequestPermissionParams) (string, error) {
	for _, opt := range req.Options {
		if opt.Kind == PermissionRejectOnce || opt.Kind == PermissionRejectAlways {
			return opt.OptionID, nil
		}
	}
	return "", fmt.Errorf("acp: no reject option offered: %+v", req.Options)
}

func (c *Client) handleRequestPermission(ctx context.Context, raw json.RawMessage) (any, *RPCError) {
	var req RequestPermissionParams
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, &RPCError{Code: ErrCodeInvalidParams, Message: "session/request_permission: " + err.Error()}
	}

	optionID, err := c.policy.Decide(ctx, req)
	if err != nil {
		return nil, &RPCError{Code: ErrCodeInternalError, Message: "session/request_permission: " + err.Error()}
	}

	return requestPermissionResult{Outcome: permissionOutcome{Outcome: "selected", OptionID: optionID}}, nil
}
