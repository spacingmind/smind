package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestMigrate_TaskBaseColumns_Idempotent(t *testing.T) {
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

	for i := 0; i < 2; i++ { // second Open must change nothing
		s, err := Open(path)
		if err != nil {
			t.Fatalf("Open #%d: %v", i, err)
		}
		task, err := s.GetTask(1)
		if err != nil {
			t.Fatal(err)
		}
		if task.BaseRef != "" || task.BaseSha != "" || task.BranchOwned {
			t.Fatalf("pre-existing task base = %+v, want empty", task)
		}
		ws, err := s.GetWorkspace(1)
		if err != nil || ws.DefaultBase != "" {
			t.Fatalf("workspace default base = %q, %v", ws.DefaultBase, err)
		}
		s.Close()
	}
}

func TestTaskBase_RoundTrip(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws, err := s.CreateWorkspace(Workspace{Path: "/r", Title: "r", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.CreateTask(Task{WorkspaceID: ws.ID, Title: "t", Status: "created", BaseRef: "main", BaseSha: "abc", BranchOwned: true})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetTask(task.ID)
	if got.BaseRef != "main" || got.BaseSha != "abc" || !got.BranchOwned {
		t.Fatalf("got %+v", got)
	}
	if err := s.SetTaskBase(task.ID, "develop", "def"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetTask(task.ID)
	if got.BaseRef != "develop" || got.BaseSha != "def" || !got.BranchOwned {
		t.Fatalf("after SetTaskBase %+v", got)
	}
	if err := s.SetWorkspaceDefaultBase(ws.ID, "trunk"); err != nil {
		t.Fatal(err)
	}
	if w, _ := s.GetWorkspace(ws.ID); w.DefaultBase != "trunk" {
		t.Fatalf("default base %q", w.DefaultBase)
	}
	if err := s.SetWorkspaceDefaultBase(ws.ID, ""); err != nil {
		t.Fatal(err)
	}
	if w, _ := s.GetWorkspace(ws.ID); w.DefaultBase != "" {
		t.Fatalf("default base not cleared: %q", w.DefaultBase)
	}
}
