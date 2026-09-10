package wsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/terminal"
	"github.com/spacingmind/smind/internal/workspace"
)

// methodHandlers returns the full set of RPC methods this package serves,
// bound to wm, runner, reg, treg, and coord.
func methodHandlers(wm *workspace.Manager, acctReg *accounts.Registry, runner *taskrunner.Runner, reg *runs.Registry, treg *terminal.Registry, coord *accounts.LoginCoordinator) map[string]handlerFunc {
	return map[string]handlerFunc{
		"account.add":           handleAccountAdd(acctReg),
		"account.oauthStart":    handleAccountOAuthStart(coord),
		"provider.list":         handleProviderList(),
		"account.list":          handleAccountList(acctReg),
		"workspace.create":      handleWorkspaceCreate(wm),
		"workspace.list":        handleWorkspaceList(wm),
		"workspace.get":         handleWorkspaceGet(wm),
		"workspace.delete":      handleWorkspaceDelete(wm),
		"space.create":          handleSpaceCreate(wm),
		"space.list":            handleSpaceList(wm),
		"space.get":             handleSpaceGet(wm),
		"space.delete":          handleSpaceDelete(wm),
		"task.create":           handleTaskCreate(wm),
		"task.list":             handleTaskList(wm),
		"task.get":              handleTaskGet(wm),
		"task.archive":          handleTaskArchive(wm),
		"task.diff":             handleTaskDiff(wm),
		"task.files":            handleTaskFiles(wm),
		"task.fileDiff":         handleTaskFileDiff(wm),
		"task.stage":            handleTaskStage(wm),
		"task.commit":           handleTaskCommit(wm),
		"task.prompt":           handleTaskPrompt(wm, runner, reg),
		"run.start":             handleRunStart(wm, runner, reg),
		"run.list":              handleRunList(reg),
		"run.attach":            handleRunAttach(reg),
		"run.logs":              handleRunLogs(reg),
		"run.stop":              handleRunStop(reg),
		"run.respondPermission": handleRunRespondPermission(reg),
		"terminal.create":       handleTerminalCreate(wm, treg),
		"terminal.attach":       handleTerminalAttach(treg),
		"terminal.write":        handleTerminalWrite(treg),
		"terminal.resize":       handleTerminalResize(treg),
		"terminal.close":        handleTerminalClose(treg),
		"terminal.list":         handleTerminalList(treg),
		"file.list":             handleFileList(wm),
		"file.read":             handleFileRead(wm),
		"file.write":            handleFileWrite(wm),
		"fs.listDir":            handleFsListDir(),
	}
}

// accountResult is the deliberately credential-free account shape exposed by
// the WebSocket API. Account metadata is useful to clients; credential
// material must stay in the registry/store and never be returned over RPC.
type accountResult struct {
	ID             int64  `json:"id"`
	Provider       string `json:"provider"`
	Label          string `json:"label"`
	CredentialType string `json:"credentialType"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
}

func accountResultFrom(a accounts.Account) accountResult {
	return accountResult{
		ID: a.ID, Provider: a.Provider, Label: a.Label, CredentialType: a.CredentialType,
		CreatedAt: a.CreatedAt.Format(time.RFC3339), UpdatedAt: a.UpdatedAt.Format(time.RFC3339),
	}
}

func handleAccountAdd(registry *accounts.Registry) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		if registry == nil {
			return nil, fmt.Errorf("account.add: accounts registry is unavailable")
		}
		var p struct {
			Provider   string `json:"provider"`
			Label      string `json:"label"`
			Credential string `json:"credential"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("account.add: invalid params: %w", err)
		}
		if p.Provider == "" || p.Label == "" || p.Credential == "" {
			return nil, fmt.Errorf("account.add: provider, label, and credential are required")
		}

		var oauth accounts.OAuthCredential
		if err := json.Unmarshal([]byte(p.Credential), &oauth); err == nil && oauth.RefreshToken != "" {
			created, err := registry.AddOAuth(p.Provider, p.Label, oauth)
			if err != nil {
				return nil, fmt.Errorf("account.add: %w", err)
			}
			account, err := registry.Get(created.ID)
			if err != nil {
				return nil, fmt.Errorf("account.add: %w", err)
			}
			return accountResultFrom(account), nil
		}

		created, err := registry.AddAPIKey(p.Provider, p.Label, strings.TrimSpace(p.Credential))
		if err != nil {
			return nil, fmt.Errorf("account.add: %w", err)
		}
		account, err := registry.Get(created.ID)
		if err != nil {
			return nil, fmt.Errorf("account.add: %w", err)
		}
		return accountResultFrom(account), nil
	}
}

