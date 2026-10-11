package store

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrPortTaken is returned by ClaimTaskPort when another live (non-archived)
// task already holds the port.
var ErrPortTaken = errors.New("port already held by another task")

// SetWorkspaceScripts updates a workspace's setup/teardown scripts. A nil
// argument keeps the stored value; a pointer to "" clears it (ADR-0024).
func (s *Store) SetWorkspaceScripts(id int64, setup, teardown *string) (Workspace, error) {
	if _, err := s.GetWorkspace(id); err != nil {
		return Workspace{}, fmt.Errorf("set workspace %d scripts: %w", id, err)
	}
	for _, f := range []struct {
		col string
		v   *string
	}{{"setup_script", setup}, {"teardown_script", teardown}} {
		if f.v == nil {
			continue
		}
		var val any
		if *f.v != "" {
			val = *f.v
		}
		if _, err := s.db.Exec(`UPDATE workspaces SET `+f.col+` = ?, updated_at = ? WHERE id = ?`, val, time.Now().UTC(), id); err != nil {
			return Workspace{}, fmt.Errorf("set workspace %d %s: %w", id, f.col, err)
		}
	}
	return s.GetWorkspace(id)
}

// UpdateTaskSetup records a task's setup state. An empty status clears it
// (NULL).
func (s *Store) UpdateTaskSetup(id int64, status, errMsg, log string) (Task, error) {
	nul := func(v string) any {
		if v == "" {
			return nil
		}
		return v
	}
	if _, err := s.db.Exec(
		`UPDATE tasks SET setup_status = ?, setup_error = ?, setup_log = ?, updated_at = ? WHERE id = ?`,
		nul(status), nul(errMsg), nul(log), time.Now().UTC(), id,
	); err != nil {
		return Task{}, fmt.Errorf("update task %d setup: %w", id, err)
	}
	return s.GetTask(id)
}

// ClaimTaskPort records port on task id. A live task already holding the
// port yields ErrPortTaken (partial unique index idx_tasks_port_live).
func (s *Store) ClaimTaskPort(id int64, port int) error {
	if _, err := s.db.Exec(`UPDATE tasks SET port = ? WHERE id = ?`, port, id); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return fmt.Errorf("claim port %d for task %d: %w", port, id, ErrPortTaken)
		}
		return fmt.Errorf("claim port %d for task %d: %w", port, id, err)
	}
	return nil
}

// ListLivePorts returns the ports held by non-archived tasks.
func (s *Store) ListLivePorts() (map[int]bool, error) {
	rows, err := s.db.Query(`SELECT port FROM tasks WHERE port IS NOT NULL AND archived_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("list live ports: %w", err)
	}
	defer rows.Close()
	out := map[int]bool{}
	for rows.Next() {
		var p int
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out[p] = true
	}
	return out, rows.Err()
}

// ListTasksSetupRunning returns tasks whose setup_status is "running".
func (s *Store) ListTasksSetupRunning() ([]Task, error) {
	rows, err := s.db.Query(`SELECT ` + taskCols + ` FROM tasks WHERE setup_status = 'running' ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list setup-running tasks: %w", err)
	}
	defer rows.Close()
	out := make([]Task, 0)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
