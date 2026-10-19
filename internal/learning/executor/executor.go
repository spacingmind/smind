// Package executor runs the learning reflector/curator as an isolated,
// hidden system run (ADR-0026): through the runs.Registry (persistence,
// usage metering, cancel) but with the no-tools claude-native runner, in an
// empty per-workspace cwd. It returns the model's final text and usage.
package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
)

// ErrBusy is returned when the workspace already has an executor run in
// flight: the cwd is shared, so runs for one workspace are serialised.
var ErrBusy = errors.New("learning executor: workspace already has a run in flight")

// systemTaskTitle titles the per-workspace hidden system task.
const systemTaskTitle = "learning"

// stopWait bounds how long a cancelled Run waits for the stopped run to
// reach its terminal state before returning anyway.
const stopWait = 10 * time.Second

// Request is one isolated reflector/curator run.
type Request struct {
	WorkspaceID  int64
	Purpose      string // store run purpose: "reflect" or "curate"
	Model        string
	SystemPrompt string
	UserPrompt   string
	MaxTurns     int
	Timeout      time.Duration
}

// Result is a completed run. Usage fields are nil when unreported.
type Result struct {
	RunID                     string
	FinalText                 string
	InputTokens, OutputTokens *int64
	CostUSD                   *float64
}

// Executor runs Requests. Safe for concurrent use.
type Executor struct {
	st     *store.Store
	wm     *workspace.Manager
	runner *taskrunner.Runner
	reg    *runs.Registry
	home   string

	mu   sync.Mutex
	busy map[int64]bool
}

// New returns an Executor whose per-workspace cwds live under
// <home>/learning/cwd.
func New(st *store.Store, wm *workspace.Manager, runner *taskrunner.Runner, reg *runs.Registry, home string) *Executor {
	return &Executor{st: st, wm: wm, runner: runner, reg: reg, home: home, busy: map[int64]bool{}}
}

// Run executes r and returns the model's final text. A run that does not
// complete (failed, violated isolation, stopped) is an error and never
// yields text. ctx cancellation stops the run and returns ctx.Err().
func (e *Executor) Run(ctx context.Context, r Request) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if !e.acquire(r.WorkspaceID) {
		return Result{}, ErrBusy
	}
	defer e.release(r.WorkspaceID)

	cwd := e.cwdFor(r.WorkspaceID)
	task, err := e.systemTask(r.WorkspaceID, cwd)
	if err != nil {
		return Result{}, err
	}
	if err := prepareCwd(cwd); err != nil {
		return Result{}, err
	}

	runID, err := e.reg.Start(context.Background(), e.wm, e.runner, task.ID, 0, taskrunner.ProviderClaudeNative, r.UserPrompt,
		taskrunner.PermissionSettings{}, taskrunner.ThinkingLevelUnspecified,
		runs.WithPurpose(r.Purpose), runs.WithModel(r.Model),
		runs.WithIsolation(taskrunner.IsolationOptions{SystemPrompt: r.SystemPrompt, MaxTurns: r.MaxTurns, Timeout: r.Timeout}))
	if err != nil {
		return Result{}, fmt.Errorf("learning executor: start run: %w", err)
	}

	if err := e.wait(ctx, runID); err != nil {
		return Result{RunID: runID}, err
	}

	_, status, err := e.reg.History(runID)
	if err != nil {
		return Result{RunID: runID}, fmt.Errorf("learning executor: run %s status: %w", runID, err)
	}
	if status.Status != runs.StatusDone {
		msg := status.Err
		if msg == "" {
			msg = "run " + string(status.Status)
		}
		// A failed run may still have spent money: hand its usage to the
		// caller so the job row records it (the budget reads run_usage).
		return e.withUsage(Result{RunID: runID}), fmt.Errorf("learning executor: run %s %s: %s", runID, status.Status, msg)
	}

	rows, err := e.st.ListRunEvents(runID)
	if err != nil {
		return Result{RunID: runID}, fmt.Errorf("learning executor: run %s events: %w", runID, err)
	}
	events := make([]taskrunner.Event, 0, len(rows))
	for _, row := range rows {
		ev, err := runs.DecodeStoredEvent(row.EventData)
		if err != nil {
			return Result{RunID: runID}, fmt.Errorf("learning executor: run %s decode event %d: %w", runID, row.Seq, err)
		}
		events = append(events, ev)
	}
	return e.withUsage(Result{RunID: runID, FinalText: taskrunner.FinalText(events)}), nil
}