// handleAccountOAuthStart runs a full browser-based OAuth login for
// provider (accounts.LoginCoordinator.Login), emitting an "authorizeUrl"
// event as soon as the vendor's authorize URL is ready -- before this
// request blocks waiting for the callback -- so the caller can open it (CLI)
// or render it (web UI) without polling. See LoginCoordinator.Login's doc
// comment for the full flow: unsupported providers, a second concurrent
// login for the same provider, callback timeout, and state mismatch all
// surface as errors from coord.Login, wrapped here the same way every other
// account.* handler wraps its registry errors.
func handleAccountOAuthStart(coord *accounts.LoginCoordinator) handlerFunc {
	return func(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		if coord == nil {
			return nil, fmt.Errorf("account.oauthStart: login coordinator is unavailable")
		}
		var p struct {
			Provider string `json:"provider"`
			Label    string `json:"label"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("account.oauthStart: invalid params: %w", err)
		}
		if p.Provider == "" || p.Label == "" {
			return nil, fmt.Errorf("account.oauthStart: provider and label are required")
		}

		account, err := coord.Login(ctx, p.Provider, p.Label, func(url string) {
			rc.Emit("authorizeUrl", map[string]string{"url": url})
		})
		if err != nil {
			return nil, fmt.Errorf("account.oauthStart: %w", err)
		}
		return accountResultFrom(account), nil
	}
}

func handleAccountList(registry *accounts.Registry) handlerFunc {
	return func(_ context.Context, _ *requestContext, _ json.RawMessage) (any, error) {
		if registry == nil {
			return nil, fmt.Errorf("account.list: accounts registry is unavailable")
		}
		stored, err := registry.List()
		if err != nil {
			return nil, fmt.Errorf("account.list: %w", err)
		}
		result := make([]accountResult, len(stored))
		for i, account := range stored {
			result[i] = accountResultFrom(account)
		}
		return result, nil
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

// deleteSummaryResult is the shared result shape of workspace.delete and
// space.delete: how many tasks/spaces were actually removed, mirroring
// workspace.DeleteSummary field-for-field, so the client can show an
// accurate "removed N tasks, M spaces" confirmation without a second round
// trip.
type deleteSummaryResult struct {
	TasksRemoved  int `json:"tasksRemoved"`
	SpacesRemoved int `json:"spacesRemoved"`
}

// handleWorkspaceDelete permanently removes a workspace and everything
// under it (every space and its tasks, every ungrouped task) from smind's
// own tracking -- see workspace.Manager.DeleteWorkspace for the
// checkpoint-then-remove-worktree-then-DB-delete ordering that makes this
// safe. It never touches the workspace's real directory on disk.
func handleWorkspaceDelete(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("workspace.delete: invalid params: %w", err)
		}
		summary, err := wm.DeleteWorkspace(p.ID)
		if err != nil {
			return nil, fmt.Errorf("workspace.delete: %w", err)
		}
		return deleteSummaryResult(summary), nil
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

// handleSpaceDelete permanently removes a space and every task within it
// from smind's own tracking -- see workspace.Manager.DeleteSpace. It never
// touches anything on disk outside smind's own worktree directories.
func handleSpaceDelete(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("space.delete: invalid params: %w", err)
		}
		summary, err := wm.DeleteSpace(p.ID)
		if err != nil {
			return nil, fmt.Errorf("space.delete: %w", err)
		}
		return deleteSummaryResult(summary), nil
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

// taskFilesResult is the result of task.files: the task's changed files,
// one entry per path in the same base→worktree diff task.diff computes,
// each with its change kind and current staged state (see
// workspace.TaskFile).
type taskFilesResult struct {
	Files []workspace.TaskFile `json:"files"`
}

// handleTaskFiles returns the task's changed files -- the per-file input
// for the review-and-commit UI. A task with no changes returns an empty
// list, not an error.
func handleTaskFiles(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			TaskID int64 `json:"taskId"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("task.files: invalid params: %w", err)
		}
		files, err := wm.TaskFiles(p.TaskID)
		if err != nil {
			return nil, fmt.Errorf("task.files: %w", err)
		}
		return taskFilesResult{Files: files}, nil
	}
}

