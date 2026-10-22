package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestMigrate_TaskUntrustedSource_Idempotent(t *testing.T) {
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
	if task.UntrustedSource {
		t.Fatal("pre-existing task untrusted after migration, want trusted")
	}
	if _, err := s.db.Exec(`UPDATE tasks SET untrusted_source = 1 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	s.Close()

	// Re-open: the flag set between opens survives a repeat migration.
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	task, err = s.GetTask(1)
	if err != nil {
		t.Fatal(err)
	}
	if !task.UntrustedSource {
		t.Fatal("untrusted_source reset by a repeat Open")
	}
}

func TestCreateTask_UntrustedAndPRIdentityRoundTrip(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws, _ := s.CreateWorkspace(Workspace{Path: "/r", Title: "r", RoutingPolicy: "hard"})
	task, err := s.CreateTask(Task{
		WorkspaceID: ws.ID, Title: "fork", Status: "created", UntrustedSource: true,
		PRForge: "github", PRRepo: "o/r", PRNumber: 7, PRURL: "https://github.com/o/r/pull/7", PRHeadRef: "feat/x",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.UntrustedSource || got.PRNumber != 7 || got.PRRepo != "o/r" || got.PRHeadRef != "feat/x" || got.PRForge != "github" {
		t.Fatalf("round trip = %+v", got)
	}
	plain, err := s.CreateTask(Task{WorkspaceID: ws.ID, Title: "plain", Status: "created"})
	if err != nil {
		t.Fatal(err)
	}
	if g, _ := s.GetTask(plain.ID); g.UntrustedSource || g.PRNumber != 0 || g.PRRepo != "" {
		t.Fatalf("plain task = %+v, want trusted with no PR", g)
	}
}

func TestBeginTaskSetup_ClearsUntrusted_OnlyThatPath(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws, _ := s.CreateWorkspace(Workspace{Path: "/r", Title: "r", RoutingPolicy: "hard"})
	task, err := s.CreateTask(Task{WorkspaceID: ws.ID, Title: "fork", Status: "created", UntrustedSource: true})
	if err != nil {
		t.Fatal(err)
	}
	// Nothing but the user's retry clears the flag: status writes keep it.
	if _, err := s.UpdateTaskSetup(task.ID, "blocked", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateTaskSetup(task.ID, "failed", "e", "l"); err != nil {
		t.Fatal(err)
	}
	if g, _ := s.GetTask(task.ID); !g.UntrustedSource {
		t.Fatal("UpdateTaskSetup cleared untrusted_source")
	}
	if ok, err := s.BeginTaskSetup(task.ID); err != nil || !ok {
		t.Fatalf("begin = %v, %v", ok, err)
	}
	if g, _ := s.GetTask(task.ID); g.UntrustedSource {
		t.Fatal("BeginTaskSetup left untrusted_source set")
	}
}

func TestClearTaskUntrusted(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws, _ := s.CreateWorkspace(Workspace{Path: "/r", Title: "r", RoutingPolicy: "hard"})
	task, _ := s.CreateTask(Task{WorkspaceID: ws.ID, Title: "fork", Status: "created", UntrustedSource: true})
	if ok, err := s.ClearTaskUntrusted(task.ID); err != nil || !ok {
		t.Fatalf("clear = %v, %v", ok, err)
	}
	if ok, err := s.ClearTaskUntrusted(task.ID); err != nil || ok {
		t.Fatalf("second clear = %v, %v, want no change", ok, err)
	}
}
