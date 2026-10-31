package wsapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/fleet/serve"
	"github.com/spacingmind/smind/internal/store"
)

// FleetWorkspaces is the workspace data the fleet read methods need;
// *workspace.Manager satisfies it.
type FleetWorkspaces interface {
	ListWorkspaces() ([]store.Workspace, error)
	DefaultBase(workspaceID int64) (ref string, detected bool, err error)
}

// fleetSource adapts the store and the workspace manager to serve.Source.
type fleetSource struct {
	db *store.Store
	ws FleetWorkspaces
}

// gitRemoteTimeout bounds the `git remote get-url` a workspace listing runs.
const gitRemoteTimeout = 3 * time.Second

func (s *fleetSource) ListWorkspaces() ([]serve.WorkspaceInfo, error) {
	list, err := s.ws.ListWorkspaces()
	if err != nil {
		return nil, err
	}
	out := make([]serve.WorkspaceInfo, 0, len(list))
	for _, w := range list {
		info := serve.WorkspaceInfo{ID: w.ID, Title: w.Title, RepoURL: fleetOriginURL(w.Path)}
		if ref, _, err := s.ws.DefaultBase(w.ID); err == nil {
			info.DefaultBranch = strings.TrimPrefix(ref, "origin/")
		}
		out = append(out, info)
	}
	return out, nil
}

// fleetOriginURL is the origin remote of the repo at path, "" on any error.
func fleetOriginURL(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), gitRemoteTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-c", "core.fsmonitor=false", "-C", path, "remote", "get-url", "--", "origin").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (s *fleetSource) ListTasks(workspaceID int64) ([]store.Task, error) {
	all, err := s.db.ListTasksByWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	out := all[:0:0]
	for _, t := range all {
		if t.ArchivedAt == nil {
			out = append(out, t)
		}
	}
	return out, nil
}

func (s *fleetSource) GetTask(id int64) (store.Task, error) {
	t, err := s.db.GetTask(id)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Task{}, fmt.Errorf("task %d: %w", id, serve.ErrNotFound)
	}
	if err != nil {
		return store.Task{}, err
	}
	if t.Kind == store.TaskKindSystem {
		return store.Task{}, fmt.Errorf("task %d: %w", id, serve.ErrNotFound)
	}
	return t, nil
}

func (s *fleetSource) TaskOrigin(taskID int64) (string, string, error) {
	host, trace, err := s.db.TaskOrigin(taskID)
	if errors.Is(err, store.ErrFleetNotFound) {
		return "", "", fmt.Errorf("task %d origin: %w", taskID, serve.ErrNotFound)
	}
	return host, trace, err
}

func (s *fleetSource) GetRun(runID string) (store.Run, error) {
	r, err := s.db.GetRun(runID)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Run{}, fmt.Errorf("run %q: %w", runID, serve.ErrNotFound)
	}
	return r, err
}

func (s *fleetSource) LatestRun(taskID int64) (store.Run, bool, error) {
	runs, err := s.db.ListRunsByTask(taskID, 1)
	if err != nil || len(runs) == 0 {
		return store.Run{}, false, err
	}
	return runs[0], true, nil
}

func (s *fleetSource) RunCostUSD(runID string) (*float64, error) {
	u, err := s.db.GetRunUsage(runID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return u.CostUSD, nil
}

func (s *fleetSource) RunEvents(runID string, afterSeq int64, limit int) ([]store.RunEvent, error) {
	return s.db.ListRunEventsAfterSeq(runID, afterSeq, limit)
}

// readHandler serves one fleet read method for an approved host principal.
func (f *fleetHost) readHandler(method string) handlerFunc {
	return func(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		pr := rc.conn.hostPrincipal()
		if pr == nil || pr.kind != "approved" {
			return nil, forbidden("not available to hosts")
		}
		res, err := f.read.Handle(ctx, pr.hostID, method, raw)
		var rf *serve.Refusal
		if errors.As(err, &rf) {
			return nil, &rpcCodeError{code: rf.Code, msg: rf.Msg, err: err}
		}
		return res, err
	}
}
