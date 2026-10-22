package wsapi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spacingmind/smind/internal/forge"
	"github.com/spacingmind/smind/internal/ghctx"
	"github.com/spacingmind/smind/internal/workspace"
)

// githubHolder lets SetGithubClient swap the gh client (tests inject a stub
// runner) after the handler table is built.
type githubHolder struct{ c *ghctx.Client }

// SetGithubClient replaces the gh client forge.search uses for forge "github". Like SetManagedBy it
// must be called before the API serves any connection.
func (a *API) SetGithubClient(c *ghctx.Client) { a.github.c = c }

// forgeSearchResult is forge.search's result: ghctx's search result tagged
// with the forge that answered.
type forgeSearchResult struct {
	Forge string `json:"forge"`
	forge.SearchResult
}

// handleForgeSearch serves forge.search (ADR-0038 §4, forge-neutral wire names
// since the 2026-10-22 amendment): the open PRs or issues of the task
// worktree's repo on `forge` (only "github" in v1; omitted means github),
// optionally filtered by query (a number or a github.com URL resolves that one
// item). The scope is exactly one of taskId (the task's worktree) or
// workspaceId (the workspace checkout, for the New Task dialog). It reads
// through the daemon host's own gh login and never takes a repo or path from
// the client.
func handleForgeSearch(wm *workspace.Manager, h *githubHolder) handlerFunc {
	return func(ctx context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			Forge       string `json:"forge"`
			TaskID      *int64 `json:"taskId"`
			WorkspaceID *int64 `json:"workspaceId"`
			Kind        string `json:"kind"`
			Query       string `json:"query"`
			Limit       int    `json:"limit"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("forge.search: invalid params: %w", err)
		}
		if p.Forge != "" && p.Forge != "github" {
			return nil, fmt.Errorf("forge.search: unsupported forge %q (only \"github\")", p.Forge)
		}
		if p.Kind != "pr" && p.Kind != "issue" {
			return nil, fmt.Errorf("forge.search: kind must be \"pr\" or \"issue\"")
		}
		if (p.TaskID == nil) == (p.WorkspaceID == nil) {
			return nil, fmt.Errorf("forge.search: invalid params: pass exactly one of taskId or workspaceId")
		}
		var dir string
		if p.TaskID != nil {
			var err error
			if dir, err = taskWorktreePath(wm, *p.TaskID); err != nil {
				return nil, fmt.Errorf("forge.search: %w", err)
			}
		} else {
			// New Task dialog: no task exists yet, so the repo is the
			// workspace checkout's origin.
			w, err := wm.GetWorkspace(*p.WorkspaceID)
			if err != nil {
				return nil, fmt.Errorf("forge.search: %w", err)
			}
			dir = w.Path
		}
		res, err := h.c.Search(ctx, dir, p.Kind, p.Query, p.Limit)
		if err != nil {
			return nil, err
		}
		return forgeSearchResult{Forge: "github", SearchResult: res}, nil
	}
}
