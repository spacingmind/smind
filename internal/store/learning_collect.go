package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// AddChatLearnPending adds n tool calls to the chat's learn_pending counter
// and returns the new value (ADR-0026 cadence).
func (s *Store) AddChatLearnPending(chatID, n int64) (int64, error) {
	var pending int64
	err := s.db.QueryRow(
		`UPDATE chats SET learn_pending = learn_pending + ? WHERE id = ? RETURNING learn_pending`, n, chatID,
	).Scan(&pending)
	if err != nil {
		return 0, fmt.Errorf("add learn pending to chat %d: %w", chatID, err)
	}
	return pending, nil
}

// SetChatLearnState records a terminal learning job's outcome on the chat:
// learn_pending becomes pending and learn_watermark becomes watermark, but
// the watermark never moves backwards.
func (s *Store) SetChatLearnState(chatID, watermark, pending int64) error {
	res, err := s.db.Exec(
		`UPDATE chats SET learn_watermark = MAX(learn_watermark, ?), learn_pending = ? WHERE id = ?`,
		watermark, pending, chatID,
	)
	if err != nil {
		return fmt.Errorf("set learn state of chat %d: %w", chatID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("set learn state of chat %d: %w", chatID, sql.ErrNoRows)
	}
	return nil
}

// ChatWindowRun is one run of a chat that has events after a watermark.
// Events holds ONLY the events with id > the watermark, ordered by seq.
// PromptInWindow is true when the run's first event is itself after the
// watermark, i.e. its prompt has not been reflected yet; otherwise the
// prompt belongs to an earlier window and is background only.
type ChatWindowRun struct {
	Run            Run
	Events         []RunEvent
	PromptInWindow bool
}

// extraScanner appends extra scan destinations after scanRun's own.
type extraScanner struct {
	row   rowScanner
	extra []any
}

func (e extraScanner) Scan(dest ...any) error { return e.row.Scan(append(dest, e.extra...)...) }

const windowRunScope = `FROM runs r JOIN tasks t ON t.id = r.task_id
	WHERE r.chat_id = ? AND r.purpose = 'user' AND t.kind != 'system'`

// ListChatRunsWithEventsAfter returns the chat's user runs that have at
// least one event with id > afterEventID, ordered by started_at, each with
// only those events. System-task runs and other chats never appear.
func (s *Store) ListChatRunsWithEventsAfter(chatID, afterEventID int64) ([]ChatWindowRun, error) {
	rows, err := s.db.Query(
		`SELECT r.id, r.task_id, r.chat_id, r.provider, r.prompt, r.status, r.started_at, r.finished_at, r.stop_reason,
			r.err_msg, r.permission_mode, r.auto_accept, r.model, r.base_sha, r.head_sha, r.dirty, r.purpose, r.origin, r.interrupt,
			NOT EXISTS (SELECT 1 FROM run_events b WHERE b.run_id = r.id AND b.id <= ?)
		`+windowRunScope+`
			AND EXISTS (SELECT 1 FROM run_events e WHERE e.run_id = r.id AND e.id > ?)
		ORDER BY r.started_at, r.id`, afterEventID, chatID, afterEventID)
	if err != nil {
		return nil, fmt.Errorf("list runs of chat %d after event %d: %w", chatID, afterEventID, err)
	}
	var out []ChatWindowRun
	idx := map[string]int{}
	for rows.Next() {
		var inWindow bool
		r, err := scanRun(extraScanner{row: rows, extra: []any{&inWindow}})
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan run of chat %d: %w", chatID, err)
		}
		idx[r.ID] = len(out)
		out = append(out, ChatWindowRun{Run: r, PromptInWindow: inWindow})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(out) == 0 {
		return out, nil
	}

	evRows, err := s.db.Query(
		`SELECT e.id, e.run_id, e.seq, e.event_data, e.created_at FROM run_events e JOIN runs r ON r.id = e.run_id JOIN tasks t ON t.id = r.task_id
		 WHERE r.chat_id = ? AND r.purpose = 'user' AND t.kind != 'system' AND e.id > ?
		 ORDER BY e.run_id, e.seq`, chatID, afterEventID)
	if err != nil {
		return nil, fmt.Errorf("list events of chat %d after event %d: %w", chatID, afterEventID, err)
	}
	defer evRows.Close()
	for evRows.Next() {
		var e RunEvent
		if err := evRows.Scan(&e.ID, &e.RunID, &e.Seq, &e.EventData, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan event of chat %d: %w", chatID, err)
		}
		if i, ok := idx[e.RunID]; ok {
			out[i].Events = append(out[i].Events, e)
		}
	}
	return out, evRows.Err()
}

// ListTailCandidates returns chats that should get a tail flush: in a
// learning_mode='propose' workspace, on a non-system task, with
// learn_pending >= minPending, and either archived or idle -- no run
// running and the latest run finished at or before idleBefore.
func (s *Store) ListTailCandidates(minPending int64, idleBefore time.Time) ([]Chat, error) {
	rows, err := s.db.Query(
		`SELECT c.id, c.task_id, c.title, c.provider, c.agent_session, c.created_at, c.archived_at, c.learn_watermark, c.learn_pending
		 FROM chats c JOIN tasks t ON t.id = c.task_id JOIN workspaces w ON w.id = t.workspace_id
		 WHERE w.learning_mode = 'propose' AND t.kind != 'system' AND c.learn_pending >= ?
		   AND (c.archived_at IS NOT NULL OR (
		     NOT EXISTS (SELECT 1 FROM runs r WHERE r.chat_id = c.id AND r.status = 'running')
		     AND (SELECT COALESCE(r.finished_at, r.started_at) FROM runs r WHERE r.chat_id = c.id AND r.purpose = 'user'
		          ORDER BY r.started_at DESC, r.id DESC LIMIT 1) <= ?))
		 ORDER BY c.id`, minPending, idleBefore.UTC())
	if err != nil {
		return nil, fmt.Errorf("list tail candidates: %w", err)
	}
	defer rows.Close()
	var out []Chat
	for rows.Next() {
		c, err := scanChat(rows)
		if err != nil {
			return nil, fmt.Errorf("scan tail candidate: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SumLearningUsageForWorkspace totals the workspace's learning (non-user)
// runs started at or after since. costUSD and tokens (input+output) sum
// only what runs reported. unreportedRuns counts runs the budget cannot see
// a cost or token figure for: no usage row, or a NULL cost, or both token
// counts NULL -- callers should price those pessimistically.
func (s *Store) SumLearningUsageForWorkspace(workspaceID int64, since time.Time) (costUSD float64, tokens int64, unreportedRuns int, err error) {
	err = s.db.QueryRow(
		`SELECT COALESCE(SUM(u.cost_usd), 0),
			COALESCE(SUM(COALESCE(u.input_tokens, 0) + COALESCE(u.output_tokens, 0)), 0),
			COALESCE(SUM(CASE WHEN u.run_id IS NULL OR u.cost_usd IS NULL OR (u.input_tokens IS NULL AND u.output_tokens IS NULL) THEN 1 ELSE 0 END), 0)
		 FROM runs r JOIN tasks t ON t.id = r.task_id LEFT JOIN run_usage u ON u.run_id = r.id
		 WHERE t.workspace_id = ? AND r.purpose != 'user' AND r.started_at >= ?`, workspaceID, since.UTC(),
	).Scan(&costUSD, &tokens, &unreportedRuns)
	if err != nil {
		err = fmt.Errorf("sum learning usage for workspace %d: %w", workspaceID, err)
	}
	return
}

// LastLearningJobAt returns when the workspace's latest non-explicit job of
// kind ("reflect" or "curate") was created, in any status; ok is false when
// there is none. Explicit jobs are excluded so they never start a cool-down.
func (s *Store) LastLearningJobAt(workspaceID int64, kind string) (time.Time, bool, error) {
	var at time.Time
	err := s.db.QueryRow(
		`SELECT created_at FROM learning_jobs WHERE workspace_id = ? AND kind = ? AND trigger_kind != 'explicit'
		 ORDER BY created_at DESC, id DESC LIMIT 1`, workspaceID, kind,
	).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("last learning job of workspace %d: %w", workspaceID, err)
	}
	return at, true, nil
}

// FailInterruptedLearningJobs marks every queued or running job failed with
// error=reason and a finish time; called once at daemon start, when no job
// can still be running. Chat watermarks are not touched, so the same window
// is retried. Returns the number of jobs changed.
func (s *Store) FailInterruptedLearningJobs(reason string) (int, error) {
	res, err := s.db.Exec(
		`UPDATE learning_jobs SET status = 'failed', error = ?, finished_at = ? WHERE status IN ('queued', 'running')`,
		reason, time.Now().UTC())
	if err != nil {
		return 0, fmt.Errorf("fail interrupted learning jobs: %w", err)
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// ChatWorkspace is a chat's place in the hierarchy, as the learning
// collector needs it.
type ChatWorkspace struct {
	ChatID        int64
	Provider      string // chats.provider, "" = unbound
	TaskID        int64
	TaskKind      string
	WorkspaceID   int64
	WorkspacePath string
	LearningMode  string
}

// GetWorkspaceByChat resolves chat -> task -> workspace.
func (s *Store) GetWorkspaceByChat(chatID int64) (ChatWorkspace, error) {
	var cw ChatWorkspace
	err := s.db.QueryRow(
		`SELECT c.id, COALESCE(c.provider, ''), t.id, t.kind, w.id, w.path, w.learning_mode
		 FROM chats c JOIN tasks t ON t.id = c.task_id JOIN workspaces w ON w.id = t.workspace_id
		 WHERE c.id = ?`, chatID,
	).Scan(&cw.ChatID, &cw.Provider, &cw.TaskID, &cw.TaskKind, &cw.WorkspaceID, &cw.WorkspacePath, &cw.LearningMode)
	if err != nil {
		return ChatWorkspace{}, fmt.Errorf("get workspace of chat %d: %w", chatID, err)
	}
	cw.TaskKind = strings.TrimSpace(cw.TaskKind)
	return cw, nil
}