// withUsage fills res's usage fields from the run's run_usage row, if any.
func (e *Executor) withUsage(res Result) Result {
	if u, err := e.st.GetRunUsage(res.RunID); err == nil {
		res.InputTokens, res.OutputTokens, res.CostUSD = u.InputTokens, u.OutputTokens, u.CostUSD
	}
	return res
}

// wait blocks until runID is terminal. On ctx cancel it stops the run,
// waits (bounded) for the stop to land, and returns ctx.Err().
func (e *Executor) wait(ctx context.Context, runID string) error {
	ch, unsub, err := e.reg.Subscribe(runID)
	if err != nil {
		return fmt.Errorf("learning executor: subscribe run %s: %w", runID, err)
	}
	defer unsub()
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for range ch {
		}
	}()
	select {
	case <-drained:
		return nil
	case <-ctx.Done():
		_ = e.reg.Stop(runID)
		select {
		case <-drained:
		case <-time.After(stopWait):
		}
		return ctx.Err()
	}
}

func (e *Executor) acquire(workspaceID int64) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.busy[workspaceID] {
		return false
	}
	e.busy[workspaceID] = true
	return true
}

func (e *Executor) release(workspaceID int64) {
	e.mu.Lock()
	delete(e.busy, workspaceID)
	e.mu.Unlock()
}

func (e *Executor) cwdFor(workspaceID int64) string {
	return filepath.Join(e.home, "learning", "cwd", strconv.FormatInt(workspaceID, 10))
}

// systemTask returns the workspace's one hidden system task, creating it
// (working in cwd) on first use. A task recorded with a different cwd is
// refused: the cleaning below must only ever touch the expected directory.
func (e *Executor) systemTask(workspaceID int64, cwd string) (store.Task, error) {
	t, ok, err := e.st.GetSystemTask(workspaceID)
	if err != nil {
		return store.Task{}, fmt.Errorf("learning executor: %w", err)
	}
	if !ok {
		if err := os.MkdirAll(cwd, 0o700); err != nil {
			return store.Task{}, fmt.Errorf("learning executor: create cwd: %w", err)
		}
		t, _, err = e.wm.CreateSystemTask(workspaceID, systemTaskTitle, cwd)
		if err != nil {
			return store.Task{}, fmt.Errorf("learning executor: %w", err)
		}
		return t, nil
	}
	if t.WorktreePath == nil || filepath.Clean(*t.WorktreePath) != filepath.Clean(cwd) {
		return store.Task{}, fmt.Errorf("learning executor: system task %d cwd %v is not %s", t.ID, t.WorktreePath, cwd)
	}
	return t, nil
}

// prepareCwd makes sure cwd is a real (non-symlink) 0700 directory holding
// nothing: the CLI may leave entries such as .claude behind, and the
// isolated runner refuses a cwd with ambient context. Only entries directly
// inside cwd are removed; RemoveAll does not follow symlinks.
func prepareCwd(cwd string) error {
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		return fmt.Errorf("learning executor: create cwd: %w", err)
	}
	fi, err := os.Lstat(cwd)
	if err != nil {
		return fmt.Errorf("learning executor: stat cwd: %w", err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("learning executor: cwd %s is not a directory", cwd)
	}
	if err := os.Chmod(cwd, 0o700); err != nil {
		return fmt.Errorf("learning executor: chmod cwd: %w", err)
	}
	entries, err := os.ReadDir(cwd)
	if err != nil {
		return fmt.Errorf("learning executor: read cwd: %w", err)
	}
	for _, ent := range entries {
		if err := os.RemoveAll(filepath.Join(cwd, ent.Name())); err != nil {
			return fmt.Errorf("learning executor: clean cwd entry %q: %w", ent.Name(), err)
		}
	}
	return nil
}
