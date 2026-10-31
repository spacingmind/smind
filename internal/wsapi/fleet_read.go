package wsapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
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

	now   func() time.Time // nil -> time.Now; tests advance it
	mu    sync.Mutex
	cache map[int64]fleetWsMeta
}

// fleetWsMeta is a workspace's origin URL and default branch, cached
// because resolving them shells out to git.
type fleetWsMeta struct {
	repoURL       string
	defaultBranch string
	at            time.Time
}

// fleetMetaTTL is how long a cached fleetWsMeta stays fresh.
const fleetMetaTTL = 30 * time.Second

func (s *fleetSource) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// meta returns the cached origin metadata of w, recomputing it once stale.
func (s *fleetSource) meta(w store.Workspace) fleetWsMeta {
	now := s.clock()
	s.mu.Lock()
	m, ok := s.cache[w.ID]
	s.mu.Unlock()
	if ok && now.Sub(m.at) < fleetMetaTTL {
		return m
	}
	m = fleetWsMeta{repoURL: fleetOriginURL(w.Path), at: now}
	if ref, _, err := s.ws.DefaultBase(w.ID); err == nil {
		m.defaultBranch = strings.TrimPrefix(ref, "origin/")
	}
	s.mu.Lock()
	if s.cache == nil {
		s.cache = map[int64]fleetWsMeta{}
	}
	s.cache[w.ID] = m
	s.mu.Unlock()
	return m
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
		m := s.meta(w)
		out = append(out, serve.WorkspaceInfo{ID: w.ID, Title: w.Title, RepoURL: m.repoURL, DefaultBranch: m.defaultBranch})
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
