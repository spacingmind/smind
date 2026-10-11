package workspace

import (
	"context"
	"fmt"
	"log"
	"strconv"

	"github.com/spacingmind/smind/internal/store"
)

// Setup states stored in tasks.setup_status (ADR-0024).
const (
	SetupRunning = "running"
	SetupOK      = "ok"
	SetupFailed  = "failed"
)

// setupRun tracks one in-flight setup goroutine so archive/delete and daemon
// shutdown can cancel it and wait for its final row write.
type setupRun struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// SetSetupFinishedHook registers fn, called (in the setup goroutine, after
// the final status row is written) whenever a setup leaves "running" --
// whether ok or failed. wsapi wires it to the run registry's queue delivery
// so a prompt queued while setup ran gets delivered (ADR-0024 §2).
func (m *Manager) SetSetupFinishedHook(fn func(taskID int64)) {
	if m == nil {
		return
	}
	m.setupMu.Lock()
	m.setupFinished = fn
	m.setupMu.Unlock()
}

// failInterruptedSetups marks every task left "running" by a previous
// daemon as failed; it is not re-run (ADR-0024 §3).
func (m *Manager) failInterruptedSetups() {
	tasks, err := m.store.ListTasksSetupRunning()
	if err != nil {
		log.Printf("workspace: list interrupted setups: %v", err)
		return
	}
	for _, t := range tasks {
		if _, err := m.store.UpdateTaskSetup(t.ID, SetupFailed, "interrupted by daemon restart", t.SetupLog); err != nil {
			log.Printf("workspace: mark task %d setup interrupted: %v", t.ID, err)
		}
	}
}

// setupEnv builds the SMIND_* environment entries for a task's setup and
// teardown scripts. Values travel only as environment variables.
func setupEnv(ws store.Workspace, t store.Task) []string {
	env := []string{
		"SMIND_SOURCE_CHECKOUT_PATH=" + ws.Path,
		"SMIND_TASK_ID=" + strconv.FormatInt(t.ID, 10),
		"SMIND_WORKSPACE_ID=" + strconv.FormatInt(ws.ID, 10),
	}
	if t.WorktreePath != nil {
		env = append(env, "SMIND_WORKTREE_PATH="+*t.WorktreePath)
	}
	if t.Branch != nil {
		env = append(env, "SMIND_BRANCH="+*t.Branch)
	}
	if t.Port != nil {
		env = append(env, "SMIND_PORT="+strconv.FormatInt(*t.Port, 10))
	}
	return env
}

// TaskEnv returns the SMIND_PORT entry for t (nil when no port), for spawn
// paths (terminals, Claude-native runs) that only need the port.
func TaskEnv(t store.Task) []string {
	if t.Port == nil {
		return nil
	}
	return []string{"SMIND_PORT=" + strconv.FormatInt(*t.Port, 10)}
}

func (m *Manager) checkSetupSupported(script string) error {
	if script != "" && m.goos == "windows" {
		return ErrSetupUnsupported
	}
	return nil
}

// setupChanged publishes a task row change caused by setup/teardown.
func (m *Manager) setupChanged(t store.Task) {
	if n := m.getNotifier(); n != nil {
		n.NotifyTaskUpdated(t)
	}
	m.notifyTask(t)
}

// startSetup launches ws's setup script for task t in a tracked goroutine.
// The caller has already stored setup_status = running.
func (m *Manager) startSetup(t store.Task, ws store.Workspace) {
	if t.WorktreePath == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	sr := &setupRun{cancel: cancel, done: make(chan struct{})}
	m.setupMu.Lock()
	m.setupRuns[t.ID] = sr
	m.setupMu.Unlock()

	env := setupEnv(ws, t)
	timeout := m.worktreesSettings().SetupTimeout
	go func() {
		defer close(sr.done)
		defer cancel()
		tail, err := runScript(ctx, *t.WorktreePath, ws.SetupScript, env, timeout, setupLogCap)
		status, errMsg := SetupOK, ""
		if err != nil {
			status, errMsg = SetupFailed, err.Error()
		}
		updated, uerr := m.store.UpdateTaskSetup(t.ID, status, errMsg, tail)
		m.setupMu.Lock()
		delete(m.setupRuns, t.ID)
		hook := m.setupFinished
		m.setupMu.Unlock()
		if uerr != nil {
			log.Printf("workspace: record task %d setup result: %v", t.ID, uerr)
			return
		}
		m.setupChanged(updated)
		if hook != nil {
			hook(t.ID)
		}
	}()
}

