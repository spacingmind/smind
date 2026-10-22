package wsapi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spacingmind/smind/internal/ghctx"
	"github.com/spacingmind/smind/internal/workspace"
)

// githubHolder lets SetGithubClient swap the gh client (tests inject a stub
// runner) after the handler table is built.
type githubHolder struct{ c *ghctx.Client }

// SetGithubClient replaces the client github.search uses. Like SetManagedBy it
// must be called before the API serves any connection.
func (a *API) SetGithubClient(c *ghctx.Client) { a.github.c = c }

// handleGithubSearch serves github.search (ADR-0038 §4): the open PRs or
// issues of the task worktree's GitHub repo, optionally filtered by query (a
// number or a github.com URL resolves that one item). It reads through the
// daemon host's own gh login and never takes a repo from the client: the
// repo is the worktree's.
func handleGithubSearch(wm *workspace.Manager, h *githubHolder) handlerFunc {
	return func(ctx context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			TaskID int64  `json:"taskId"`
			Kind   string `json:"kind"`
			Query  string `json:"query"`
			Limit  int    `json:"limit"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("github.search: invalid params: %w", err)
		}
		if p.Kind != "pr" && p.Kind != "issue" {
			return nil, fmt.Errorf("github.search: kind must be \"pr\" or \"issue\"")
		}
		dir, err := taskWorktreePath(wm, p.TaskID)
		if err != nil {
			return nil, fmt.Errorf("github.search: %w", err)
		}
		res, err := h.c.Search(ctx, dir, p.Kind, p.Query, p.Limit)
		if err != nil {
			return nil, err
		}
		return res, nil
	}
}
