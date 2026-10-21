package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrParentTaskMismatch and ErrParentTaskNotFound are returned by
// CreateTask when a ParentTaskID fails validation. Compare with errors.Is.
var (
	ErrParentTaskMismatch = errors.New("parent task is in a different workspace")
	ErrParentTaskNotFound = errors.New("parent task not found")
)

// CreateTask inserts a new task, stamping created_at/updated_at. A
// non-nil ParentTaskID must point at an existing task in the same
// workspace; otherwise CreateTask returns an error wrapping
// ErrParentTaskNotFound or ErrParentTaskMismatch (checked via errors.Is),
// and nothing is inserted.
func (s *Store) CreateTask(t Task) (Task, error) {
	if t.ParentTaskID != nil {
		parent, err := s.GetTask(*t.ParentTaskID)
		if errors.Is(err, sql.ErrNoRows) {
			return Task{}, fmt.Errorf("create task: parent task %d: %w", *t.ParentTaskID, ErrParentTaskNotFound)
		}
		if err != nil {
			return Task{}, fmt.Errorf("create task: parent task %d: %w", *t.ParentTaskID, err)
		}
		if parent.WorkspaceID != t.WorkspaceID {
			return Task{}, fmt.Errorf("create task: parent task %d: %w", *t.ParentTaskID, ErrParentTaskMismatch)
		}
	}

	now := time.Now().UTC()
	t.CreatedAt, t.UpdatedAt = now, now
	if t.Kind == "" {
		t.Kind = TaskKindTask
	}
	return insertTask(s.db, t)
}

// Task kinds (tasks.kind, ADR-0026).
const (
	TaskKindTask   = "task"
	TaskKindSystem = "system"
)

// Run purposes (runs.purpose, ADR-0026).
const RunPurposeUser = "user"

const RunPurposeProbe = "probe"

