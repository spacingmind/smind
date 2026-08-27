package wsapi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
)

// methodHandlers returns the full set of RPC methods this package serves,
// bound to wm, runner, and reg.
func methodHandlers(wm *workspace.Manager, runner *taskrunner.Runner, reg *runs.Registry) map[string]handlerFunc {
	return map[string]handlerFunc{
		"workspace.create": handleWorkspaceCreate(wm),
		"workspace.list":   handleWorkspaceList(wm),
		"workspace.get":    handleWorkspaceGet(wm),
		"space.create":     handleSpaceCreate(wm),
		"space.list":       handleSpaceList(wm),
		"space.get":        handleSpaceGet(wm),
		"task.create":      handleTaskCreate(wm),
		"task.list":        handleTaskList(wm),
		"task.get":         handleTaskGet(wm),
		"task.archive":     handleTaskArchive(wm),
		"task.diff":        handleTaskDiff(wm),
		"task.prompt":      handleTaskPrompt(wm, runner, reg),
		"run.start":        handleRunStart(wm, runner, reg),
		"run.list":         handleRunList(reg),
		"run.attach":       handleRunAttach(reg),
		"run.logs":         handleRunLogs(reg),
		"run.stop":         handleRunStop(reg),
		"file.list":        handleFileList(wm),
		"file.read":        handleFileRead(wm),
		"file.write":       handleFileWrite(wm),
	}
}

func handleWorkspaceCreate(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			Path          string  `json:"path"`
			Title         string  `json:"title"`
			RoutingPolicy string  `json:"routingPolicy"`
			AccountIDs    []int64 `json:"accountIds"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("workspace.create: invalid params: %w", err)
		}
		return wm.CreateWorkspace(p.Path, p.Title, p.RoutingPolicy, p.AccountIDs)
	}
}

func handleWorkspaceList(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, _ json.RawMessage) (any, error) {
		return wm.ListWorkspaces()
	}
}

func handleWorkspaceGet(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("workspace.get: invalid params: %w", err)
		}
		return wm.GetWorkspace(p.ID)
	}
}

func handleSpaceCreate(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			WorkspaceID int64  `json:"workspaceId"`
			Title       string `json:"title"`
			EnvData     string `json:"envData"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("space.create: invalid params: %w", err)
		}
		return wm.CreateSpace(p.WorkspaceID, p.Title, p.EnvData)
	}
}

func handleSpaceList(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			WorkspaceID int64 `json:"workspaceId"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("space.list: invalid params: %w", err)
		}
		return wm.ListSpaces(p.WorkspaceID)
	}
}

func handleSpaceGet(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("space.get: invalid params: %w", err)
		}
		return wm.GetSpace(p.ID)
	}
}

func handleTaskCreate(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			WorkspaceID int64  `json:"workspaceId"`
			SpaceID     *int64 `json:"spaceId"`
			Title       string `json:"title"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("task.create: invalid params: %w", err)
		}
		return wm.CreateTask(p.WorkspaceID, p.SpaceID, p.Title)
	}
}

func handleTaskList(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			WorkspaceID int64 `json:"workspaceId"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("task.list: invalid params: %w", err)
		}
		return wm.ListTasks(p.WorkspaceID)
	}
}

func handleTaskGet(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("task.get: invalid params: %w", err)
		}
		return wm.GetTask(p.ID)
	}
}

func handleTaskArchive(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("task.archive: invalid params: %w", err)
		}
		return wm.ArchiveTask(p.ID)
	}
}

// taskDiffResult is the result of task.diff: id's full unified diff text
// (see workspace.Manager.Diff), or an empty string for a task with no
// changes.
type taskDiffResult struct {
	Diff string `json:"diff"`
}

// handleTaskDiff returns the task's full unified diff -- everything
// changed in its git worktree relative to the commit its branch was
// created from, both committed-but-not-on-base commits and any current
// uncommitted changes. See workspace.Manager.Diff / git.go's taskDiff for
// the exact git invocation.
func handleTaskDiff(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			TaskID int64 `json:"taskId"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("task.diff: invalid params: %w", err)
		}
		diff, err := wm.Diff(p.TaskID)
		if err != nil {
			return nil, fmt.Errorf("task.diff: %w", err)
		}
		return taskDiffResult{Diff: diff}, nil
	}
}

