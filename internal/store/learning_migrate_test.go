package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// TestMigrate_AddsLearningTablesAndColumns builds a database with the
// pre-ADR-0026 minimal tables (as a real old smind.db had), opens it twice
// through Open (fresh schema + migrations, idempotency), and asserts the
// learning tables exist and the new columns carry their defaults on
// pre-existing rows.
func TestMigrate_AddsLearningTablesAndColumns(t *testing.T) {
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
		`CREATE TABLE chats (id INTEGER PRIMARY KEY AUTOINCREMENT, task_id INTEGER NOT NULL REFERENCES tasks(id), title TEXT NOT NULL DEFAULT '', provider TEXT, agent_session TEXT, created_at TIMESTAMP NOT NULL, archived_at TIMESTAMP)`,
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
	if _, err := db.Exec(`INSERT INTO chats (id, task_id, created_at) VALUES (1,1,?)`, now); err != nil {
		t.Fatal(err)
	}
	db.Close()

	for i := 0; i < 2; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("Open #%d: %v", i, err)
		}
		for _, table := range []string{
			"learned_skills", "learned_skill_versions", "learned_skill_ledger",
			"learned_skill_proposals", "learning_jobs",
		} {
			var name string
			if err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name); err != nil {
				t.Fatalf("Open #%d: table %s not found: %v", i, table, err)
			}
		}
		var mode string
		if err := s.db.QueryRow(`SELECT learning_mode FROM workspaces WHERE id = 1`).Scan(&mode); err != nil || mode != "off" {
			t.Fatalf("Open #%d: learning_mode = %q err=%v, want 'off'", i, mode, err)
		}
		var wm, pend int
		if err := s.db.QueryRow(`SELECT learn_watermark, learn_pending FROM chats WHERE id = 1`).Scan(&wm, &pend); err != nil || wm != 0 || pend != 0 {
			t.Fatalf("Open #%d: chat watermark/pending = %d/%d err=%v, want 0/0", i, wm, pend, err)
		}
		s.Close()
	}
}

// TestMigrate_PreExistingRowsGetPurposeUserAndKindTask: an old run keeps
// purpose 'user', an old task keeps kind 'task', an old workspace keeps
// learning_mode 'off'. Read with raw queries: the Go structs are not
// extended in this change.
func TestMigrate_PreExistingRowsGetPurposeUserAndKindTask(t *testing.T) {
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
		`INSERT INTO chats (id, task_id, created_at) VALUES (1,1,?)`,
		`INSERT INTO runs (id, task_id, provider, prompt, status, started_at) VALUES ('r1',1,'claude','p','ok',?)`,
	} {
		if _, err := db.Exec(q, now, now); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	var purpose, kind, mode string
	if err := s.db.QueryRow(`SELECT purpose FROM runs WHERE id = 'r1'`).Scan(&purpose); err != nil || purpose != "user" {
		t.Fatalf("run purpose = %q err=%v, want 'user'", purpose, err)
	}
	if err := s.db.QueryRow(`SELECT kind FROM tasks WHERE id = 1`).Scan(&kind); err != nil || kind != "task" {
		t.Fatalf("task kind = %q err=%v, want 'task'", kind, err)
	}
	if err := s.db.QueryRow(`SELECT learning_mode FROM workspaces WHERE id = 1`).Scan(&mode); err != nil || mode != "off" {
		t.Fatalf("workspace learning_mode = %q err=%v, want 'off'", mode, err)
	}
}
