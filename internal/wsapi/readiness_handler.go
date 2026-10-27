package wsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spacingmind/smind/internal/readiness"
	"github.com/spacingmind/smind/internal/workspace"
)

// maxReadinessFindings caps workspace.readiness' result.
const maxReadinessFindings = 32

type workspaceReadinessResult struct {
	WorkspaceID int64               `json:"workspaceId"`
	Findings    []readiness.Finding `json:"findings"`
	CheckedAt   time.Time           `json:"checkedAt"`
}

// handleWorkspaceReadiness runs the static agent-readiness check on the
// workspace's own directory. Read-only; guarded like workspace.get (local and
// relay, refused to run-scoped agents because it is not in RunScopedMethods,
// and a plugin-never method).
func handleWorkspaceReadiness(wm *workspace.Manager) handlerFunc {
	return func(ctx context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			WorkspaceID int64 `json:"workspaceId"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("workspace.readiness: invalid params: %w", err)
		}
		ws, err := wm.GetWorkspace(p.WorkspaceID)
		if err != nil {
			return nil, err
		}
		fs := readiness.Check(ctx, ws.Path, readiness.Options{
			HasSetupScript: ws.HasSetupScript,
			TrackedEnv:     func(c context.Context) ([]string, error) { return workspace.TrackedEnvFiles(c, ws.Path) },
		})
		if len(fs) > maxReadinessFindings {
			fs = fs[:maxReadinessFindings]
		}
		return workspaceReadinessResult{WorkspaceID: ws.ID, Findings: fs, CheckedAt: time.Now().UTC()}, nil
	}
}
