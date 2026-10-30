package store

import "testing"

func TestTaskLifecycle_ArchivedAndLive(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws, err := s.CreateWorkspace(Workspace{Path: "/r", Title: "r", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	live, err := s.CreateTask(Task{WorkspaceID: ws.ID, Title: "live", Status: "created"})
	if err != nil {
		t.Fatal(err)
	}
	closed, err := s.CreateTask(Task{WorkspaceID: ws.ID, Title: "closed", Status: "created"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ArchiveTask(closed.ID); err != nil {
		t.Fatal(err)
	}

	got, err := s.TaskLifecycle()
	if err != nil {
		t.Fatal(err)
	}
	if archived, ok := got[live.ID]; !ok || archived {
		t.Fatalf("live task %d = %v (present %v), want present and false", live.ID, archived, ok)
	}
	if archived, ok := got[closed.ID]; !ok || !archived {
		t.Fatalf("archived task %d = %v (present %v), want present and true", closed.ID, archived, ok)
	}
	if _, ok := got[live.ID+1000]; ok {
		t.Fatalf("unknown id %d present in lifecycle map", live.ID+1000)
	}
}