// taskPromptResult is the terminal result of a successful task.prompt (or
// run.attach/run.start reaching StatusDone).
type taskPromptResult struct {
	RunID      string `json:"runId"`
	StopReason string `json:"stopReason"`
}

// taskChunkParams is the params payload of every "chunk" event task.prompt
// and run.attach emit.
type taskChunkParams struct {
	Text string `json:"text"`
}

// handleTaskPrompt starts a Run and then behaves like an implicit
// run.attach on it, for backward compatibility with task.prompt's existing
// (PR #18) behavior: a single connection driving a run start-to-finish
// looks the same as before -- same "chunk" events, same terminal result --
// even though the run itself now lives in reg, independent of this
// connection.
//
// The one place task.prompt's behavior deliberately still differs from
// run.attach's: this request's own context going Done (via task.cancel on
// this request's id, or the connection closing) stops the run it started,
// matching task.prompt's pre-Registry behavior where the run's context
// *was* this request's context. run.attach's context going Done, by
// contrast, only detaches -- see handleRunAttach.
func handleTaskPrompt(wm *workspace.Manager, runner *taskrunner.Runner, reg *runs.Registry) handlerFunc {
	return func(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			TaskID   int64               `json:"taskId"`
			Provider taskrunner.Provider `json:"provider"`
			Prompt   string              `json:"prompt"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("task.prompt: invalid params: %w", err)
		}

		runID, err := reg.Start(context.Background(), wm, runner, p.TaskID, p.Provider, p.Prompt)
		if err != nil {
			return nil, fmt.Errorf("task.prompt: %w", err)
		}

		return attachAndStream(ctx, rc, reg, runID, true)
	}
}

// runStartResult is the terminal result of a successful run.start: just the
// new run's ID, returned as soon as the run is registered -- unlike
// task.prompt/run.attach, run.start never streams and never blocks waiting
// for the run to progress or finish.
type runStartResult struct {
	RunID string `json:"runId"`
}

// handleRunStart is task.prompt's first half on its own: it starts a Run
// (via reg.Start) and returns its ID immediately, without the implicit
// run.attach that makes task.prompt stream and block until the run
// finishes. This is what lets a caller decouple "start a run" from
// "watch a run": the request that starts the run terminates right away, so
// it is never in flight by the time anything might want to cancel a
// separate, later run.attach watching the same run -- see run.attach's own
// doc comment, and the CLI's task-send command, for why that decoupling
// matters (Ctrl+C during a foreground `task send` must detach the watch,
// not stop the run, which task.prompt's own request-scoped stop-on-detach
// behavior cannot support).
func handleRunStart(wm *workspace.Manager, runner *taskrunner.Runner, reg *runs.Registry) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			TaskID   int64               `json:"taskId"`
			Provider taskrunner.Provider `json:"provider"`
			Prompt   string              `json:"prompt"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("run.start: invalid params: %w", err)
		}

		runID, err := reg.Start(context.Background(), wm, runner, p.TaskID, p.Provider, p.Prompt)
		if err != nil {
			return nil, fmt.Errorf("run.start: %w", err)
		}
		return runStartResult{RunID: runID}, nil
	}
}

func handleRunList(reg *runs.Registry) handlerFunc {
	return func(_ context.Context, _ *requestContext, _ json.RawMessage) (any, error) {
		return reg.List(), nil
	}
}