// taskFileDiffResult is the result of task.fileDiff: the unified diff for
// exactly one file of the task's base→worktree diff, or an empty string
// for a path with no changes.
type taskFileDiffResult struct {
	Diff string `json:"diff"`
}

// handleTaskFileDiff returns one file's slice of the task's diff -- the
// same snapshot-index computation task.diff runs, restricted to path.
func handleTaskFileDiff(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			TaskID int64  `json:"taskId"`
			Path   string `json:"path"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("task.fileDiff: invalid params: %w", err)
		}
		if p.Path == "" {
			return nil, fmt.Errorf("task.fileDiff: path is required")
		}
		diff, err := wm.TaskFileDiff(p.TaskID, p.Path)
		if err != nil {
			return nil, fmt.Errorf("task.fileDiff: %w", err)
		}
		return taskFileDiffResult{Diff: diff}, nil
	}
}

// handleTaskStage stages or unstages a single file in the task worktree's
// real index -- unlike task.diff's throwaway snapshot index, a real
// mutation, which is the point (see ADR 0006 decision 1).
func handleTaskStage(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			TaskID int64  `json:"taskId"`
			Path   string `json:"path"`
			Staged bool   `json:"staged"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("task.stage: invalid params: %w", err)
		}
		if p.Path == "" {
			return nil, fmt.Errorf("task.stage: path is required")
		}
		if err := wm.TaskStage(p.TaskID, p.Path, p.Staged); err != nil {
			return nil, fmt.Errorf("task.stage: %w", err)
		}
		return struct{}{}, nil
	}
}

// taskCommitResult is the result of task.commit: the new commit's full
// SHA, its subject line, and how many staged files it recorded.
type taskCommitResult struct {
	Commit  string `json:"commit"`
	Subject string `json:"subject"`
	Files   int    `json:"files"`
}

// handleTaskCommit commits exactly what is currently staged in the task
// worktree. author is "human" or "agent"; agent commits gain
// Smind-Agent/Smind-Task trailers (ADR 0006 decision 3) and require a
// non-empty agent (provider) name. Nothing staged surfaces as the clean
// "nothing staged to commit" error rather than a git stderr leak.
func handleTaskCommit(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			TaskID  int64  `json:"taskId"`
			Message string `json:"message"`
			Author  string `json:"author"`
			Agent   string `json:"agent"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("task.commit: invalid params: %w", err)
		}
		result, err := wm.CommitTask(p.TaskID, p.Message, p.Author, p.Agent)
		if err != nil {
			return nil, fmt.Errorf("task.commit: %w", err)
		}
		return taskCommitResult(result), nil
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

// permissionOptionParams is one choice offered by a "permission_request"
// event or a run.logs "permission_request" entry -- the wire shape of
// taskrunner.PermissionOption.
type permissionOptionParams struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"`
}

func toPermissionOptionParams(opts []taskrunner.PermissionOption) []permissionOptionParams {
	out := make([]permissionOptionParams, len(opts))
	for i, o := range opts {
		out[i] = permissionOptionParams{ID: o.ID, Label: o.Label, Kind: o.Kind}
	}
	return out
}

// permissionRequestParams is the params payload of a "permission_request"
// event task.prompt/run.attach emit for taskrunner.EventTypePermissionRequest.
type permissionRequestParams struct {
	RequestID string                   `json:"requestId"`
	Summary   string                   `json:"summary"`
	Options   []permissionOptionParams `json:"options"`
}

