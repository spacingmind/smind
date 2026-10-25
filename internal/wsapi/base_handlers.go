package wsapi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spacingmind/smind/internal/workspace"
)

// defaultBaseResult is the result of workspace.defaultBase and
// workspace.setDefaultBase (ADR-0039 section 2): ref is the effective default
// base branch, detected is true when it was auto-detected rather than the
// user's override, and override is the stored override ("" = none).
type defaultBaseResult struct {
	Ref      string `json:"ref"`
	Detected bool   `json:"detected"`
	Override string `json:"override"`
}

func workspaceDefaultBaseResult(wm *workspace.Manager, workspaceID int64) (defaultBaseResult, error) {
	ref, detected, err := wm.DefaultBase(workspaceID)
	if err != nil {
		return defaultBaseResult{}, err
	}
	res := defaultBaseResult{Ref: ref, Detected: detected}
	if !detected {
		res.Override = ref
	}
	return res, nil
}

// handleWorkspaceDefaultBase returns the workspace's default base branch.
func handleWorkspaceDefaultBase(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			WorkspaceID int64 `json:"workspaceId"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("workspace.defaultBase: invalid params: %w", err)
		}
		res, err := workspaceDefaultBaseResult(wm, p.WorkspaceID)
		if err != nil {
			return nil, fmt.Errorf("workspace.defaultBase: %w", err)
		}
		return res, nil
	}
}

// handleWorkspaceSetDefaultBase stores (or, for an empty branch, clears) the
// workspace's default-base override and returns the resulting default.
func handleWorkspaceSetDefaultBase(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			WorkspaceID int64  `json:"workspaceId"`
			Branch      string `json:"branch"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("workspace.setDefaultBase: invalid params: %w", err)
		}
		if _, err := wm.SetDefaultBase(p.WorkspaceID, p.Branch); err != nil {
			return nil, fmt.Errorf("workspace.setDefaultBase: %w", err)
		}
		res, err := workspaceDefaultBaseResult(wm, p.WorkspaceID)
		if err != nil {
			return nil, fmt.Errorf("workspace.setDefaultBase: %w", err)
		}
		return res, nil
	}
}

// handleTaskSetBase recomputes and stores a task's base (base_ref + the
// merge-base sha) from baseRef and returns the updated task.
func handleTaskSetBase(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			TaskID  int64  `json:"taskId"`
			BaseRef string `json:"baseRef"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("task.setBase: invalid params: %w", err)
		}
		t, err := wm.SetTaskBase(p.TaskID, p.BaseRef)
		if err != nil {
			return nil, fmt.Errorf("task.setBase: %w", err)
		}
		return t, nil
	}
}
