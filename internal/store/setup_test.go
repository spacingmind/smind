package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestMigrate_SetupColumns(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	for _, q := range []string{
		`CREATE TABLE workspaces (id INTEGER PRIMARY KEY AUTOINCREMENT, path TEXT NOT NULL, title TEXT NOT NULL, routing_policy TEXT NOT NULL, created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL)`,
		`CREATE TABLE tasks (id INTEGER PRIMARY KEY AUTOINCREMENT, workspace_id INTEGER NOT NULL, space_id INTEGER, parent_task_id INTEGER, title TEXT NOT NULL, status TEXT NOT NULL, worktree_path TEXT, branch TEXT, created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL, archived_at TIMESTAMP)`,
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

	for i := 0; i < 2; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("Open #%d: %v", i, err)
		}
		w, err := s.GetWorkspace(1)
		if err != nil || w.SetupScript != "" || w.TeardownScript != "" {
			t.Fatalf("workspace = %+v err=%v", w, err)
		}
		task, err := s.GetTask(1)
		if err != nil || task.SetupStatus != "" || task.Port != nil {
			t.Fatalf("task = %+v err=%v", task, err)
		}
		s.Close()
	}
}

func TestStore_WorkspaceScriptsRoundTrip(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws, _ := s.CreateWorkspace(Workspace{Path: "/r", Title: "r", RoutingPolicy: "hard"})
	a, b := "npm ci", "rm -rf node_modules"
	got, err := s.SetWorkspaceScripts(ws.ID, &a, &b)
	if err != nil || got.SetupScript != a || got.TeardownScript != b {
		t.Fatalf("set = %+v err=%v", got, err)
	}
	// nil keeps, "" clears.
	empty := ""
	got, err = s.SetWorkspaceScripts(ws.ID, nil, &empty)
	if err != nil || got.SetupScript != a || got.TeardownScript != "" {
		t.Fatalf("keep/clear = %+v err=%v", got, err)
	}
	list, _ := s.ListWorkspaces()
	if list[0].SetupScript != a {
		t.Fatalf("list = %+v", list[0])
	}
	task, _ := s.CreateTask(Task{WorkspaceID: ws.ID, Title: "t", Status: "created"})
	task, err = s.UpdateTaskSetup(task.ID, "failed", "boom", "log")
	if err != nil || task.SetupStatus != "failed" || task.SetupError != "boom" || task.SetupLog != "log" {
		t.Fatalf("setup = %+v err=%v", task, err)
	}
	run, _ := s.ListTasksSetupRunning()
	if len(run) != 0 {
		t.Fatalf("running = %v", run)
	}
}

func TestStore_PortUniqueAmongLiveTasks(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws, _ := s.CreateWorkspace(Workspace{Path: "/r", Title: "r", RoutingPolicy: "hard"})
	t1 := newTestTaskInWorkspace(t, s, ws.ID, "a")
	t2 := newTestTaskInWorkspace(t, s, ws.ID, "b")
	if err := s.ClaimTaskPort(t1.ID, 31000); err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimTaskPort(t2.ID, 31000); !errors.Is(err, ErrPortTaken) {
		t.Fatalf("err = %v, want ErrPortTaken", err)
	}
	live, _ := s.ListLivePorts()
	if !live[31000] {
		t.Fatal("port not live")
	}
	if _, err := s.ArchiveTask(t1.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimTaskPort(t2.ID, 31000); err != nil {
		t.Fatalf("after archive: %v", err)
	}
}
