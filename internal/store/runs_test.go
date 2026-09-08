package store

import (
	"path/filepath"
	"testing"
	"time"
)

// newTestTaskForRuns creates a workspace + task so run rows have a real
// task_id to reference (runs.task_id REFERENCES tasks(id), enforced via the
// foreign_keys pragma).
func newTestTaskForRuns(t *testing.T, s *Store) Task {
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

func TestStore_Runs(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	task := newTestTaskForRuns(t, s)
	startedAt := time.Now().UTC().Truncate(time.Second)

	created, err := s.CreateRun(Run{
		ID: "run-1", TaskID: task.ID, Provider: "glm", Prompt: "hi",
		Status: "running", StartedAt: startedAt,
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if created.ID != "run-1" {
		t.Fatalf("CreateRun() ID = %q, want %q", created.ID, "run-1")
	}

	got, err := s.GetRun("run-1")
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if got.TaskID != task.ID || got.Provider != "glm" || got.Prompt != "hi" || got.Status != "running" {
		t.Fatalf("GetRun() = %+v", got)
	}
	if got.FinishedAt != nil {
		t.Fatalf("GetRun() FinishedAt = %v, want nil", got.FinishedAt)
	}
	if !got.StartedAt.Equal(startedAt) {
		t.Fatalf("GetRun() StartedAt = %v, want %v", got.StartedAt, startedAt)
	}

	finishedAt := startedAt.Add(5 * time.Second)
	updated, err := s.UpdateRunStatus("run-1", "done", &finishedAt, "end_turn", "")
	if err != nil {
		t.Fatalf("UpdateRunStatus() error = %v", err)
	}
	if updated.Status != "done" || updated.StopReason != "end_turn" {
		t.Fatalf("UpdateRunStatus() = %+v", updated)
	}
	if updated.FinishedAt == nil || !updated.FinishedAt.Equal(finishedAt) {
		t.Fatalf("UpdateRunStatus() FinishedAt = %v, want %v", updated.FinishedAt, finishedAt)
	}
}

func TestStore_GetRunMissing(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	if _, err := s.GetRun("no-such-run"); err == nil {
		t.Fatal("GetRun() error = nil, want error")
	}
}

func TestStore_ListRecentRuns(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	task := newTestTaskForRuns(t, s)
	base := time.Now().UTC().Truncate(time.Second)

	for i, id := range []string{"run-a", "run-b", "run-c"} {
		if _, err := s.CreateRun(Run{
			ID: id, TaskID: task.ID, Provider: "glm", Prompt: "hi",
			Status: "done", StartedAt: base.Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatalf("CreateRun(%q) error = %v", id, err)
		}
	}

	runs, err := s.ListRecentRuns(2)
	if err != nil {
		t.Fatalf("ListRecentRuns() error = %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("ListRecentRuns() len = %d, want 2", len(runs))
	}
	if runs[0].ID != "run-c" || runs[1].ID != "run-b" {
		t.Fatalf("ListRecentRuns() order = [%s, %s], want [run-c, run-b]", runs[0].ID, runs[1].ID)
	}
}

func TestStore_MarkRunningRunsInterrupted(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	task := newTestTaskForRuns(t, s)
	now := time.Now().UTC()

	if _, err := s.CreateRun(Run{ID: "running-1", TaskID: task.ID, Provider: "glm", Prompt: "hi", Status: "running", StartedAt: now}); err != nil {
		t.Fatalf("CreateRun(running-1) error = %v", err)
	}
	if _, err := s.CreateRun(Run{ID: "running-2", TaskID: task.ID, Provider: "glm", Prompt: "hi", Status: "running", StartedAt: now}); err != nil {
		t.Fatalf("CreateRun(running-2) error = %v", err)
	}
	if _, err := s.CreateRun(Run{ID: "done-1", TaskID: task.ID, Provider: "glm", Prompt: "hi", Status: "done", StartedAt: now}); err != nil {
		t.Fatalf("CreateRun(done-1) error = %v", err)
	}

	n, err := s.MarkRunningRunsInterrupted("interrupted")
	if err != nil {
		t.Fatalf("MarkRunningRunsInterrupted() error = %v", err)
	}
	if n != 2 {
		t.Fatalf("MarkRunningRunsInterrupted() = %d, want 2", n)
	}

	for _, id := range []string{"running-1", "running-2"} {
		r, err := s.GetRun(id)
		if err != nil {
			t.Fatalf("GetRun(%q) error = %v", id, err)
		}
		if r.Status != "interrupted" {
			t.Fatalf("GetRun(%q).Status = %q, want %q", id, r.Status, "interrupted")
		}
	}
	doneRun, err := s.GetRun("done-1")
	if err != nil {
		t.Fatalf("GetRun(done-1) error = %v", err)
	}
	if doneRun.Status != "done" {
		t.Fatalf("GetRun(done-1).Status = %q, want unchanged %q", doneRun.Status, "done")
	}

	// Idempotent: nothing left "running" to mark a second time.
	n2, err := s.MarkRunningRunsInterrupted("interrupted")
	if err != nil {
		t.Fatalf("MarkRunningRunsInterrupted() second call error = %v", err)
	}
	if n2 != 0 {
		t.Fatalf("MarkRunningRunsInterrupted() second call = %d, want 0", n2)
	}
}

func TestStore_RunEvents(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	task := newTestTaskForRuns(t, s)
	if _, err := s.CreateRun(Run{ID: "run-1", TaskID: task.ID, Provider: "glm", Prompt: "hi", Status: "running", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	for i, data := range []string{`{"type":0,"text":"a"}`, `{"type":0,"text":"b"}`, `{"type":1,"stopReason":"end_turn"}`} {
		if _, err := s.AppendRunEvent("run-1", int64(i), data); err != nil {
			t.Fatalf("AppendRunEvent(seq=%d) error = %v", i, err)
		}
	}

	events, err := s.ListRunEvents("run-1")
	if err != nil {
		t.Fatalf("ListRunEvents() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("ListRunEvents() len = %d, want 3", len(events))
	}
	for i, e := range events {
		if e.Seq != int64(i) {
			t.Fatalf("ListRunEvents()[%d].Seq = %d, want %d", i, e.Seq, i)
		}
	}
	if events[0].EventData != `{"type":0,"text":"a"}` {
		t.Fatalf("ListRunEvents()[0].EventData = %q", events[0].EventData)
	}
}

func TestStore_RunsSurviveReopen(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "smind.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	task := newTestTaskForRuns(t, s)
	if _, err := s.CreateRun(Run{ID: "run-1", TaskID: task.ID, Provider: "glm", Prompt: "hi", Status: "running", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if _, err := s.AppendRunEvent("run-1", 0, `{"type":0,"text":"hello"}`); err != nil {
		t.Fatalf("AppendRunEvent() error = %v", err)
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

	got, err := reopened.GetRun("run-1")
	if err != nil {
		t.Fatalf("GetRun() after reopen error = %v", err)
	}
	if got.Status != "running" {
		t.Fatalf("GetRun() after reopen Status = %q, want %q", got.Status, "running")
	}
	events, err := reopened.ListRunEvents("run-1")
	if err != nil {
		t.Fatalf("ListRunEvents() after reopen error = %v", err)
	}
	if len(events) != 1 || events[0].EventData != `{"type":0,"text":"hello"}` {
		t.Fatalf("ListRunEvents() after reopen = %+v", events)
	}
}