// cancelSetup kills task id's running setup (if any) and waits for its
// goroutine to record the outcome.
func (m *Manager) cancelSetup(id int64) {
	m.setupMu.Lock()
	sr := m.setupRuns[id]
	m.setupMu.Unlock()
	if sr == nil {
		return
	}
	sr.cancel()
	<-sr.done
}

// CancelSetups kills every running setup script and waits for them; called
// on daemon shutdown so no script outlives the daemon.
func (m *Manager) CancelSetups() {
	if m == nil {
		return
	}
	m.setupMu.Lock()
	runs := make([]*setupRun, 0, len(m.setupRuns))
	for _, sr := range m.setupRuns {
		runs = append(runs, sr)
	}
	m.setupMu.Unlock()
	for _, sr := range runs {
		sr.cancel()
	}
	for _, sr := range runs {
		<-sr.done
	}
}

// RetrySetup re-runs the workspace's setup script on a non-running task
// (ADR-0024 §3). It errors if setup is running, the task is archived or has
// no worktree, or the workspace has no setup script.
func (m *Manager) RetrySetup(id int64) (store.Task, error) {
	t, err := m.store.GetTask(id)
	if err != nil {
		return store.Task{}, fmt.Errorf("retry setup: %w", err)
	}
	if t.SetupStatus == SetupRunning {
		return store.Task{}, fmt.Errorf("retry setup: task %d: setup is already running", id)
	}
	if t.ArchivedAt != nil || t.WorktreePath == nil || !dirExists(*t.WorktreePath) {
		return store.Task{}, fmt.Errorf("retry setup: task %d has no live worktree", id)
	}
	ws, err := m.store.GetWorkspace(t.WorkspaceID)
	if err != nil {
		return store.Task{}, fmt.Errorf("retry setup: %w", err)
	}
	if ws.SetupScript == "" {
		return store.Task{}, fmt.Errorf("retry setup: workspace %d has no setup script", ws.ID)
	}
	if err := m.checkSetupSupported(ws.SetupScript); err != nil {
		return store.Task{}, fmt.Errorf("retry setup: %w", err)
	}
	t, err = m.store.UpdateTaskSetup(id, SetupRunning, "", "")
	if err != nil {
		return store.Task{}, fmt.Errorf("retry setup: %w", err)
	}
	m.setupChanged(t)
	m.startSetup(t, ws)
	return t, nil
}

// runTeardown runs ws's teardown script for t in its worktree, best effort:
// a failure or timeout is appended to the task's setup_log and never
// blocks the archive/delete (ADR-0024 §4).
func (m *Manager) runTeardown(t store.Task, ws store.Workspace) {
	if ws.TeardownScript == "" || t.WorktreePath == nil {
		return
	}
	if err := m.checkSetupSupported(ws.TeardownScript); err != nil {
		m.appendSetupLog(t.ID, "teardown skipped: "+err.Error())
		return
	}
	// Re-read so SMIND_PORT reflects the stored row.
	if fresh, err := m.store.GetTask(t.ID); err == nil {
		t = fresh
	}
	tail, err := runScript(context.Background(), *t.WorktreePath, ws.TeardownScript, setupEnv(ws, t), m.worktreesSettings().TeardownTimeout, setupLogCap)
	if err != nil {
		m.appendSetupLog(t.ID, fmt.Sprintf("teardown failed: %v\n%s", err, tail))
	}
}

// appendSetupLog appends msg to the task's setup_log, keeping the tail
// within setupLogCap. Errors are logged, never returned.
func (m *Manager) appendSetupLog(id int64, msg string) {
	t, err := m.store.GetTask(id)
	if err != nil {
		return
	}
	cur := t.SetupLog
	if cur != "" {
		cur += "\n"
	}
	cur += "--- " + msg
	if over := len(cur) - setupLogCap; over > 0 {
		cur = cur[over:]
	}
	if _, err := m.store.UpdateTaskSetup(id, t.SetupStatus, t.SetupError, cur); err != nil {
		log.Printf("workspace: append task %d setup log: %v", id, err)
	}
}
