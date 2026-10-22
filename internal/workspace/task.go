package workspace

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/store"
)

// lastTaskStamp is the most recent value taskStamp handed out.
var lastTaskStamp atomic.Int64

// taskStamp returns the UnixNano-based suffix of a task's worktree and
// branch name, strictly increasing within the process. The wall clock alone
// isn't enough: on Windows it advances in ~0.5-15.6 ms steps, so tasks with
// the same title created in one tick (parallel tests, an orchestrator
// fanning out) got the same name and collided on `git worktree add`.
func taskStamp() int64 {
	for {
		last := lastTaskStamp.Load()
		next := time.Now().UTC().UnixNano()
		if next <= last {
			next = last + 1
		}
		if lastTaskStamp.CompareAndSwap(last, next) {
			return next
		}
	}
}

// CreateTaskOption customizes CreateTask; see WithParentTask.
type CreateTaskOption func(*createTaskParams)

type createTaskParams struct {
	parentTaskID *int64
	baseRef      string
	branch       string
	prNumber     int
	prSupplied   bool
}

// WithParentTask makes the new task a child of parentID, which CreateTask
// validates exists in the same workspace and would not push the hierarchy
// past the configured depth limit (see CreateTask's doc comment).
func WithParentTask(parentID int64) CreateTaskOption {
	return func(p *createTaskParams) { p.parentTaskID = &parentID }
}

