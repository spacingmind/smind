package store

import (
	"path/filepath"
	"testing"
	"time"
)

// newTestTaskForTerminalSessions creates a workspace + task so terminal
// session rows have a real task_id to reference (terminal_sessions.task_id
// REFERENCES tasks(id), enforced via the foreign_keys pragma) -- mirrors
// runs_test.go's newTestTaskForRuns.
func newTestTaskForTerminalSessions(t *testing.T, s *Store) Task {
	t.Helper()
	ws, err := s.CreateWorkspace(Workspace{Path: "/repo", Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	task, err := s.CreateTask(Task{WorkspaceID: ws.ID, Title: "do the thing", Status: "created"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	return task
}

func TestStore_TerminalSessions(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	task := newTestTaskForTerminalSessions(t, s)
	startedAt := time.Now().UTC().Truncate(time.Second)

	created, err := s.CreateTerminalSession(TerminalSession{
		ID: "term-1", TaskID: task.ID, Status: "running", StartedAt: startedAt,
	})
	if err != nil {
		t.Fatalf("CreateTerminalSession() error = %v", err)
	}
	if created.ID != "term-1" {
		t.Fatalf("CreateTerminalSession() ID = %q, want %q", created.ID, "term-1")
	}

	got, err := s.GetTerminalSession("term-1")
	if err != nil {
		t.Fatalf("GetTerminalSession() error = %v", err)
	}
	if got.TaskID != task.ID || got.Status != "running" || got.Scrollback != "" {
		t.Fatalf("GetTerminalSession() = %+v", got)
	}
	if got.ClosedAt != nil {
		t.Fatalf("GetTerminalSession() ClosedAt = %v, want nil", got.ClosedAt)
	}
	if !got.StartedAt.Equal(startedAt) {
		t.Fatalf("GetTerminalSession() StartedAt = %v, want %v", got.StartedAt, startedAt)
	}

	updated, err := s.UpdateTerminalSessionScrollback("term-1", "hello scrollback")
	if err != nil {
		t.Fatalf("UpdateTerminalSessionScrollback() error = %v", err)
	}
	if updated.Scrollback != "hello scrollback" || updated.Status != "running" {
		t.Fatalf("UpdateTerminalSessionScrollback() = %+v", updated)
	}

	closedAt := startedAt.Add(5 * time.Second)
	closed, err := s.UpdateTerminalSessionStatus("term-1", "closed", &closedAt, "final scrollback")
	if err != nil {
		t.Fatalf("UpdateTerminalSessionStatus() error = %v", err)
	}
	if closed.Status != "closed" || closed.Scrollback != "final scrollback" {
		t.Fatalf("UpdateTerminalSessionStatus() = %+v", closed)
	}
	if closed.ClosedAt == nil || !closed.ClosedAt.Equal(closedAt) {
		t.Fatalf("UpdateTerminalSessionStatus() ClosedAt = %v, want %v", closed.ClosedAt, closedAt)
	}
}

func TestStore_GetTerminalSessionMissing(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	if _, err := s.GetTerminalSession("no-such-session"); err == nil {
		t.Fatal("GetTerminalSession() error = nil, want error")
	}
}

func TestStore_ListRecentTerminalSessions(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	task := newTestTaskForTerminalSessions(t, s)
	base := time.Now().UTC().Truncate(time.Second)

	for i, id := range []string{"term-a", "term-b", "term-c"} {
		if _, err := s.CreateTerminalSession(TerminalSession{
			ID: id, TaskID: task.ID, Status: "closed", StartedAt: base.Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatalf("CreateTerminalSession(%q) error = %v", id, err)
		}
	}

	sessions, err := s.ListRecentTerminalSessions(2)
	if err != nil {
		t.Fatalf("ListRecentTerminalSessions() error = %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("ListRecentTerminalSessions() len = %d, want 2", len(sessions))
	}
	if sessions[0].ID != "term-c" || sessions[1].ID != "term-b" {
		t.Fatalf("ListRecentTerminalSessions() order = [%s, %s], want [term-c, term-b]", sessions[0].ID, sessions[1].ID)
	}
}

func TestStore_MarkRunningTerminalSessionsInterrupted(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	task := newTestTaskForTerminalSessions(t, s)
	now := time.Now().UTC()

	if _, err := s.CreateTerminalSession(TerminalSession{ID: "running-1", TaskID: task.ID, Status: "running", StartedAt: now}); err != nil {
		t.Fatalf("CreateTerminalSession(running-1) error = %v", err)
	}
	if _, err := s.CreateTerminalSession(TerminalSession{ID: "running-2", TaskID: task.ID, Status: "running", StartedAt: now}); err != nil {
		t.Fatalf("CreateTerminalSession(running-2) error = %v", err)
	}
	if _, err := s.CreateTerminalSession(TerminalSession{ID: "closed-1", TaskID: task.ID, Status: "closed", StartedAt: now}); err != nil {
		t.Fatalf("CreateTerminalSession(closed-1) error = %v", err)
	}

	n, err := s.MarkRunningTerminalSessionsInterrupted("interrupted")
	if err != nil {
		t.Fatalf("MarkRunningTerminalSessionsInterrupted() error = %v", err)
	}
	if n != 2 {
		t.Fatalf("MarkRunningTerminalSessionsInterrupted() = %d, want 2", n)
	}

	for _, id := range []string{"running-1", "running-2"} {
		ts, err := s.GetTerminalSession(id)
		if err != nil {
			t.Fatalf("GetTerminalSession(%q) error = %v", id, err)
		}
		if ts.Status != "interrupted" {
			t.Fatalf("GetTerminalSession(%q).Status = %q, want %q", id, ts.Status, "interrupted")
		}
	}
	closedSession, err := s.GetTerminalSession("closed-1")
	if err != nil {
		t.Fatalf("GetTerminalSession(closed-1) error = %v", err)
	}
	if closedSession.Status != "closed" {
		t.Fatalf("GetTerminalSession(closed-1).Status = %q, want unchanged %q", closedSession.Status, "closed")
	}

	// Idempotent: nothing left "running" to mark a second time.
	n2, err := s.MarkRunningTerminalSessionsInterrupted("interrupted")
	if err != nil {
		t.Fatalf("MarkRunningTerminalSessionsInterrupted() second call error = %v", err)
	}
	if n2 != 0 {
		t.Fatalf("MarkRunningTerminalSessionsInterrupted() second call = %d, want 0", n2)
	}
}

func TestStore_TerminalSessionsSurviveReopen(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "smind.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	task := newTestTaskForTerminalSessions(t, s)
	if _, err := s.CreateTerminalSession(TerminalSession{ID: "term-1", TaskID: task.ID, Status: "running", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("CreateTerminalSession() error = %v", err)
	}
	if _, err := s.UpdateTerminalSessionScrollback("term-1", "hello"); err != nil {
		t.Fatalf("UpdateTerminalSessionScrollback() error = %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	got, err := reopened.GetTerminalSession("term-1")
	if err != nil {
		t.Fatalf("GetTerminalSession() after reopen error = %v", err)
	}
	if got.Status != "running" || got.Scrollback != "hello" {
		t.Fatalf("GetTerminalSession() after reopen = %+v", got)
	}
}
