package store

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

// createTestTaskWithRunData creates a task under workspaceID (and spaceID,
// if non-nil) plus a run, a run_event on that run, and a terminal session,
// so DeleteTask's full cascade (run_events -> runs -> terminal_sessions ->
// tasks) has real child rows to prove it removes.
func createTestTaskWithRunData(t *testing.T, s *Store, workspaceID int64, spaceID *int64, title string) Task {
	t.Helper()
	task, err := s.CreateTask(Task{WorkspaceID: workspaceID, SpaceID: spaceID, Title: title, Status: "created"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	runID := "run-" + title
	if _, err := s.CreateRun(Run{
		ID: runID, TaskID: task.ID, Provider: "glm", Prompt: "hi",
		Status: "done", StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if _, err := s.AppendRunEvent(runID, 0, `{"type":"text"}`); err != nil {
		t.Fatalf("AppendRunEvent() error = %v", err)
	}
	if _, err := s.CreateTerminalSession(TerminalSession{
		ID: "term-" + title, TaskID: task.ID, Status: "running", StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateTerminalSession() error = %v", err)
	}
	return task
}

func TestStore_DeleteTask(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ws, err := s.CreateWorkspace(Workspace{Path: "/repo", Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	task := createTestTaskWithRunData(t, s, ws.ID, nil, "t1")

	if err := s.DeleteTask(task.ID); err != nil {
		t.Fatalf("DeleteTask() error = %v", err)
	}

	if _, err := s.GetTask(task.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetTask() after delete error = %v, want sql.ErrNoRows", err)
	}
	if _, err := s.GetRun("run-t1"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetRun() after delete error = %v, want sql.ErrNoRows", err)
	}
	events, err := s.ListRunEvents("run-t1")
	if err != nil {
		t.Fatalf("ListRunEvents() error = %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("ListRunEvents() after delete = %+v, want empty", events)
	}
	if _, err := s.GetTerminalSession("term-t1"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetTerminalSession() after delete error = %v, want sql.ErrNoRows", err)
	}
}

func TestStore_DeleteTaskMissing(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	if err := s.DeleteTask(999); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("DeleteTask(999) error = %v, want sql.ErrNoRows", err)
	}
}

func TestStore_DeleteSpace(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ws, err := s.CreateWorkspace(Workspace{Path: "/repo", Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	sp, err := s.CreateSpace(Space{WorkspaceID: ws.ID, Title: "feature-x", EnvData: "{}"})
	if err != nil {
		t.Fatalf("CreateSpace() error = %v", err)
	}
	taskInSpace1 := createTestTaskWithRunData(t, s, ws.ID, &sp.ID, "in-space-1")
	taskInSpace2, err := s.CreateTask(Task{WorkspaceID: ws.ID, SpaceID: &sp.ID, Title: "in-space-2", Status: "created"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	// A task elsewhere in the same workspace but not in this space, and a
	// task in a second space, must both survive.
	ungrouped, err := s.CreateTask(Task{WorkspaceID: ws.ID, Title: "ungrouped", Status: "created"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	otherSpace, err := s.CreateSpace(Space{WorkspaceID: ws.ID, Title: "other-space", EnvData: "{}"})
	if err != nil {
		t.Fatalf("CreateSpace() error = %v", err)
	}
	otherSpaceTask, err := s.CreateTask(Task{WorkspaceID: ws.ID, SpaceID: &otherSpace.ID, Title: "other-space-task", Status: "created"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	if err := s.DeleteSpace(sp.ID); err != nil {
		t.Fatalf("DeleteSpace() error = %v", err)
	}

	if _, err := s.GetSpace(sp.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetSpace() after delete error = %v, want sql.ErrNoRows", err)
	}
	if _, err := s.GetTask(taskInSpace1.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetTask(taskInSpace1) after delete error = %v, want sql.ErrNoRows", err)
	}
	if _, err := s.GetTask(taskInSpace2.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetTask(taskInSpace2) after delete error = %v, want sql.ErrNoRows", err)
	}
	if _, err := s.GetRun("run-in-space-1"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetRun() after delete error = %v, want sql.ErrNoRows", err)
	}

	if _, err := s.GetTask(ungrouped.ID); err != nil {
		t.Fatalf("GetTask(ungrouped) after delete error = %v, want nil (untouched)", err)
	}
	if _, err := s.GetTask(otherSpaceTask.ID); err != nil {
		t.Fatalf("GetTask(otherSpaceTask) after delete error = %v, want nil (untouched)", err)
	}
	if _, err := s.GetSpace(otherSpace.ID); err != nil {
		t.Fatalf("GetSpace(otherSpace) after delete error = %v, want nil (untouched)", err)
	}
}

func TestStore_DeleteSpaceMissing(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	if err := s.DeleteSpace(999); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("DeleteSpace(999) error = %v, want sql.ErrNoRows", err)
	}
}

func TestStore_DeleteWorkspace(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ws, err := s.CreateWorkspace(Workspace{Path: "/repo", Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	acct, err := s.CreateAccount(Account{Provider: "anthropic", Label: "personal", CredentialType: "oauth", CredentialData: "x"})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}
	if err := s.AddWorkspaceAccount(ws.ID, acct.ID); err != nil {
		t.Fatalf("AddWorkspaceAccount() error = %v", err)
	}

	spA, err := s.CreateSpace(Space{WorkspaceID: ws.ID, Title: "space-a", EnvData: "{}"})
	if err != nil {
		t.Fatalf("CreateSpace() error = %v", err)
	}
	taskInSpaceA := createTestTaskWithRunData(t, s, ws.ID, &spA.ID, "in-space-a")
	spB, err := s.CreateSpace(Space{WorkspaceID: ws.ID, Title: "space-b", EnvData: "{}"})
	if err != nil {
		t.Fatalf("CreateSpace() error = %v", err)
	}
	taskInSpaceB, err := s.CreateTask(Task{WorkspaceID: ws.ID, SpaceID: &spB.ID, Title: "in-space-b", Status: "created"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	ungrouped, err := s.CreateTask(Task{WorkspaceID: ws.ID, Title: "ungrouped", Status: "created"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	// A second, unrelated workspace with its own account link, space, and
	// task must be completely untouched by deleting the first.
	otherWS, err := s.CreateWorkspace(Workspace{Path: "/repo-other", Title: "other", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	if err := s.AddWorkspaceAccount(otherWS.ID, acct.ID); err != nil {
		t.Fatalf("AddWorkspaceAccount() error = %v", err)
	}
	otherSpace, err := s.CreateSpace(Space{WorkspaceID: otherWS.ID, Title: "other-space", EnvData: "{}"})
	if err != nil {
		t.Fatalf("CreateSpace() error = %v", err)
	}
	otherTask, err := s.CreateTask(Task{WorkspaceID: otherWS.ID, Title: "other-task", Status: "created"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	if err := s.DeleteWorkspace(ws.ID); err != nil {
		t.Fatalf("DeleteWorkspace() error = %v", err)
	}

	if _, err := s.GetWorkspace(ws.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetWorkspace() after delete error = %v, want sql.ErrNoRows", err)
	}
	if _, err := s.GetSpace(spA.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetSpace(spA) after delete error = %v, want sql.ErrNoRows", err)
	}
	if _, err := s.GetSpace(spB.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetSpace(spB) after delete error = %v, want sql.ErrNoRows", err)
	}
	if _, err := s.GetTask(taskInSpaceA.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetTask(taskInSpaceA) after delete error = %v, want sql.ErrNoRows", err)
	}
	if _, err := s.GetTask(taskInSpaceB.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetTask(taskInSpaceB) after delete error = %v, want sql.ErrNoRows", err)
	}
	if _, err := s.GetTask(ungrouped.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetTask(ungrouped) after delete error = %v, want sql.ErrNoRows", err)
	}
	if _, err := s.GetRun("run-in-space-a"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetRun() after delete error = %v, want sql.ErrNoRows", err)
	}
	ids, err := s.ListWorkspaceAccountIDs(ws.ID)
	if err != nil {
		t.Fatalf("ListWorkspaceAccountIDs() error = %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("ListWorkspaceAccountIDs() after delete = %v, want empty", ids)
	}

	// The unrelated workspace and everything under it must be untouched.
	if _, err := s.GetWorkspace(otherWS.ID); err != nil {
		t.Fatalf("GetWorkspace(otherWS) after delete error = %v, want nil (untouched)", err)
	}
	if _, err := s.GetSpace(otherSpace.ID); err != nil {
		t.Fatalf("GetSpace(otherSpace) after delete error = %v, want nil (untouched)", err)
	}
	if _, err := s.GetTask(otherTask.ID); err != nil {
		t.Fatalf("GetTask(otherTask) after delete error = %v, want nil (untouched)", err)
	}
	otherIDs, err := s.ListWorkspaceAccountIDs(otherWS.ID)
	if err != nil {
		t.Fatalf("ListWorkspaceAccountIDs(otherWS) error = %v", err)
	}
	if len(otherIDs) != 1 || otherIDs[0] != acct.ID {
		t.Fatalf("ListWorkspaceAccountIDs(otherWS) after delete = %v, want [%d] (untouched)", otherIDs, acct.ID)
	}
}

func TestStore_DeleteWorkspaceMissing(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	if err := s.DeleteWorkspace(999); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("DeleteWorkspace(999) error = %v, want sql.ErrNoRows", err)
	}
}