// CreateTask creates a task under workspaceID, materializing a real git
// worktree checked out on a new branch before any database row exists.
//
// Worktree layout: worktrees live under
// <config.Dir()>/worktrees/<workspace-id>/<slug>, i.e. under smind's own
// home directory rather than beside the workspace (e.g. workspace.Path +
// "/.."). This keeps all of smind's on-disk state under one root
// regardless of where workspaces happen to live, avoids littering sibling
// directories next to a workspace's own repo checkout where they could
// confuse the workspace owner's tooling, and mirrors how Paseo lays out its
// own agent worktrees in a dedicated directory tree outside the repos they
// check out.
//
// The worktree directory name (and branch name) is derived from the task
// title, not the task's future database id: the worktree must exist before
// the Task row is inserted (a worktree that fails to materialize must not
// leave a DB row behind), so the id isn't known yet at creation time. A
// slug plus a nanosecond timestamp suffix keeps the name readable while
// guaranteeing it won't collide with another task sharing the same title.
//
// The task's starting point is chosen with exactly one optional ref mode:
// WithBaseRef (new branch smind/task-<slug> at that ref's commit),
// WithExistingBranch (check out an existing branch into the worktree, no
// new branch), or WithPullRequest (new branch smind/pr-<n>-<slug> from the
// PR head, GitHub only). Omitting all three keeps the default behavior
// (new branch off the checkout's current HEAD). Supplying more than one is
// rejected up front, before any git command runs. Every user-supplied
// ref/branch is validated (empty, leading '-', whitespace/control chars,
// '..', git check-ref-format, rev-parse --verify) before a worktree is
// touched; git is only ever invoked with discrete argv, never a shell.
//
// WithParentTask(parentID) makes the new task a child of parentID: rejected
// up front, before any worktree is touched, if parentID doesn't exist, if
// it belongs to a different workspace, or if the new task would sit deeper
// than maxTaskDepth allows (store.CreateTask independently re-validates
// existence/workspace once the row is actually inserted, but the depth
// check only lives here -- store.Task carries no config).
func (m *Manager) CreateTask(workspaceID int64, spaceID *int64, title string, opts ...CreateTaskOption) (store.Task, error) {
	var p createTaskParams
	for _, opt := range opts {
		opt(&p)
	}

	ws, err := m.store.GetWorkspace(workspaceID)
	if err != nil {
		return store.Task{}, fmt.Errorf("create task: %w", err)
	}

	if p.parentTaskID != nil {
		if err := m.validateParentDepth(workspaceID, *p.parentTaskID); err != nil {
			return store.Task{}, err
		}
	}

	refModes := 0
	if p.baseRef != "" {
		refModes++
	}
	if p.branch != "" {
		refModes++
	}
	if p.prSupplied {
		refModes++
	}
	if refModes > 1 {
		return store.Task{}, fmt.Errorf("create task: pass at most one of baseRef, branch, prNumber")
	}
	if p.prSupplied && p.prNumber <= 0 {
		return store.Task{}, fmt.Errorf("create task: prNumber must be > 0")
	}

	if err := m.checkSetupSupported(ws.SetupScript); err != nil {
		return store.Task{}, fmt.Errorf("create task: %w", err)
	}

	name := slugify(title) + "-" + strconv.FormatInt(taskStamp(), 36)
	worktreePath := filepath.Join(config.Dir(), "worktrees", strconv.FormatInt(workspaceID, 10), name)
	branch := "smind/task-" + name

	// createdBranch is the branch this call itself creates ("" when it
	// checks out a pre-existing one): the only branch discardTask may ever
	// delete. A task from an existing branch must never delete the user's
	// own branch on a later failure.
	createdBranch := branch
	// branchOwned: CreateTask created the branch (every mode but an
	// existing branch), ADR-0039 section 2.
	branchOwned := true
	var wtErr error
	switch {
	case p.baseRef != "":
		wtErr = createTaskFromBaseRef(ws.Path, worktreePath, branch, p.baseRef)
	case p.branch != "":
		wtErr = createTaskFromBranch(ws.Path, worktreePath, p.branch)
		branch = p.branch
		createdBranch = ""
		branchOwned = false
	case p.prSupplied:
		branch = "smind/pr-" + strconv.Itoa(p.prNumber) + "-" + name
		createdBranch = branch
		wtErr = createTaskFromPR(ws.Path, worktreePath, branch, p.prNumber)
	default:
		wtErr = gitWorktreeAdd(ws.Path, worktreePath, branch)
	}
	if wtErr != nil {
		// A half-created worktree directory must not outlive the failure:
		// no task row exists yet, so nothing but this cleanup stands
		// between the caller and a stray directory.
		if dirExists(worktreePath) {
			_, _ = runGitOutput(ws.Path, "worktree", "remove", "--force", worktreePath)
		}
		return store.Task{}, fmt.Errorf("create task worktree: %w", wtErr)
	}

	// The base is recorded in the same insert as the task, so a failed
	// creation leaves no row; an unresolvable base is empty, never fatal.
	baseRef, baseSha := computeTaskBase(ws, p, worktreePath)

	t, err := m.store.CreateTask(store.Task{
		WorkspaceID:  workspaceID,
		SpaceID:      spaceID,
		ParentTaskID: p.parentTaskID,
		Title:        title,
		Status:       "created",
		WorktreePath: &worktreePath,
		Branch:       &branch,
		BaseRef:      baseRef,
		BaseSha:      baseSha,
		BranchOwned:  branchOwned,
	})
	if err != nil {
		if createdBranch != "" || dirExists(worktreePath) {
			// The worktree exists but the row could not be written: undo it
			// so a failed creation leaves nothing behind.
			_ = gitWorktreeRemove(ws.Path, worktreePath)
			if createdBranch != "" {
				_ = runGit(ws.Path, "branch", "-D", createdBranch)
			}
		}
		return store.Task{}, fmt.Errorf("create task: %w", err)
	}

	// ADR-0024: per-task port, then (if the workspace has a setup script)
	// mark setup running before the task is announced; the script itself
	// starts below, once the task is fully materialized.
	if _, err := m.claimPort(t.ID); err != nil {
		// branch is generated above, so CreateTask created it. Any future
		// mode that checks out a pre-existing branch must pass "" here.
		m.discardTask(ws, t, createdBranch)
		return store.Task{}, fmt.Errorf("create task: %w", err)
	}
	if ws.SetupScript != "" {
		if _, err := m.store.UpdateTaskSetup(t.ID, SetupRunning, "", ""); err != nil {
			return store.Task{}, fmt.Errorf("create task %d: %w", t.ID, err)
		}
	}
	if t, err = m.store.GetTask(t.ID); err != nil {
		return store.Task{}, fmt.Errorf("create task: %w", err)
	}
	if n := m.getNotifier(); n != nil {
		n.NotifyTaskCreated(t)
	}
	m.notifyTask(t)

	// Every task needs at least one chat for task.prompt/run.start's
	// "omitted chatId" resolution (DefaultChat) to find -- see
	// docs/decisions/0016-multiple-chats-per-task.md. A failure here would
	// leave a chat-less task behind (the worktree and task row above are
	// already committed), so it's surfaced rather than swallowed, but
	// nothing rolls back: the same "no store transactions" tradeoff
	// CreateWorkspace's own doc comment already documents for this
	// codebase.
	if _, err := m.createDefaultChat(t.ID); err != nil {
		if ws.SetupScript != "" {
			_, _ = m.store.UpdateTaskSetup(t.ID, SetupFailed, "setup not started: task creation failed", "")
		}
		return store.Task{}, fmt.Errorf("create task %d: %w", t.ID, err)
	}

	if ws.SetupScript != "" {
		m.startSetup(t, ws)
	}
	return t, nil
}

