package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestMigrate_BranchDeletedAt_Idempotent(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	for _, q := range []string{
		`CREATE TABLE workspaces (id INTEGER PRIMARY KEY AUTOINCREMENT, path TEXT NOT NULL, title TEXT NOT NULL, routing_policy TEXT NOT NULL, created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL)`,
		`CREATE TABLE tasks (id INTEGER PRIMARY KEY AUTOINCREMENT, workspace_id INTEGER NOT NULL, space_id INTEGER, title TEXT NOT NULL, status TEXT NOT NULL, worktree_path TEXT, branch TEXT, created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL, archived_at TIMESTAMP)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO workspaces VALUES (1,'/r','r','hard',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tasks (id, workspace_id, title, status, created_at, updated_at) VALUES (1,1,'t','created',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.GetTask(1)
	if err != nil {
		t.Fatal(err)
	}
	if task.BranchDeletedAt != nil {
		t.Fatalf("pre-existing task BranchDeletedAt = %v, want nil", task.BranchDeletedAt)
	}
	if _, err := s.SetTaskBranchDeleted(1); err != nil {
		t.Fatal(err)
	}
	s.Close()

	// A repeat Open keeps the stamp and does not fail on the existing column.
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	task, err = s.GetTask(1)
	if err != nil {
		t.Fatal(err)
	}
	if task.BranchDeletedAt == nil {
		t.Fatal("branch_deleted_at lost by a repeat Open")
	}
}

func TestListTasks_ArchivedOnly(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws, _ := s.CreateWorkspace(Workspace{Path: "/r", Title: "r", RoutingPolicy: "hard"})
	other, _ := s.CreateWorkspace(Workspace{Path: "/o", Title: "o", RoutingPolicy: "hard"})
	mk := func(wsID int64, title string) Task {
		t.Helper()
		task, err := s.CreateTask(Task{WorkspaceID: wsID, Title: title, Status: "created"})
		if err != nil {
			t.Fatal(err)
		}
		return task
	}
	live := mk(ws.ID, "live")
	first := mk(ws.ID, "first")
	second := mk(ws.ID, "second")
	foreign := mk(other.ID, "foreign")
	sys, _, err := s.CreateSystemTask(ws.ID, "learning", "/r")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{first.ID, second.ID, foreign.ID, sys.ID} {
		if _, err := s.ArchiveTask(id); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond) // distinct archived_at
	}
	if _, err := s.SetTaskBranchDeleted(second.ID); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListArchivedTasks(ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != second.ID || got[1].ID != first.ID {
		t.Fatalf("archived = %+v, want [second first] (newest archive first, no live/foreign/system)", got)
	}
	if got[0].BranchDeletedAt == nil || got[1].BranchDeletedAt != nil {
		t.Fatalf("BranchDeletedAt = %v / %v, want set on second only", got[0].BranchDeletedAt, got[1].BranchDeletedAt)
	}
	if got[0].ID == live.ID {
		t.Fatal("a live task is listed as archived")
	}
}
