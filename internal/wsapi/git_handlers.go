package wsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/gitstate"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
)

// taskGitPayload is the payload of task.git events: {taskId, git} (ADR-0039 §3).
type taskGitPayload struct {
	TaskID int64            `json:"taskId"`
	Git    gitstate.TaskGit `json:"git"`
}

// taskGitListResult is the result of task.gitList.
type taskGitListResult struct {
	Items []gitstate.TaskGit `json:"items"`
}

// gitSource adapts *workspace.Manager to gitstate.Source.
type gitSource struct{ wm *workspace.Manager }

func (g gitSource) GetTask(id int64) (store.Task, error) { return g.wm.GetTask(id) }

func (g gitSource) GetWorkspace(id int64) (store.Workspace, error) { return g.wm.GetWorkspace(id) }

func (g gitSource) ListTasks(workspaceID int64) ([]store.Task, error) {
	return g.wm.ListTasks(workspaceID)
}

func (g gitSource) TaskDiffStat(id int64) (int, int, int, error) { return g.wm.TaskDiffStat(id) }

// focusRegistry holds the one focused task per connection (task.gitFocus); a
// connection's entry is dropped when it closes.
type focusRegistry struct {
	mu sync.Mutex
	by map[*conn]int64
}

func newFocusRegistry() *focusRegistry { return &focusRegistry{by: map[*conn]int64{}} }

func (f *focusRegistry) set(c *conn, taskID int64) {
	f.mu.Lock()
	f.by[c] = taskID
	f.mu.Unlock()
}

func (f *focusRegistry) drop(c *conn) {
	f.mu.Lock()
	delete(f.by, c)
	f.mu.Unlock()
}

// ids returns the distinct focused task ids, sorted.
func (f *focusRegistry) ids() []int64 {
	f.mu.Lock()
	seen := map[int64]bool{}
	for _, id := range f.by {
		seen[id] = true
	}
	f.mu.Unlock()
	out := make([]int64, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// gitInterest is gitstate.Interest backed by the event bus (any task.git
// subscriber) and the per-connection focus set.
type gitInterest struct {
	bus   *eventBus
	focus *focusRegistry
}

func (g gitInterest) Watched() bool    { return g.bus.HasSubscribers(TopicTaskGit) }
func (g gitInterest) Focused() []int64 { return g.focus.ids() }

// gitOptions overrides the production wiring of the git state service (tests).
type gitOptions struct {
	Runner gitstate.Runner
	Tick   <-chan time.Time
	After  func(time.Duration, func()) func()
	Now    func() time.Time
}

func newGitService(wm *workspace.Manager, bus *eventBus, focus *focusRegistry, o gitOptions) *gitstate.Service {
	return gitstate.New(gitstate.Options{
		Source: gitSource{wm}, Runner: o.Runner, Now: o.Now, After: o.After, Tick: o.Tick,
		Interest: gitInterest{bus: bus, focus: focus},
		Publisher: func(taskID int64, g gitstate.TaskGit) {
			bus.Publish(Event{Topic: TopicTaskGit, Payload: taskGitPayload{TaskID: taskID, Git: g}})
		},
	})
}

// gitHandlers returns the task.git* RPCs. Reads only ever run local git.
func gitHandlers(svc *gitstate.Service, focus *focusRegistry) map[string]handlerFunc {
	return map[string]handlerFunc{
		"task.git": func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
			var p struct {
				TaskID int64 `json:"taskId"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, fmt.Errorf("task.git: invalid params: %w", err)
			}
			g, err := svc.Get(p.TaskID)
			if err != nil {
				return nil, fmt.Errorf("task.git: %w", err)
			}
			return g, nil
		},
		"task.gitList": func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
			var p struct {
				WorkspaceID int64 `json:"workspaceId"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, fmt.Errorf("task.gitList: invalid params: %w", err)
			}
			items, err := svc.List(p.WorkspaceID)
			if err != nil {
				return nil, fmt.Errorf("task.gitList: %w", err)
			}
			return taskGitListResult{Items: items}, nil
		},
		"task.gitFocus": func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
			var p struct {
				TaskID *int64 `json:"taskId"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, fmt.Errorf("task.gitFocus: invalid params: %w", err)
			}
			if p.TaskID == nil {
				focus.drop(rc.conn)
			} else {
				focus.set(rc.conn, *p.TaskID)
			}
			return struct{}{}, nil
		},
	}
}

// invalidateAfter wraps a mutating task RPC (params carry taskId) so a
// successful call recomputes that task's git state.
func invalidateAfter(h handlerFunc, svc *gitstate.Service) handlerFunc {
	return func(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		res, err := h(ctx, rc, raw)
		if err == nil {
			var p struct {
				TaskID int64 `json:"taskId"`
			}
			if json.Unmarshal(raw, &p) == nil && p.TaskID != 0 {
				svc.Invalidate(p.TaskID)
			}
		}
		return res, err
	}
}