// discardTask undoes a just-created task whose port allocation failed: the
// row, the worktree, and createdBranch.
//
// createdBranch must be the branch CreateTask itself created for this task,
// or "" when the task's branch pre-existed (e.g. a user's own branch checked
// out into the worktree). Only a branch we created may be force-deleted
// here: `git branch -D` on a user's branch would destroy their work.
func (m *Manager) discardTask(ws store.Workspace, t store.Task, createdBranch string) {
	if t.WorktreePath != nil {
		_ = gitWorktreeRemove(ws.Path, *t.WorktreePath)
	}
	if createdBranch != "" {
		_ = runGit(ws.Path, "branch", "-D", createdBranch)
	}
	_ = m.store.DeleteTask(t.ID)
}

// validateParentDepth checks that parentID names an existing task in
// workspaceID and that a new child under it would not exceed
// m.maxTaskDepth, returning a model/human-readable error otherwise. It
// duplicates store.CreateTask's own existence/workspace checks (rather
// than reusing them) so the rejection happens before CreateTask
// materializes a worktree for a task that's about to be refused.
func (m *Manager) validateParentDepth(workspaceID, parentID int64) error {
	parent, err := m.store.GetTask(parentID)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("create task: parent task %d: %w", parentID, store.ErrParentTaskNotFound)
	}
	if err != nil {
		return fmt.Errorf("create task: parent task %d: %w", parentID, err)
	}
	if parent.WorkspaceID != workspaceID {
		return fmt.Errorf("create task: parent task %d: %w", parentID, store.ErrParentTaskMismatch)
	}

	parentDepth, err := m.store.TaskDepth(parentID)
	if err != nil {
		return fmt.Errorf("create task: %w", err)
	}
	if childDepth := parentDepth + 1; childDepth > m.maxTaskDepth {
		return fmt.Errorf("create task: task depth limit reached (%d): do this work yourself or ask the user", m.maxTaskDepth)
	}
	return nil
}

var slugInvalidRunRE = regexp.MustCompile(`[^a-z0-9]+`)