// handleRunAttach streams runID's backfilled-then-live events exactly like
// task.prompt does, terminating once the run reaches a terminal state
// (immediately, if it already has). Unlike task.prompt, this request's own
// context going Done (connection closing, or a task.cancel naming this
// request's id) only detaches -- the run keeps going -- matching the
// "detaching does not stop the run" requirement.
func handleRunAttach(reg *runs.Registry) handlerFunc {
	return func(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			RunID string `json:"runId"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("run.attach: invalid params: %w", err)
		}
		return attachAndStream(ctx, rc, reg, p.RunID, false)
	}
}

// attachAndStream subscribes to runID and forwards its events as "chunk"
// events on rc until the run goes terminal, at which point it returns the
// same shape task.prompt always has: a taskPromptResult on success, or an
// error if the run ended in StatusError or StatusStopped.
//
// If stopOnDetach is true and ctx goes Done before the run finishes, the
// run is stopped (via reg.Stop) rather than merely detached from -- see
// handleTaskPrompt's doc comment for why task.prompt needs that and
// run.attach doesn't. Either way, once ctx is Done this stops re-selecting
// on it (cancelCh is set to nil, which blocks forever) so a repeated
// cancel can't call Stop twice or otherwise re-enter that branch; the loop
// then just drains events to their natural close.
func attachAndStream(ctx context.Context, rc *requestContext, reg *runs.Registry, runID string, stopOnDetach bool) (any, error) {
	events, unsubscribe, err := reg.Subscribe(runID)
	if err != nil {
		return nil, fmt.Errorf("run %s: %w", runID, err)
	}
	defer unsubscribe()

	cancelCh := ctx.Done()
	for {
		select {
		case e, ok := <-events:
			if !ok {
				return terminalResult(reg, runID)
			}
			if e.Type == taskrunner.EventTypeText {
				rc.Emit("chunk", taskChunkParams{Text: e.Text})
			}
		case <-cancelCh:
			if !stopOnDetach {
				return nil, ctx.Err()
			}
			_ = reg.Stop(runID)
			cancelCh = nil
		}
	}
}

func terminalResult(reg *runs.Registry, runID string) (any, error) {
	_, status, err := reg.History(runID)
	if err != nil {
		return nil, fmt.Errorf("run %s: %w", runID, err)
	}
	switch status.Status {
	case runs.StatusDone:
		return taskPromptResult{RunID: runID, StopReason: status.StopReason}, nil
	case runs.StatusStopped:
		return nil, fmt.Errorf("run %s: stopped", runID)
	case runs.StatusError:
		return nil, fmt.Errorf("run %s: %s", runID, status.Err)
	default:
		return nil, fmt.Errorf("run %s: not terminal", runID)
	}
}

// runLogEvent is the wire shape of one event in a run.logs response --
// the same fields task.prompt/run.attach's "chunk" events and terminal
// results carry, just batched instead of streamed.
type runLogEvent struct {
	Type       string `json:"type"`
	Text       string `json:"text,omitempty"`
	StopReason string `json:"stopReason,omitempty"`
}

func toRunLogEvent(e taskrunner.Event) runLogEvent {
	switch e.Type {
	case taskrunner.EventTypeDone:
		return runLogEvent{Type: "done", StopReason: e.StopReason}
	default:
		return runLogEvent{Type: "chunk", Text: e.Text}
	}
}

// runLogsResult is the terminal result of run.logs.
type runLogsResult struct {
	RunID      string        `json:"runId"`
	Status     string        `json:"status"`
	StopReason string        `json:"stopReason,omitempty"`
	Err        string        `json:"err,omitempty"`
	Events     []runLogEvent `json:"events"`
}

// handleRunLogs returns runID's full (or, with Tail set, last Tail)
// recorded events plus its current status as a single response -- it never
// streams and never blocks waiting for the run to progress, unlike
// run.attach.
func handleRunLogs(reg *runs.Registry) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			RunID string `json:"runId"`
			Tail  int    `json:"tail"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("run.logs: invalid params: %w", err)
		}

		history, status, err := reg.History(p.RunID)
		if err != nil {
			return nil, fmt.Errorf("run.logs: %w", err)
		}
		if p.Tail > 0 && len(history) > p.Tail {
			history = history[len(history)-p.Tail:]
		}

		events := make([]runLogEvent, len(history))
		for i, e := range history {
			events[i] = toRunLogEvent(e)
		}
		return runLogsResult{
			RunID:      status.ID,
			Status:     string(status.Status),
			StopReason: status.StopReason,
			Err:        status.Err,
			Events:     events,
		}, nil
	}
}

// handleRunStop stops a run by ID regardless of which connection started
// it -- unlike task.cancel, which only knows about still-in-flight
// requests on its own connection. It's not an error to stop an
// already-finished run (see Registry.Stop).
func handleRunStop(reg *runs.Registry) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			RunID string `json:"runId"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("run.stop: invalid params: %w", err)
		}
		if err := reg.Stop(p.RunID); err != nil {
			return nil, fmt.Errorf("run.stop: %w", err)
		}
		return struct{}{}, nil
	}
}
