package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrAutomationExists reports a create with a name that is already used.
var ErrAutomationExists = errors.New("automation name already exists")

// ErrAutomationNotFound reports an unknown automation id.
var ErrAutomationNotFound = errors.New("automation not found")

// Automation statuses (ADR-0044 section 1).
const (
	AutomationActive = "active"
	AutomationPaused = "paused"
)

// Automation is one signal -> task rule (ADR-0044). Signal, Filter, Template
// and Rails are JSON owned by internal/automation; the store does not
// interpret them.
type Automation struct {
	ID                  int64
	Name                string
	Enabled             bool
	WorkspaceID         int64
	SignalKind          string
	Signal              string
	Filter              string
	Template            string
	Rails               string
	Status              string
	PausedReason        string
	PausedAt            *time.Time
	ConsecutiveFailures int
	BudgetBlocked       bool
	LastFiredAt         *time.Time
	NextRunAt           *time.Time
	PollCursor          string
	PollNextAllowedAt   *time.Time
	PollFailures        int
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

const automationColumns = `id, name, enabled, workspace_id, signal_kind, signal, filter, template, rails, status, paused_reason, paused_at,
	consecutive_failures, budget_blocked, last_fired_at, next_run_at, poll_cursor, poll_next_allowed_at, poll_failures, created_at, updated_at`

func scanAutomation(sc rowScanner) (Automation, error) {
	var a Automation
	var pausedAt, lastFired, nextRun, pollNext sql.NullTime
	err := sc.Scan(&a.ID, &a.Name, &a.Enabled, &a.WorkspaceID, &a.SignalKind, &a.Signal, &a.Filter, &a.Template, &a.Rails, &a.Status,
		&a.PausedReason, &pausedAt, &a.ConsecutiveFailures, &a.BudgetBlocked, &lastFired, &nextRun, &a.PollCursor, &pollNext, &a.PollFailures,
		&a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return Automation{}, err
	}
	a.PausedAt, a.LastFiredAt, a.NextRunAt, a.PollNextAllowedAt = nullTimeToPtr(pausedAt), nullTimeToPtr(lastFired), nullTimeToPtr(nextRun), nullTimeToPtr(pollNext)
	return a, nil
}

func nullTimeToPtr(n sql.NullTime) *time.Time {
	if !n.Valid {
		return nil
	}
	t := n.Time.UTC()
	return &t
}

// InsertAutomation records a new automation, DISABLED and active whatever the
// caller set: only an explicit enable arms it (ADR-0044 section 5).
func (s *Store) InsertAutomation(a Automation) (Automation, error) {
	now := time.Now().UTC()
	for _, f := range []*string{&a.Signal, &a.Filter, &a.Template, &a.Rails} {
		if *f == "" {
			*f = "{}"
		}
	}
	res, err := s.db.Exec(`INSERT INTO automations (name, enabled, workspace_id, signal_kind, signal, filter, template, rails, status, created_at, updated_at)
		VALUES (?, 0, ?, ?, ?, ?, ?, ?, 'active', ?, ?)`,
		a.Name, a.WorkspaceID, a.SignalKind, a.Signal, a.Filter, a.Template, a.Rails, now, now)
	if err != nil {
		if isUniqueViolation(err) {
			return Automation{}, fmt.Errorf("insert automation %q: %w", a.Name, ErrAutomationExists)
		}
		return Automation{}, fmt.Errorf("insert automation: %w", err)
	}
	id, _ := res.LastInsertId()
	return s.GetAutomation(id)
}

// GetAutomation returns one automation or ErrAutomationNotFound.
func (s *Store) GetAutomation(id int64) (Automation, error) {
	a, err := scanAutomation(s.db.QueryRow(`SELECT `+automationColumns+` FROM automations WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Automation{}, ErrAutomationNotFound
	}
	if err != nil {
		return Automation{}, fmt.Errorf("get automation %d: %w", id, err)
	}
	return a, nil
}

// ListAutomations returns every automation, oldest first.
func (s *Store) ListAutomations() ([]Automation, error) {
	rows, err := s.db.Query(`SELECT ` + automationColumns + ` FROM automations ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list automations: %w", err)
	}
	defer rows.Close()
	var out []Automation
	for rows.Next() {
		a, err := scanAutomation(rows)
		if err != nil {
			return nil, fmt.Errorf("list automations: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) execAutomation(id int64, what, q string, args ...any) error {
	res, err := s.db.Exec(q, args...)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%s automation %d: %w", what, id, ErrAutomationExists)
		}
		return fmt.Errorf("%s automation %d: %w", what, id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAutomationNotFound
	}
	return nil
}

// UpdateAutomationDefinition replaces the human-authored fields. It never
// touches the engine's state columns (status, counters, cursors). A changed
// definition clears the budget block (the human raised or fixed it).
func (s *Store) UpdateAutomationDefinition(a Automation) error {
	return s.execAutomation(a.ID, "update",
		`UPDATE automations SET name = ?, workspace_id = ?, signal_kind = ?, signal = ?, filter = ?, template = ?, rails = ?,
			budget_blocked = 0, next_run_at = NULL, updated_at = ? WHERE id = ?`,
		a.Name, a.WorkspaceID, a.SignalKind, a.Signal, a.Filter, a.Template, a.Rails, time.Now().UTC(), a.ID)
}

// SetAutomationEnabled flips the human's on/off switch. Enabling clears the
// schedule and poll cursor so the engine re-primes (nothing replays history).
func (s *Store) SetAutomationEnabled(id int64, enabled bool) error {
	return s.execAutomation(id, "enable",
		`UPDATE automations SET enabled = ?, next_run_at = NULL, poll_cursor = CASE WHEN ? THEN '' ELSE poll_cursor END,
			poll_next_allowed_at = NULL, poll_failures = 0, updated_at = ? WHERE id = ?`,
		enabled, enabled, time.Now().UTC(), id)
}

// PauseAutomation is the engine's brake: status paused with a visible reason.
func (s *Store) PauseAutomation(id int64, reason string, now time.Time) error {
	return s.execAutomation(id, "pause",
		`UPDATE automations SET status = 'paused', paused_reason = ?, paused_at = ?, updated_at = ? WHERE id = ?`,
		reason, now.UTC(), now.UTC(), id)
}

// ResumeAutomation clears a pause, the failure counter and the budget block.
func (s *Store) ResumeAutomation(id int64, now time.Time) error {
	return s.execAutomation(id, "resume",
		`UPDATE automations SET status = 'active', paused_reason = '', paused_at = NULL, consecutive_failures = 0, budget_blocked = 0, updated_at = ? WHERE id = ?`,
		now.UTC(), id)
}

// SetAutomationFailures stores the consecutive failed-run counter.
func (s *Store) SetAutomationFailures(id int64, n int) error {
	return s.execAutomation(id, "set failures", `UPDATE automations SET consecutive_failures = ? WHERE id = ?`, n, id)
}

// SetAutomationBudgetBlocked marks (or clears) "the last run reached its budget".
func (s *Store) SetAutomationBudgetBlocked(id int64, blocked bool) error {
	return s.execAutomation(id, "set budget block", `UPDATE automations SET budget_blocked = ? WHERE id = ?`, blocked, id)
}

// SetAutomationFired stamps the last firing time.
func (s *Store) SetAutomationFired(id int64, at time.Time) error {
	return s.execAutomation(id, "stamp fired", `UPDATE automations SET last_fired_at = ? WHERE id = ?`, at.UTC(), id)
}

// SetAutomationNextRun stores a cron source's next scheduled instant (nil clears it).
func (s *Store) SetAutomationNextRun(id int64, next *time.Time) error {
	return s.execAutomation(id, "set next run", `UPDATE automations SET next_run_at = ? WHERE id = ?`, timePtrToNull(utcPtr(next)), id)
}

// SetAutomationPoll stores a polled source's cursor, next allowed poll time
// and failure count in one write.
func (s *Store) SetAutomationPoll(id int64, cursor string, nextAllowed *time.Time, failures int) error {
	return s.execAutomation(id, "set poll state",
		`UPDATE automations SET poll_cursor = ?, poll_next_allowed_at = ?, poll_failures = ? WHERE id = ?`,
		cursor, timePtrToNull(utcPtr(nextAllowed)), failures, id)
}

// DeleteAutomation removes an automation and (by cascade) its audit rows.
func (s *Store) DeleteAutomation(id int64) error {
	return s.execAutomation(id, "delete", `DELETE FROM automations WHERE id = ?`, id)
}

// Audit decisions (ADR-0044 section 6.7).
const (
	AuditFire        = "fire"
	AuditDeduped     = "deduped"
	AuditCapped      = "capped"
	AuditPaused      = "paused"
	AuditMissed      = "missed"
	AuditKillSwitch  = "disabled_kill_switch"
	AuditFailed      = "failed"
	maxAuditPayload  = 4096
	auditKeepLastN   = 100
	auditPruneBatch  = 5000
	auditTruncMarker = "…"
)

// AutomationAudit is one signal evaluation.
type AutomationAudit struct {
	ID           int64
	AutomationID int64
	SignalKind   string
	Fingerprint  string
	Decision     string
	Reason       string
	Manual       bool
	Catchup      bool
	Payload      string
	TaskID       *int64
	RunID        string
	Outcome      string
	Note         string
	CreatedAt    time.Time
	FinishedAt   *time.Time
}

const auditColumns = `id, automation_id, signal_kind, fingerprint, decision, reason, manual, catchup, payload, task_id, run_id, outcome, note, created_at, finished_at`

func scanAudit(sc rowScanner) (AutomationAudit, error) {
	var r AutomationAudit
	var task sql.NullInt64
	var fin sql.NullTime
	if err := sc.Scan(&r.ID, &r.AutomationID, &r.SignalKind, &r.Fingerprint, &r.Decision, &r.Reason, &r.Manual, &r.Catchup, &r.Payload, &task, &r.RunID, &r.Outcome, &r.Note, &r.CreatedAt, &fin); err != nil {
		return AutomationAudit{}, err
	}
	r.TaskID, r.FinishedAt = nullToInt64Ptr(task), nullTimeToPtr(fin)
	return r, nil
}

// InsertAutomationAudit appends one row; the payload is capped at 4 KiB.
func (s *Store) InsertAutomationAudit(r AutomationAudit) (AutomationAudit, error) {
	if len(r.Payload) > maxAuditPayload {
		r.Payload = r.Payload[:maxAuditPayload-len(auditTruncMarker)] + auditTruncMarker
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now()
	}
	res, err := s.db.Exec(`INSERT INTO automation_audit (automation_id, signal_kind, fingerprint, decision, reason, manual, catchup, payload, task_id, run_id, outcome, note, created_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.AutomationID, r.SignalKind, r.Fingerprint, r.Decision, r.Reason, r.Manual, r.Catchup, r.Payload, nullInt64Ptr(r.TaskID), r.RunID, r.Outcome, r.Note,
		r.CreatedAt.UTC(), timePtrToNull(utcPtr(r.FinishedAt)))
	if err != nil {
		return AutomationAudit{}, fmt.Errorf("insert automation audit: %w", err)
	}
	r.ID, _ = res.LastInsertId()
	return r, nil
}

// SetAutomationAuditTask links a fire row to the task and run it started.
func (s *Store) SetAutomationAuditTask(id, taskID int64, runID string) error {
	_, err := s.db.Exec(`UPDATE automation_audit SET task_id = ?, run_id = ? WHERE id = ?`, taskID, runID, id)
	if err != nil {
		return fmt.Errorf("link audit %d to task: %w", id, err)
	}
	return nil
}

// FinishAutomationAudit backfills the outcome of a fire row.
func (s *Store) FinishAutomationAudit(id int64, outcome, note string, at time.Time) error {
	_, err := s.db.Exec(`UPDATE automation_audit SET outcome = ?, note = ?, finished_at = ? WHERE id = ?`, outcome, note, at.UTC(), id)
	if err != nil {
		return fmt.Errorf("finish audit %d: %w", id, err)
	}
	return nil
}

// ListAutomationAudit returns an automation's rows newest first; beforeID > 0
// pages (rows with a smaller id).
func (s *Store) ListAutomationAudit(automationID int64, limit int, beforeID int64) ([]AutomationAudit, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	q := `SELECT ` + auditColumns + ` FROM automation_audit WHERE automation_id = ?`
	args := []any{automationID}
	if beforeID > 0 {
		q += ` AND id < ?`
		args = append(args, beforeID)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	return s.queryAudit(q, args...)
}

func (s *Store) queryAudit(q string, args ...any) ([]AutomationAudit, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("query automation audit: %w", err)
	}
	defer rows.Close()
	var out []AutomationAudit
	for rows.Next() {
		r, err := scanAudit(rows)
		if err != nil {
			return nil, fmt.Errorf("query automation audit: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// OpenAutomationFirings returns the fire rows of an automation whose outcome
// is not recorded yet, oldest first (the engine reconciles them each tick).
func (s *Store) OpenAutomationFirings(automationID int64) ([]AutomationAudit, error) {
	return s.queryAudit(`SELECT `+auditColumns+` FROM automation_audit WHERE automation_id = ? AND decision = 'fire' AND finished_at IS NULL ORDER BY id`, automationID)
}

// CountAutomationFiresSince counts fire rows (manual included) created at or
// after since.
func (s *Store) CountAutomationFiresSince(automationID int64, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM automation_audit WHERE automation_id = ? AND decision = 'fire' AND created_at >= ?`, automationID, since.UTC()).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count automation fires: %w", err)
	}
	return n, nil
}

// HasAutomationFireSince reports whether a fire row with this fingerprint was
// recorded at or after since (deduped rows never count: they do not extend
// the window).
func (s *Store) HasAutomationFireSince(automationID int64, fingerprint string, since time.Time) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM automation_audit WHERE automation_id = ? AND fingerprint = ? AND decision = 'fire' AND created_at >= ?`,
		automationID, fingerprint, since.UTC()).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("check automation fingerprint: %w", err)
	}
	return n > 0, nil
}

// AutomationAuditForTask returns the fire row that started taskID (the task's
// link back to its automation), or ok=false.
func (s *Store) AutomationAuditForTask(taskID int64) (AutomationAudit, bool, error) {
	r, err := scanAudit(s.db.QueryRow(`SELECT `+auditColumns+` FROM automation_audit WHERE task_id = ? AND decision = 'fire' ORDER BY id DESC LIMIT 1`, taskID))
	if errors.Is(err, sql.ErrNoRows) {
		return AutomationAudit{}, false, nil
	}
	if err != nil {
		return AutomationAudit{}, false, fmt.Errorf("audit for task %d: %w", taskID, err)
	}
	return r, true, nil
}

// AutomationLink is a task's link back to the automation that started it.
type AutomationLink struct {
	AutomationID int64
	Name         string
}

// AutomationLinksForTasks returns, for each task an automation started (the
// latest fire row wins), its link back to that automation, in one query per
// batch; tasks without a fire row are simply absent from the map.
func (s *Store) AutomationLinksForTasks(ids []int64) (map[int64]AutomationLink, error) {
	out := make(map[int64]AutomationLink, len(ids))
	// Stay well under SQLite's bound-variable limit.
	const batch = 500
	for start := 0; start < len(ids); start += batch {
		end := min(start+batch, len(ids))
		chunk := ids[start:end]
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}
		rows, err := s.db.Query(`SELECT a.task_id, a.automation_id, n.name FROM automation_audit a
			JOIN automations n ON n.id = a.automation_id
			WHERE a.decision = 'fire' AND a.task_id IN (`+
			strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ",")+`) ORDER BY a.id ASC`, args...)
		if err != nil {
			return nil, fmt.Errorf("automation links for tasks: %w", err)
		}
		for rows.Next() {
			var taskID int64
			var link AutomationLink
			if err := rows.Scan(&taskID, &link.AutomationID, &link.Name); err != nil {
				rows.Close()
				return nil, fmt.Errorf("automation links for tasks: %w", err)
			}
			out[taskID] = link
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, fmt.Errorf("automation links for tasks: %w", err)
		}
	}
	return out, nil
}

// PruneAutomationAudit deletes audit rows older than before, but never the
// newest auditKeepLastN rows of each automation (ADR-0044 question 9).
func (s *Store) PruneAutomationAudit(before time.Time) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM automation_audit WHERE created_at < ? AND id NOT IN (
		SELECT id FROM (
			SELECT id, ROW_NUMBER() OVER (PARTITION BY automation_id ORDER BY id DESC) AS rn FROM automation_audit
		) WHERE rn <= ?) AND id IN (SELECT id FROM automation_audit WHERE created_at < ? LIMIT ?)`,
		before.UTC(), auditKeepLastN, before.UTC(), auditPruneBatch)
	if err != nil {
		return 0, fmt.Errorf("prune automation audit: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
