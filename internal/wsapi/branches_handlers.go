package wsapi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spacingmind/smind/internal/workspace"
)

// handleWorkspaceBranches serves workspace.branches (ADR-0039 section 7): the
// workspace's local and origin/* branches for the New Task "Start from"
// picker, filtered by query, at most limit (default 30, max 50) per page,
// with ahead/behind against the default base for the returned page only.
// Local git only: no fetch, no forge call.
func handleWorkspaceBranches(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			WorkspaceID int64  `json:"workspaceId"`
			Query       string `json:"query"`
			Limit       int    `json:"limit"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("workspace.branches: invalid params: %w", err)
		}
		res, err := wm.Branches(p.WorkspaceID, p.Query, p.Limit)
		if err != nil {
			return nil, fmt.Errorf("workspace.branches: %w", err)
		}
		return res, nil
	}
}
