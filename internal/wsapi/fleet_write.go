package wsapi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/fleet/serve"
	"github.com/spacingmind/smind/internal/fleet/wire"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
)

// fleetFetchTimeout bounds the one git fetch a fleet task create runs, and
// fleetStopTimeout how long stopping a remote-origin run waits for the
// agent subprocess to end.
const (
	fleetFetchTimeout = 60 * time.Second
	fleetStopTimeout  = 15 * time.Second
)

// fleetBackend implements serve.Backend against this daemon's own workspace
// manager, task runner and runs registry (ADR-0048 D6): the wire types
// cannot carry the caller's credentials or agent configuration, so this is
// the only place a fleet write ever resolves a provider or starts a run,
// always with the callee's own accounts and settings.
type fleetBackend struct {
	a  *API
	db *store.Store
}

func (b *fleetBackend) ProviderConfigured(id string) bool {
	_, ok := b.a.runner.Providers().Get(id)
	return ok
}

// CreateTask brings branch or baseRef in from the workspace's own origin
// and creates the task worktree (D5: code arrives by git only, never as an
// argument). git's stderr is never returned -- a remote URL can carry
// credentials -- so every failure reads as "branch not found on origin".
func (b *fleetBackend) CreateTask(workspaceID int64, title, branch, baseRef string) (serve.TaskInfo, error) {
	ws, err := b.db.GetWorkspace(workspaceID)
	if err != nil {
		return serve.TaskInfo{}, serve.NewRefusal(wire.CodeNotFound, "workspace not found")
	}
	ref := branch
	if ref == "" {
		ref = strings.TrimPrefix(baseRef, "origin/")
	}
	var opts []workspace.CreateTaskOption
	if ref != "" {
		// The wire layer validated the name already; this re-check is the
		// last gate before the value lands in a git argv.
		if strings.HasPrefix(ref, "-") || strings.ContainsFunc(ref, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
			return serve.TaskInfo{}, serve.NewRefusal(wire.CodeInvalid, "branch is not a valid ref name")
		}
		if err := fetchOriginRef(ws.Path, ref); err != nil {
			return serve.TaskInfo{}, err
		}
		if branch != "" {
			opts = append(opts, workspace.WithExistingBranch(branch))
		} else {
			opts = append(opts, workspace.WithBaseRef("origin/"+ref))
		}
	}
	task, err := b.a.wm.CreateTask(workspaceID, nil, title, opts...)
	if err != nil {
		return serve.TaskInfo{}, err
	}
	ti := serve.TaskInfo{ID: task.ID}
	if task.Branch != nil {
		ti.Branch = *task.Branch
	}
	return ti, nil
}

// fetchOriginRef runs the one fetch a remote task's branch or base ref
// needs, into the tracking ref the workspace manager reads back
// (refs/remotes/origin/<ref>), with no credential prompts and no tags.
func fetchOriginRef(path, ref string) error {
	ctx, cancel := context.WithTimeout(context.Background(), fleetFetchTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-c", "core.fsmonitor=false", "-C", path,
		"fetch", "--no-tags", "origin",
		fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", ref, ref))
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if err := cmd.Run(); err != nil {
		return serve.NewRefusal(wire.CodeBranchNotFound, "branch not found on origin")
	}
	return nil
}

func (b *fleetBackend) SetTaskOrigin(taskID int64, originHost, originTrace string) error {
	return b.db.SetTaskOrigin(taskID, originHost, originTrace)
}

// StartRun starts the remote-origin run: reject-when-busy (the caller is
// told the task is busy, never silently queued), the remote queue source
// (which forces the run off AutoAccept and stamps runs.origin_host), no
// reply chat and no finish notification -- nobody local is waiting.
func (b *fleetBackend) StartRun(taskID int64, provider, model, prompt, originHost string) (string, error) {
	out, err := b.a.Runs.StartWhenBusy(context.Background(), b.a.wm, b.a.runner, taskID, 0,
		taskrunner.Provider(provider), prompt, taskrunner.PermissionSettings{}, "",
		runs.WhenBusyReject, store.ChatQueueSourceRemote, 0, 0,
		runs.WithModel(model), runs.WithOriginHost(originHost), runs.WithNotifyOnFinish(false))
	if err != nil {
		// The two busy shapes WhenBusyReject answers with: the task's chat
		// has a running run, or its worktree setup is still in flight.
		var chatBusy *runs.ChatBusyError
		var setupBusy *runs.SetupBusyError
		if errors.As(err, &chatBusy) || errors.As(err, &setupBusy) {
			return "", serve.NewRefusal("run_busy", "task already has a running run")
		}
		return "", err
	}
	if out.Queued {
		// Unreachable with WhenBusyReject; held as an error because the
		// caller would otherwise wait on a run id that does not exist.
		return "", fmt.Errorf("fleet write: start of task %d queued (item %d) under reject-when-busy", taskID, out.QueueItemID)
	}
	return out.RunID, nil
}

func (b *fleetBackend) StopRun(runID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), fleetStopTimeout)
	defer cancel()
	return b.a.Runs.StopAndWait(ctx, runID)
}

// WaitingPermission reports whether the run still holds an unanswered
// permission request -- the flag is all a remote caller ever learns of it
// (AC19); the request itself sits in the callee's inbox.
func (b *fleetBackend) WaitingPermission(runID string) bool {
	for _, p := range b.a.Runs.PendingPermissions() {
		if p.RunID == runID {
			return true
		}
	}
	return false
}

// startFleetIdemSweeper deletes idempotency rows past the 24 h window once
// at start and then hourly. Without it an expired key's row would live
// forever and block re-storing the same key, so a caller reusing it after
// the window would keep hitting the stale row's method check.
func startFleetIdemSweeper(st *store.Store, now func() time.Time) (stop func()) {
	if now == nil {
		now = time.Now
	}
	done := make(chan struct{})
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		sweep := func() { _, _ = st.SweepFleetIdem(now().Add(-wire.IdempotencyWindow)) }
		sweep()
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				sweep()
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() { close(done) })
		<-exited
	}
}
