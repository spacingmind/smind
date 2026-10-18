package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestRuns_OriginRoundTrip(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	task := newTestTaskForRuns(t, s)

	for _, c := range []struct {
		id        string
		origin    string
		interrupt bool
	}{
		{"o-human", ChatQueueSourceHuman, false},
		{"o-agent-int", ChatQueueSourceAgent, true},
		{"o-unknown", "", false},
	} {
		if _, err := s.CreateRun(Run{
			ID: c.id, TaskID: task.ID, Provider: "glm", Prompt: "p", Status: "running",
			StartedAt: time.Now().UTC(), Origin: c.origin, Interrupt: c.interrupt,
		}); err != nil {
			t.Fatalf("CreateRun(%s): %v", c.id, err)
		}
		got, err := s.GetRun(c.id)
		if err != nil {
			t.Fatal(err)
		}
		if got.Origin != c.origin || got.Interrupt != c.interrupt {
			t.Fatalf("%s: origin/interrupt = %q/%v, want %q/%v", c.id, got.Origin, got.Interrupt, c.origin, c.interrupt)
		}
	}
	recent, err := s.ListRecentRuns(10)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Run{}
	for _, r := range recent {
		byID[r.ID] = r
	}
	if r := byID["o-agent-int"]; r.Origin != ChatQueueSourceAgent || !r.Interrupt {
		t.Fatalf("ListRecentRuns lost origin/interrupt: %+v", r)
	}
}

func TestMigrate_PreExistingRunsHaveEmptyOrigin(t *testing.T) {
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
		`CREATE TABLE chats (id INTEGER PRIMARY KEY AUTOINCREMENT, task_id INTEGER NOT NULL REFERENCES tasks(id), title TEXT NOT NULL DEFAULT '', provider TEXT, agent_session TEXT, created_at TIMESTAMP NOT NULL, archived_at TIMESTAMP)`,
		`CREATE TABLE runs (id TEXT PRIMARY KEY, task_id INTEGER NOT NULL REFERENCES tasks(id), provider TEXT NOT NULL, prompt TEXT NOT NULL, status TEXT NOT NULL, started_at TIMESTAMP NOT NULL, finished_at TIMESTAMP)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{
		`INSERT INTO workspaces VALUES (1,'/r','r','hard',?,?)`,
		`INSERT INTO tasks (id, workspace_id, title, status, created_at, updated_at) VALUES (1,1,'t','created',?,?)`,
		`INSERT INTO runs (id, task_id, provider, prompt, status, started_at) VALUES ('r1',1,'claude','p','ok',?)`,
	} {
		args := []any{now, now}
		if _, err := db.Exec(q, args[:countArgs(q)]...); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var origin string
	var interrupt int
	if err := s.db.QueryRow(`SELECT origin, interrupt FROM runs WHERE id = 'r1'`).Scan(&origin, &interrupt); err != nil {
		t.Fatal(err)
	}
	if origin != "" || interrupt != 0 {
		t.Fatalf("origin/interrupt = %q/%d, want ''/0", origin, interrupt)
	}
}

func countArgs(q string) int {
	n := 0
	for _, c := range q {
		if c == '?' {
			n++
		}
	}
	return n
}
