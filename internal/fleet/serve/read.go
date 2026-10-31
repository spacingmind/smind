package serve

import (
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"unicode/utf8"

	"github.com/spacingmind/smind/internal/fleet/hello"
	"github.com/spacingmind/smind/internal/fleet/registry"
	"github.com/spacingmind/smind/internal/fleet/wire"
	"github.com/spacingmind/smind/internal/learning/redact"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// ReaderOptions configure NewReader. Zero OS/Arch/Redact take the runtime
// values and a redactor over HomeDirs.
type ReaderOptions struct {
	HostID, Version string
	Caps            []string
	OS, Arch        string
	HomeDirs        []string
	Redact          func(string) string
}

type reader struct {
	src Source
	o   ReaderOptions
}

// NewReader returns a Reader that answers the read methods from src. It
// copies only the fields the wire types name: no paths, credentials or
// budgets leave through it.
func NewReader(src Source, o ReaderOptions) Reader {
	if o.OS == "" {
		o.OS = runtime.GOOS
	}
	if o.Arch == "" {
		o.Arch = runtime.GOARCH
	}
	if o.Redact == nil {
		o.Redact = redact.New(nil, o.HomeDirs).String
	}
	return &reader{src: src, o: o}
}

func (r *reader) Info(g registry.Grant) (wire.Info, error) {
	return wire.Info{
		HostID:  r.o.HostID,
		Version: r.o.Version,
		FleetV:  hello.FleetV,
		Caps:    append([]string{}, r.o.Caps...),
		OS:      r.o.OS,
		Arch:    r.o.Arch,
		Limits: wire.Limits{
			Ops:            append([]string{}, g.Ops...),
			MaxLiveAgents:  g.MaxLiveAgents,
			MaxWallSeconds: g.MaxWallSeconds,
			MaxCostUSD:     g.MaxCostUSD,
		},
	}, nil
}

func (r *reader) readable(g registry.Grant) ([]WorkspaceInfo, error) {
	all, err := r.src.ListWorkspaces()
	if err != nil {
		return nil, err
	}
	var out []WorkspaceInfo
	for _, w := range all {
		if g.CanRead(w.ID) {
			out = append(out, w)
		}
	}
	return out, nil
}

func (r *reader) Workspaces(g registry.Grant) (wire.WorkspaceList, error) {
	ws, err := r.readable(g)
	if err != nil {
		return wire.WorkspaceList{}, err
	}
	out := make([]wire.Workspace, 0, len(ws))
	for _, w := range ws {
		out = append(out, wire.Workspace{
			ID:            w.ID,
			Title:         w.Title,
			RepoURL:       SafeRepoURL(w.RepoURL),
			DefaultBranch: w.DefaultBranch,
		})
	}
	return wire.WorkspaceList{Workspaces: out}, nil
}

func pageLimit(n, def int) (int, error) {
	switch {
	case n < 0:
		return 0, refuse(wire.CodeInvalid, "limit must not be negative")
	case n == 0:
		return def, nil
	case n > wire.MaxPageSize:
		return wire.MaxPageSize, nil
	}
	return n, nil
}

func (r *reader) TaskList(g registry.Grant, p wire.TaskListParams) (wire.TaskList, error) {
	limit, err := pageLimit(p.Limit, wire.DefaultPageSize)
	if err != nil {
		return wire.TaskList{}, err
	}
	ws, err := r.readable(g)
	if err != nil {
		return wire.TaskList{}, err
	}
	if p.WorkspaceID != 0 {
		if !g.CanRead(p.WorkspaceID) {
			return wire.TaskList{}, refuse(wire.CodeNotGranted, "not granted")
		}
		ws = slices.DeleteFunc(ws, func(w WorkspaceInfo) bool { return w.ID != p.WorkspaceID })
	}
	var tasks []store.Task
	for _, w := range ws {
		ts, err := r.src.ListTasks(w.ID)
		if err != nil {
			return wire.TaskList{}, err
		}
		for _, t := range ts {
			if t.ID > p.Cursor {
				tasks = append(tasks, t)
			}
		}
	}
	slices.SortFunc(tasks, func(a, b store.Task) int {
		switch {
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		}
		return 0
	})
	res := wire.TaskList{Tasks: []wire.Task{}}
	more := len(tasks) > limit
	if more {
		tasks = tasks[:limit]
	}
	for _, t := range tasks {
		res.Tasks = append(res.Tasks, r.toTask(t))
	}
	if more {
		res.NextCursor = tasks[len(tasks)-1].ID
	}
	return res, nil
}

func notFound(what string) error { return fmt.Errorf("%s: %w", what, ErrNotFound) }

// readableTask returns the task or an ErrNotFound error that is identical for
// absent and unreadable tasks.
func (r *reader) readableTask(g registry.Grant, id int64) (store.Task, error) {
	t, err := r.src.GetTask(id)
	if err != nil {
		if isNotFound(err) {
			return store.Task{}, notFound("task")
		}
		return store.Task{}, err
	}
	if !g.CanRead(t.WorkspaceID) {
		return store.Task{}, notFound("task")
	}
	return t, nil
}

func isNotFound(err error) bool { return err != nil && errors.Is(err, ErrNotFound) }

func (r *reader) TaskGet(g registry.Grant, p wire.TaskGetParams) (wire.Task, error) {
	t, err := r.readableTask(g, p.ID)
	if err != nil {
		return wire.Task{}, err
	}
	return r.toTask(t), nil
}

func (r *reader) toTask(t store.Task) wire.Task {
	out := wire.Task{
		ID:          t.ID,
		WorkspaceID: t.WorkspaceID,
		Title:       t.Title,
		State:       t.Status,
		CreatedAt:   t.CreatedAt.UTC(),
		UpdatedAt:   t.UpdatedAt.UTC(),
	}
	if t.Branch != nil {
		out.Branch = *t.Branch
	}
	if t.ParentTaskID != nil {
		id := *t.ParentTaskID
		out.ParentID = &id
	}
	if host, trace, err := r.src.TaskOrigin(t.ID); err == nil && host != "" {
		out.Origin = &wire.Origin{Host: host, Trace: trace}
	}
	return out
}

func (r *reader) RunStatus(g registry.Grant, p wire.RunStatusParams) (wire.RunStatus, error) {
	if (p.RunID == "") == (p.TaskID == 0) {
		return wire.RunStatus{}, refuse(wire.CodeInvalid, "exactly one of runId and taskId is required")
	}
	var run store.Run
	var err error
	if p.RunID != "" {
		run, err = r.src.GetRun(p.RunID)
	} else {
		var ok bool
		run, ok, err = r.src.LatestRun(p.TaskID)
		if err == nil && !ok {
			err = notFound("run")
		}
	}
	if err != nil {
		if isNotFound(err) {
			return wire.RunStatus{}, notFound("run")
		}
		return wire.RunStatus{}, err
	}
	if run.Purpose != "" && run.Purpose != store.RunPurposeUser {
		return wire.RunStatus{}, notFound("run")
	}
	if _, err := r.readableTask(g, run.TaskID); err != nil {
		if isNotFound(err) {
			return wire.RunStatus{}, notFound("run")
		}
		return wire.RunStatus{}, err
	}
	out := wire.RunStatus{
		RunID:       run.ID,
		TaskID:      run.TaskID,
		State:       run.Status,
		Provider:    run.Provider,
		Model:       run.Model,
		StartedAt:   run.StartedAt.UTC(),
		StopReason:  run.StopReason,
		FailureKind: run.FailureKind,
	}
	if run.FinishedAt != nil {
		f := run.FinishedAt.UTC()
		out.FinishedAt = &f
	}
	if cost, err := r.src.RunCostUSD(run.ID); err == nil {
		out.CostUSD = cost
	}
	return out, nil
}

// activityEvent is the subset of a persisted run event the wire may carry.
type activityEvent struct {
	Type       taskrunner.EventType `json:"type"`
	Text       string               `json:"text"`
	StopReason string               `json:"stopReason"`
	ToolName   string               `json:"toolName"`
	ToolStatus string               `json:"toolStatus"`
}

func (r *reader) Activity(g registry.Grant, p wire.TaskActivityParams) (wire.Activity, error) {
	if p.TaskID == 0 {
		return wire.Activity{}, refuse(wire.CodeInvalid, "taskId is required")
	}
	limit, err := pageLimit(p.Limit, wire.DefaultPageSize)
	if err != nil {
		return wire.Activity{}, err
	}
	if _, err := r.readableTask(g, p.TaskID); err != nil {
		return wire.Activity{}, err
	}
	var run store.Run
	if p.RunID != "" {
		run, err = r.src.GetRun(p.RunID)
		if err != nil {
			if isNotFound(err) {
				return wire.Activity{}, notFound("run")
			}
			return wire.Activity{}, err
		}
		if run.TaskID != p.TaskID || (run.Purpose != "" && run.Purpose != store.RunPurposeUser) {
			return wire.Activity{}, notFound("run")
		}
	} else {
		var ok bool
		run, ok, err = r.src.LatestRun(p.TaskID)
		if err != nil {
			return wire.Activity{}, err
		}
		if !ok {
			return wire.Activity{RunID: "", Events: []wire.Event{}, NextCursor: p.Cursor}, nil
		}
	}
	evs, err := r.src.RunEvents(run.ID, p.Cursor, limit+1)
	if err != nil {
		return wire.Activity{}, err
	}
	res := wire.Activity{RunID: run.ID, Events: []wire.Event{}, NextCursor: p.Cursor}
	if len(evs) > limit {
		res.HasMore = true
		evs = evs[:limit]
	}
	for _, e := range evs {
		res.NextCursor = e.Seq
		if out, ok := r.toEvent(e); ok {
			res.Events = append(res.Events, out)
		}
	}
	return res, nil
}

func (r *reader) toEvent(e store.RunEvent) (wire.Event, bool) {
	var d activityEvent
	if err := json.Unmarshal([]byte(e.EventData), &d); err != nil {
		return wire.Event{}, false
	}
	out := wire.Event{Seq: e.Seq, TS: e.CreatedAt.UTC()}
	switch d.Type {
	case taskrunner.EventTypeText:
		out.Type = "text"
		out.Text, out.Truncated = capText(r.o.Redact(d.Text))
	case taskrunner.EventTypeDone:
		out.Type = "done"
		out.StopReason = d.StopReason
	case taskrunner.EventTypePermissionRequest:
		out.Type = "permission_request"
	case taskrunner.EventTypePermissionResolved:
		out.Type = "permission_resolved"
	case taskrunner.EventTypeToolCall:
		out.Type = "tool_call"
		out.ToolName = d.ToolName
		out.ToolStatus = d.ToolStatus
	default:
		return wire.Event{}, false
	}
	return out, true
}

// capText cuts s to wire.MaxEventTextBytes on a rune boundary.
func capText(s string) (string, bool) {
	if len(s) <= wire.MaxEventTextBytes {
		return s, false
	}
	n := wire.MaxEventTextBytes
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n], true
}