func insertTask(db execer, t Task) (Task, error) {
	res, err := db.Exec(
		`INSERT INTO tasks (workspace_id, space_id, parent_task_id, title, status, worktree_path, branch, created_at, updated_at, archived_at, kind)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.WorkspaceID, int64PtrToNull(t.SpaceID), int64PtrToNull(t.ParentTaskID), t.Title, t.Status,
		stringPtrToNull(t.WorktreePath), stringPtrToNull(t.Branch),
		t.CreatedAt, t.UpdatedAt, timePtrToNull(t.ArchivedAt), t.Kind,
	)
	if err != nil {
		return Task{}, fmt.Errorf("insert task: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Task{}, fmt.Errorf("task id: %w", err)
	}
	t.ID = id
	return t, nil
}

// CreateSystemTask inserts a hidden system task (kind 'system', status
// 'done', worktree_path = cwd, no branch/port/setup) plus its default chat
// in one transaction. It is a dedicated helper rather than a Kind field on
// CreateTask callers because the pair must be atomic and needs no parent
// validation; no git or notifier work happens at this layer.
func (s *Store) CreateSystemTask(workspaceID int64, title, cwd string) (Task, Chat, error) {
	now := time.Now().UTC()
	t := Task{
		WorkspaceID: workspaceID, Title: title, Status: "done", Kind: TaskKindSystem,
		WorktreePath: &cwd, CreatedAt: now, UpdatedAt: now,
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Task{}, Chat{}, fmt.Errorf("create system task: begin: %w", err)
	}
	t, err = insertTask(tx, t)
	if err != nil {
		_ = tx.Rollback()
		return Task{}, Chat{}, fmt.Errorf("create system task: %w", err)
	}
	res, err := tx.Exec(
		`INSERT INTO chats (task_id, title, provider, agent_session, created_at) VALUES (?, ?, NULL, NULL, ?)`,
		t.ID, title, now,
	)
	if err != nil {
		_ = tx.Rollback()
		return Task{}, Chat{}, fmt.Errorf("create system task: insert chat: %w", err)
	}
	chatID, err := res.LastInsertId()
	if err != nil {
		_ = tx.Rollback()
		return Task{}, Chat{}, fmt.Errorf("create system task: chat id: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Task{}, Chat{}, fmt.Errorf("create system task: commit: %w", err)
	}
	return t, Chat{ID: chatID, TaskID: t.ID, Title: title, CreatedAt: now}, nil
}

// GetSystemTask returns workspaceID's hidden system task (kind 'system'), or
// ok=false when it has none. The learning executor keeps exactly one.
func (s *Store) GetSystemTask(workspaceID int64) (t Task, ok bool, err error) {
	row := s.db.QueryRow(
		`SELECT `+taskCols+` FROM tasks WHERE workspace_id = ? AND kind = 'system' ORDER BY id LIMIT 1`, workspaceID,
	)
	t, err = scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, false, nil
	}
	if err != nil {
		return Task{}, false, fmt.Errorf("get system task for workspace %d: %w", workspaceID, err)
	}
	return t, true, nil
}

// GetTask returns the task with the given id.
func (s *Store) GetTask(id int64) (Task, error) {
	row := s.db.QueryRow(
		`SELECT `+taskCols+`
		 FROM tasks WHERE id = ?`, id,
	)
	t, err := scanTask(row)
	if err != nil {
		return Task{}, fmt.Errorf("get task %d: %w", id, err)
	}
	return t, nil
}

// ListTasksByWorkspace returns all non-system tasks for workspaceID,
// ordered by id.
func (s *Store) ListTasksByWorkspace(workspaceID int64) ([]Task, error) {
	return s.listWorkspaceTasks(workspaceID, false)
}

// ListAllTasksByWorkspace is ListTasksByWorkspace including hidden system
// tasks; cascades (DeleteWorkspace) need it so FKs don't block deletion.
func (s *Store) ListAllTasksByWorkspace(workspaceID int64) ([]Task, error) {
	return s.listWorkspaceTasks(workspaceID, true)
}

func (s *Store) listWorkspaceTasks(workspaceID int64, includeSystem bool) ([]Task, error) {
	query := `SELECT ` + taskCols + `
		 FROM tasks WHERE workspace_id = ?`
	if !includeSystem {
		query += ` AND kind != 'system'`
	}
	rows, err := s.db.Query(query+` ORDER BY id`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list tasks for workspace %d: %w", workspaceID, err)
	}
	defer rows.Close()

	tasks := make([]Task, 0)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// ListTasksBySpace returns all non-system tasks in spaceID, ordered by id.
func (s *Store) ListTasksBySpace(spaceID int64) ([]Task, error) {
	return s.listSpaceTasks(spaceID, false)
}

func (s *Store) listSpaceTasks(spaceID int64, includeSystem bool) ([]Task, error) {
	query := `SELECT ` + taskCols + `
		 FROM tasks WHERE space_id = ?`
	if !includeSystem {
		query += ` AND kind != 'system'`
	}
	rows, err := s.db.Query(query+` ORDER BY id`, spaceID)
	if err != nil {
		return nil, fmt.Errorf("list tasks for space %d: %w", spaceID, err)
	}
	defer rows.Close()

	tasks := make([]Task, 0)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// ListTasksByParent returns the direct children of task parentID (tasks
// whose parent_task_id is parentID), ordered by id. Grandchildren are not
// included; callers walk one level at a time.
func (s *Store) ListTasksByParent(parentID int64) ([]Task, error) {
	rows, err := s.db.Query(
		`SELECT `+taskCols+`
		 FROM tasks WHERE parent_task_id = ? AND kind != 'system' ORDER BY id`,
		parentID,
	)
	if err != nil {
		return nil, fmt.Errorf("list tasks for parent %d: %w", parentID, err)
	}
	defer rows.Close()

	tasks := make([]Task, 0)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// TaskDepth returns the number of ancestors task id has -- a root task
// (ParentTaskID nil) is at depth 0, its direct children at depth 1, and so
// on. It walks the parent_task_id chain one hop at a time via GetTask, so
// callers enforcing a depth limit (internal/workspace's CreateTask) can add
// 1 for the depth a prospective child would sit at.
func (s *Store) TaskDepth(id int64) (int, error) {
	depth := 0
	current := id
	for {
		t, err := s.GetTask(current)
		if err != nil {
			return 0, fmt.Errorf("task depth %d: %w", id, err)
		}
		if t.ParentTaskID == nil {
			return depth, nil
		}
		depth++
		current = *t.ParentTaskID
	}
}

// ListUngroupedTasksByWorkspace returns all tasks directly in workspaceID
// (i.e. with no space), ordered by id.
func (s *Store) ListUngroupedTasksByWorkspace(workspaceID int64) ([]Task, error) {
	rows, err := s.db.Query(
		`SELECT `+taskCols+`
		 FROM tasks WHERE workspace_id = ? AND space_id IS NULL AND kind != 'system' ORDER BY id`,
		workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("list ungrouped tasks for workspace %d: %w", workspaceID, err)
	}
	defer rows.Close()

	tasks := make([]Task, 0)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// UpdateTaskStatus sets a task's status and returns the updated task.
func (s *Store) UpdateTaskStatus(id int64, status string) (Task, error) {
	now := time.Now().UTC()
	_, err := s.db.Exec(
		`UPDATE tasks SET status = ?, updated_at = ? WHERE id = ?`,
		status, now, id,
	)
	if err != nil {
		return Task{}, fmt.Errorf("update task %d status: %w", id, err)
	}
	return s.GetTask(id)
}

// UpdateTaskSpace moves a task to spaceID (nil ungroups it back to the
// workspace's own ungrouped list) and returns the updated task. Callers are
// responsible for checking spaceID belongs to the same workspace as the
// task before calling -- this is a plain column write, not a validated move.
func (s *Store) UpdateTaskSpace(id int64, spaceID *int64) (Task, error) {
	now := time.Now().UTC()
	_, err := s.db.Exec(
		`UPDATE tasks SET space_id = ?, updated_at = ? WHERE id = ?`,
		int64PtrToNull(spaceID), now, id,
	)
	if err != nil {
		return Task{}, fmt.Errorf("update task %d space: %w", id, err)
	}
	return s.GetTask(id)
}

// ArchiveTask sets a task's status to "archived" and stamps archived_at, then
// returns the updated task. Calling it again on an already-archived task is a
// no-op: the WHERE clause matches no rows, so archived_at is left as the
// first archival time rather than being bumped forward on a repeated or
// retried call.
func (s *Store) ArchiveTask(id int64) (Task, error) {
	now := time.Now().UTC()
	_, err := s.db.Exec(
		`UPDATE tasks SET status = 'archived', archived_at = ? WHERE id = ? AND status != 'archived'`,
		now, id,
	)
	if err != nil {
		return Task{}, fmt.Errorf("archive task %d: %w", id, err)
	}
	return s.GetTask(id)
}

// DeleteTask permanently removes task id and everything scoped to it from
// smind's own tracking: run_events (via each of its runs), runs, chats,
// terminal_sessions, and finally the tasks row itself, in that FK-safe
// child-before-parent order (matching the schema's foreign keys, enforced
// by _pragma=foreign_keys(1) -- see store.sqliteDSN; chats must go after
// runs since runs.chat_id references chats(id)). The task's children are
// not deleted: their parent_task_id is set to NULL first, making them root
// tasks (the plan's no-automatic-cascade decision), and without which the
// final DELETE would trip the tasks.parent_task_id foreign key for any
// task that has children. Everything runs in one transaction, so a failure
// partway leaves the task and all of its scoped rows intact. DeleteTask
// never touches anything on disk; git worktree cleanup is
// workspace.Manager's job (see workspace.Manager.DeleteTask), which calls
// this only after that succeeds. Deleting a nonexistent task is a clear
// not-found error (via GetTask), never a silent no-op.
func (s *Store) DeleteTask(id int64) error {
	if _, err := s.GetTask(id); err != nil {
		return fmt.Errorf("delete task %d: %w", id, err)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("delete task %d: begin: %w", id, err)
	}
	for _, step := range []struct{ name, query string }{
		{"detach children", `UPDATE tasks SET parent_task_id = NULL WHERE parent_task_id = ?`},
		{"delete run events", `DELETE FROM run_events WHERE run_id IN (SELECT id FROM runs WHERE task_id = ?)`},
		{"delete run usage", `DELETE FROM run_usage WHERE task_id = ?`},
		{"delete runs", `DELETE FROM runs WHERE task_id = ?`},
		{"delete chats", `DELETE FROM chats WHERE task_id = ?`},
		{"delete terminal sessions", `DELETE FROM terminal_sessions WHERE task_id = ?`},
		{"delete task", `DELETE FROM tasks WHERE id = ?`},
	} {
		if _, err := tx.Exec(step.query, id); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("delete task %d: %s: %w", id, step.name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("delete task %d: commit: %w", id, err)
	}
	return nil
}

// taskCols is the column list scanTask reads.
const taskCols = "id, workspace_id, space_id, parent_task_id, title, status, worktree_path, branch, created_at, updated_at, archived_at, setup_status, setup_error, setup_log, port, kind"

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTask(row rowScanner) (Task, error) {
	var t Task
	var spaceID, parentTaskID sql.NullInt64
	var worktreePath, branch, setupStatus, setupError, setupLog sql.NullString
	var archivedAt sql.NullTime
	var port sql.NullInt64

	if err := row.Scan(&t.ID, &t.WorkspaceID, &spaceID, &parentTaskID, &t.Title, &t.Status,
		&worktreePath, &branch, &t.CreatedAt, &t.UpdatedAt, &archivedAt,
		&setupStatus, &setupError, &setupLog, &port, &t.Kind); err != nil {
		return Task{}, err
	}

	t.SpaceID = nullToInt64Ptr(spaceID)
	t.ParentTaskID = nullToInt64Ptr(parentTaskID)
	t.WorktreePath = nullToStringPtr(worktreePath)
	t.Branch = nullToStringPtr(branch)
	t.ArchivedAt = nullToTimePtr(archivedAt)
	t.SetupStatus = setupStatus.String
	t.SetupError = setupError.String
	t.SetupLog = setupLog.String
	t.Port = nullToInt64Ptr(port)
	return t, nil
}

func int64PtrToNull(p *int64) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *p, Valid: true}
}

func stringPtrToNull(p *string) sql.NullString {
	if p == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *p, Valid: true}
}

func timePtrToNull(p *time.Time) sql.NullTime {
	if p == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *p, Valid: true}
}

func nullToInt64Ptr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

func nullToStringPtr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	v := n.String
	return &v
}

func nullToTimePtr(n sql.NullTime) *time.Time {
	if !n.Valid {
		return nil
	}
	v := n.Time
	return &v
}

// nullString maps "" to SQL NULL, for optional TEXT columns where empty
// means "unset" (runs.model, agent_profiles.model).
func nullString(v string) sql.NullString {
	return sql.NullString{String: v, Valid: v != ""}
}