// permissionResolvedParams is the params payload of a "permission_resolved"
// event task.prompt/run.attach emit for taskrunner.EventTypePermissionResolved.
type permissionResolvedParams struct {
	RequestID string `json:"requestId"`
	OptionID  string `json:"optionId"`
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
			switch e.Type {
			case taskrunner.EventTypeText:
				rc.Emit("chunk", taskChunkParams{Text: e.Text})
			case taskrunner.EventTypePermissionRequest:
				rc.Emit("permission_request", permissionRequestParams{
					RequestID: e.PermissionRequestID,
					Summary:   e.PermissionSummary,
					Options:   toPermissionOptionParams(e.PermissionOptions),
				})
			case taskrunner.EventTypePermissionResolved:
				rc.Emit("permission_resolved", permissionResolvedParams{
					RequestID: e.PermissionRequestID,
					OptionID:  e.PermissionOptionID,
				})
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
// the same fields task.prompt/run.attach's streamed events and terminal
// results carry, just batched instead of streamed. Type is "chunk", "done",
// "permission_request", or "permission_resolved"; which of the other fields
// are populated depends on it, mirroring taskrunner.Event's own
// discriminated-by-Type shape.
type runLogEvent struct {
	Type       string                   `json:"type"`
	Text       string                   `json:"text,omitempty"`
	StopReason string                   `json:"stopReason,omitempty"`
	RequestID  string                   `json:"requestId,omitempty"`
	Summary    string                   `json:"summary,omitempty"`
	Options    []permissionOptionParams `json:"options,omitempty"`
	OptionID   string                   `json:"optionId,omitempty"`
}

// toRunLogEvent translates one taskrunner.Event into its run.logs wire
// shape. Every taskrunner.EventType has its own explicit case here
// (including the two permission event types) rather than falling through a
// default -- a default branch would otherwise silently mis-render a
// permission event as an empty/wrong "chunk" entry instead of dropping or
// erroring, which is worse and easy to miss if untested.
func toRunLogEvent(e taskrunner.Event) runLogEvent {
	switch e.Type {
	case taskrunner.EventTypeText:
		return runLogEvent{Type: "chunk", Text: e.Text}
	case taskrunner.EventTypeDone:
		return runLogEvent{Type: "done", StopReason: e.StopReason}
	case taskrunner.EventTypePermissionRequest:
		return runLogEvent{
			Type:      "permission_request",
			RequestID: e.PermissionRequestID,
			Summary:   e.PermissionSummary,
			Options:   toPermissionOptionParams(e.PermissionOptions),
		}
	case taskrunner.EventTypePermissionResolved:
		return runLogEvent{
			Type:      "permission_resolved",
			RequestID: e.PermissionRequestID,
			OptionID:  e.PermissionOptionID,
		}
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

// handleRunRespondPermission answers a pending permission request by ID,
// from any connection regardless of which one (if any) is watching the run
// -- mirroring run.stop's cross-connection reasoning. The blocked provider
// callback (see internal/runs.Registry's runPermissionDecider) unblocks
// with this answer and the turn continues.
func handleRunRespondPermission(reg *runs.Registry) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			RunID     string `json:"runId"`
			RequestID string `json:"requestId"`
			OptionID  string `json:"optionId"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("run.respondPermission: invalid params: %w", err)
		}
		if err := reg.RespondPermission(p.RunID, p.RequestID, p.OptionID); err != nil {
			return nil, fmt.Errorf("run.respondPermission: %w", err)
		}
		return struct{}{}, nil
	}
}

// providerListResult is the result of provider.list: every provider the
// daemon supports, sourced from taskrunner.SupportedProviders (the single
// source of truth RunPrompt's dispatch stays in sync with).
type providerListResult struct {
	Providers []taskrunner.ProviderInfo `json:"providers"`
}

// handleProviderList returns the daemon's supported providers. No params.
func handleProviderList() handlerFunc {
	return func(_ context.Context, _ *requestContext, _ json.RawMessage) (any, error) {
		return providerListResult{Providers: taskrunner.SupportedProviders()}, nil
	}
}