// slugify lowercases title and collapses any run of non-alphanumeric
// characters into a single hyphen, for use in worktree directory and branch
// names. An empty or all-punctuation title falls back to "task".
func slugify(title string) string {
	slug := strings.Trim(slugInvalidRunRE.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if slug == "" {
		return "task"
	}
	return slug
}

// RunTask transitions a task from "created" to "running". It does not spawn
// an agent — that's a separate ACP integration task — this only flips the
// lifecycle state so that work has a hook point.
func (m *Manager) RunTask(id int64) (store.Task, error) {
	t, err := m.store.GetTask(id)
	if err != nil {
		return store.Task{}, fmt.Errorf("run task: %w", err)
	}
	if t.Status != "created" {
		return store.Task{}, fmt.Errorf("run task %d: status is %q, want \"created\"", id, t.Status)
	}
	t, err = m.store.UpdateTaskStatus(id, "running")
	if err != nil {
		return store.Task{}, fmt.Errorf("run task %d: %w", id, err)
	}
	if n := m.getNotifier(); n != nil {
		n.NotifyTaskUpdated(t)
	}
	m.notifyTask(t)
	return t, nil
}

// MoveTask reassigns task id to spaceID (nil ungroups it back to the
// workspace's own ungrouped list). spaceID, when non-nil, must belong to
// the same workspace as the task -- a task's workspace is fixed by its
// worktree, so moving it across workspaces isn't a thing this reassigns;
// only which space *within* that workspace groups it can change.
func (m *Manager) MoveTask(id int64, spaceID *int64) (store.Task, error) {
	t, err := m.store.GetTask(id)
	if err != nil {
		return store.Task{}, fmt.Errorf("move task: %w", err)
	}

	if spaceID != nil {
		sp, err := m.store.GetSpace(*spaceID)
		if err != nil {
			return store.Task{}, fmt.Errorf("move task %d: %w", id, err)
		}
		if sp.WorkspaceID != t.WorkspaceID {
			return store.Task{}, fmt.Errorf("move task %d: space %d belongs to a different workspace", id, *spaceID)
		}
	}

	t, err = m.store.UpdateTaskSpace(id, spaceID)
	if err != nil {
		return store.Task{}, fmt.Errorf("move task %d: %w", id, err)
	}
	if n := m.getNotifier(); n != nil {
		n.NotifyTaskUpdated(t)
	}
	m.notifyTask(t)
	return t, nil
}

// ArchiveTask checkpoints the task's git worktree (if any) onto its branch,
// removes the worktree, and marks the task archived.
//
// The checkpoint (gitWorktreeCheckpoint) commits everything currently
// uncommitted in the worktree -- including untracked files -- under a
// machine-generated checkpoint message, so the branch alone retains all
// work even after the worktree directory is gone; nothing reviewable is
// destroyed by the subsequent `git worktree remove --force`. A checkpoint
// failure aborts the archive before any removal happens: losing the
// worktree with the checkpoint still unwritten is exactly the data-loss
// bug this ordering guards against, so an error is preferable to a
// half-archive that can't be recovered from.
//
// If the worktree directory no longer exists on disk (e.g. it was already
// removed some other way), that's not treated as a failure: the directory
// being gone is exactly the end state archiving is trying to reach.
func (m *Manager) ArchiveTask(id int64) (store.Task, error) {
	t, err := m.store.GetTask(id)
	if err != nil {
		return store.Task{}, fmt.Errorf("archive task: %w", err)
	}

	if err := m.checkpointAndRemoveWorktree(t); err != nil {
		return store.Task{}, fmt.Errorf("archive task %d: %w", id, err)
	}

	t, err = m.store.ArchiveTask(id)
	if err != nil {
		return store.Task{}, fmt.Errorf("archive task %d: %w", id, err)
	}
	if n := m.getNotifier(); n != nil {
		n.NotifyTaskArchived(t)
	}
	m.notifyTask(t)
	return t, nil
}

// ListTasksOption customizes ListTasks; see WithParentFilter.
type ListTasksOption func(*listTasksParams)

type listTasksParams struct {
	parentTaskID *int64
}

// WithParentFilter narrows ListTasks to parentID's direct children only,
// instead of every task in the workspace.
func WithParentFilter(parentID int64) ListTasksOption {
	return func(p *listTasksParams) { p.parentTaskID = &parentID }
}

// ListTasks returns the workspace's non-archived tasks, ordered by id --
// the sidebar's working set. Archived tasks stay queryable via GetTask
// but leave the active list, mirroring how the UI treats archive as
// removal from the tree (the crud-ui spec).
//
// WithParentFilter(parentID) narrows this to parentID's direct children
// (grandchildren are not included); parentID must belong to workspaceID,
// same cross-workspace rejection as CreateTask's WithParentTask.
func (m *Manager) ListTasks(workspaceID int64, opts ...ListTasksOption) ([]store.Task, error) {
	var p listTasksParams
	for _, opt := range opts {
		opt(&p)
	}

	var all []store.Task
	var err error
	if p.parentTaskID != nil {
		parent, gerr := m.store.GetTask(*p.parentTaskID)
		if errors.Is(gerr, sql.ErrNoRows) {
			return nil, fmt.Errorf("list tasks: parent task %d: %w", *p.parentTaskID, store.ErrParentTaskNotFound)
		}
		if gerr != nil {
			return nil, fmt.Errorf("list tasks: parent task %d: %w", *p.parentTaskID, gerr)
		}
		if parent.WorkspaceID != workspaceID {
			return nil, fmt.Errorf("list tasks: parent task %d: %w", *p.parentTaskID, store.ErrParentTaskMismatch)
		}
		all, err = m.store.ListTasksByParent(*p.parentTaskID)
	} else {
		all, err = m.store.ListTasksByWorkspace(workspaceID)
	}
	if err != nil {
		return nil, err
	}

	live := all[:0]
	for _, t := range all {
		if t.Status != "archived" {
			live = append(live, t)
		}
	}
	return live, nil
}

// GetTask returns the task with the given id.
func (m *Manager) GetTask(id int64) (store.Task, error) {
	return m.store.GetTask(id)
}

// Diff returns task id's full unified diff: everything changed in its git
// worktree relative to the commit its branch was created from -- see
// taskDiff (git.go) for the exact git invocation and why. A task with no
// changes at all returns an empty string, not an error.
func (m *Manager) Diff(id int64) (string, error) {
	t, err := m.store.GetTask(id)
	if err != nil {
		return "", fmt.Errorf("task diff: %w", err)
	}
	if t.WorktreePath == nil || t.Branch == nil {
		return "", fmt.Errorf("task diff %d: task has no worktree", id)
	}
	base, err := taskBaseSha(t)
	if err != nil {
		return "", fmt.Errorf("task diff %d: %w", id, err)
	}
	diff, err := taskDiff(*t.WorktreePath, base)
	if err != nil {
		return "", fmt.Errorf("task diff %d: %w", id, err)
	}
	return diff, nil
}
