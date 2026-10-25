package store

import (
	"testing"
	"time"
)

func stallByID(t *testing.T, rows []StallRow, id int64) StallRow {
	t.Helper()
	for _, r := range rows {
		if r.TaskID == id {
			return r
		}
	}
	t.Fatalf("task %d not in snapshot", id)
	return StallRow{}
}

func TestStallSnapshotOwnFacts(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws := childEventWS(t, s)
	root := childEventTask(t, s, ws, nil)
	child := childEventTask(t, s, ws, &root.ID)
	archived := childEventTask(t, s, ws, nil)
	if _, err := s.ArchiveTask(archived.ID); err != nil {
		t.Fatalf("ArchiveTask() error = %v", err)
	}
	_, err := s.CreateTask(Task{WorkspaceID: ws, Title: "sys", Status: "created", Kind: "system"})
	if err != nil {
		t.Fatalf("CreateTask(system) error = %v", err)
	}

	yes := true
	prof, err := s.CreateAgentProfile(AgentProfile{Name: "lead", Provider: "glm", OnRateLimit: "wait", OrchestrationTools: &yes})
	if err != nil {
		t.Fatalf("CreateAgentProfile() error = %v", err)
	}

	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	reset := base.Add(10 * time.Minute)
	if _, err := s.CreateRun(Run{ID: "r1", TaskID: root.ID, Provider: "glm", Prompt: "p", Status: "running", StartedAt: base}); err != nil {
		t.Fatalf("CreateRun(r1) error = %v", err)
	}
	fin := base.Add(time.Minute)
	if _, err := s.UpdateRunStatus("r1", "done", &fin, "", ""); err != nil {
		t.Fatalf("UpdateRunStatus() error = %v", err)
	}
	later := base.Add(5 * time.Minute)
	if _, err := s.CreateRun(Run{ID: "r2", TaskID: root.ID, Provider: "glm", Prompt: "p", Status: "running", StartedAt: later, ProfileID: prof.ID}); err != nil {
		t.Fatalf("CreateRun(r2) error = %v", err)
	}
	fin2 := later.Add(time.Minute)
	if _, err := s.UpdateRunStatus("r2", "error", &fin2, "", "limit"); err != nil {
		t.Fatalf("UpdateRunStatus(r2) error = %v", err)
	}
	if err := s.UpdateRunFailure("r2", "rate_limited", "5h", &reset, false); err != nil {
		t.Fatalf("UpdateRunFailure() error = %v", err)
	}
	if _, err := s.CreateRun(Run{ID: "c1", TaskID: child.ID, Provider: "glm", Prompt: "p", Status: "running", StartedAt: later}); err != nil {
		t.Fatalf("CreateRun(c1) error = %v", err)
	}

	chat, err := s.CreateChat(root.ID, "q")
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}
	hold := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	item := enqueueAt(t, s, chat.ID, "later", &hold)
	enqueueAt(t, s, chat.ID, "now", nil)

	rows, err := s.StallSnapshot()
	if err != nil {
		t.Fatalf("StallSnapshot() error = %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("snapshot rows = %d, want 2 (archived and system excluded)", len(rows))
	}

	r := stallByID(t, rows, root.ID)
	if r.ParentID != nil || r.Title != "t" {
		t.Errorf("root = parent %v title %q", r.ParentID, r.Title)
	}
	if r.Running {
		t.Error("root Running = true, want false")
	}
	if !r.HasRun || r.LatestStatus != "error" || r.LatestFailureKind != "rate_limited" {
		t.Errorf("root latest run = %+v", r)
	}
	if r.LatestFailureResetAt == nil || !r.LatestFailureResetAt.Equal(reset) {
		t.Errorf("LatestFailureResetAt = %v, want %v", r.LatestFailureResetAt, reset)
	}
	if r.ProfileOnRateLimit != "wait" || !r.ProfileOrchestration {
		t.Errorf("profile facts = %q %v", r.ProfileOnRateLimit, r.ProfileOrchestration)
	}
	if !r.HasQueued || r.QueuedHold == nil || !r.QueuedHold.Equal(hold) {
		t.Errorf("queue facts = %v %v, want hold %v", r.HasQueued, r.QueuedHold, hold)
	}
	// Queue items were created just now, which is newer than every run.
	if r.LastActivity.Before(item.CreatedAt.Add(-time.Second)) {
		t.Errorf("LastActivity = %v, want >= queue item created %v", r.LastActivity, item.CreatedAt)
	}

	c := stallByID(t, rows, child.ID)
	if c.ParentID == nil || *c.ParentID != root.ID {
		t.Errorf("child parent = %v", c.ParentID)
	}
	if !c.Running || !c.LastActivity.Equal(maxTime(later, c.CreatedAt)) {
		t.Errorf("child Running=%v LastActivity=%v, want true max(run start, created) = %v", c.Running, c.LastActivity, maxTime(later, c.CreatedAt))
	}
	if c.HasQueued || c.ProfileOrchestration || c.ProfileOnRateLimit != "" {
		t.Errorf("child unexpected facts %+v", c)
	}
}

func TestMarkStallAlertedRoundTrip(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws := childEventWS(t, s)
	task := childEventTask(t, s, ws, nil)

	rows, err := s.StallSnapshot()
	if err != nil {
		t.Fatalf("StallSnapshot() error = %v", err)
	}
	if got := stallByID(t, rows, task.ID).StallAlertedSince; got != nil {
		t.Fatalf("StallAlertedSince = %v before mark, want nil", got)
	}
	since := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	if err := s.MarkStallAlerted(task.ID, since); err != nil {
		t.Fatalf("MarkStallAlerted() error = %v", err)
	}
	rows, err = s.StallSnapshot()
	if err != nil {
		t.Fatalf("StallSnapshot() error = %v", err)
	}
	got := stallByID(t, rows, task.ID).StallAlertedSince
	if got == nil || !got.Equal(since) {
		t.Fatalf("StallAlertedSince = %v, want %v", got, since)
	}
	if err := s.MarkStallAlerted(9999, since); err == nil {
		t.Error("MarkStallAlerted(missing task) error = nil")
	}
}
