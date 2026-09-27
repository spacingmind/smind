package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// preApprovalPolicySchema recreates the runs table shape from before PR #93
// added approval_policy -- just enough of the surrounding schema (workspaces,
// tasks) for runs.task_id's foreign key to resolve. This is what a real
// pre-#93 smind.db looked like on disk.
const preApprovalPolicySchema = `
CREATE TABLE workspaces (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    path TEXT NOT NULL,
    title TEXT NOT NULL,
    routing_policy TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE TABLE tasks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id INTEGER NOT NULL REFERENCES workspaces(id),
    space_id INTEGER,
    title TEXT NOT NULL,
    status TEXT NOT NULL,
    worktree_path TEXT,
    branch TEXT,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    archived_at TIMESTAMP
);

CREATE TABLE runs (
    id TEXT PRIMARY KEY,
    task_id INTEGER NOT NULL REFERENCES tasks(id),
    provider TEXT NOT NULL,
    prompt TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at TIMESTAMP NOT NULL,
    finished_at TIMESTAMP,
    stop_reason TEXT NOT NULL DEFAULT '',
    err_msg TEXT NOT NULL DEFAULT ''
);
`

// TestMigrate_AddsApprovalPolicyToPreExistingRunsTable is the regression test
// for the 2026-09-13 boot failure: a database created before PR #93 lacks
// runs.approval_policy, and Open must add it (preserving existing rows)
// rather than requiring the manual ALTER TABLE that patched it live.
func TestMigrate_AddsApprovalPolicyToPreExistingRunsTable(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "pre-93.db")
	seedPreApprovalPolicyDB(t, path)

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() on pre-#93 database error = %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	// The pre-existing row (created before the column existed) must read
	// back with the column's default, not an error or a NULL.
	got, err := s.GetRun("old-run")
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if got.ApprovalPolicy != "manual" {
		t.Fatalf("GetRun() ApprovalPolicy = %q, want %q", got.ApprovalPolicy, "manual")
	}
	if got.Provider != "glm" || got.Prompt != "hi" || got.Status != "done" {
		t.Fatalf("GetRun() = %+v, old row data not preserved", got)
	}

	// A newly inserted run must also see the column (with its default when
	// unset), exercising the same path CreateRun/GetRun use elsewhere.
	task := newTestTaskForRuns(t, s)
	created, err := s.CreateRun(Run{
		ID: "new-run", TaskID: task.ID, Provider: "glm", Prompt: "hi",
		Status: "running", StartedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("CreateRun() after migration error = %v", err)
	}
	if created.ApprovalPolicy != "manual" {
		t.Fatalf("CreateRun() ApprovalPolicy = %q, want %q", created.ApprovalPolicy, "manual")
	}
}

// TestMigrate_IdempotentAcrossRepeatedOpen proves migrating an already-
// migrated database (either a fresh one made from today's schema.sql, or one
// that already went through the pre-#93 migration once) is a safe no-op on
// every subsequent Open -- migrate() must run on every boot, not just the
// first.
func TestMigrate_IdempotentAcrossRepeatedOpen(t *testing.T) {
	t.Parallel()

	t.Run("fresh database", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "fresh.db")

		for i := 0; i < 3; i++ {
			s, err := Open(path)
			if err != nil {
				t.Fatalf("Open() call %d error = %v", i, err)
			}
			if err := s.Close(); err != nil {
				t.Fatalf("Close() call %d error = %v", i, err)
			}
		}

		s, err := Open(path)
		if err != nil {
			t.Fatalf("final Open() error = %v", err)
		}
		t.Cleanup(func() {
			if err := s.Close(); err != nil {
				t.Errorf("Close() error = %v", err)
			}
		})
		assertRunsHasApprovalPolicyColumn(t, s)
	})

	t.Run("pre-existing database migrated repeatedly", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "pre-93-repeat.db")
		seedPreApprovalPolicyDB(t, path)

		for i := 0; i < 3; i++ {
			s, err := Open(path)
			if err != nil {
				t.Fatalf("Open() call %d error = %v", i, err)
			}
			if err := s.Close(); err != nil {
				t.Fatalf("Close() call %d error = %v", i, err)
			}
		}

		s, err := Open(path)
		if err != nil {
			t.Fatalf("final Open() error = %v", err)
		}
		t.Cleanup(func() {
			if err := s.Close(); err != nil {
				t.Errorf("Close() error = %v", err)
			}
		})
		assertRunsHasApprovalPolicyColumn(t, s)

		got, err := s.GetRun("old-run")
		if err != nil {
			t.Fatalf("GetRun() error = %v", err)
		}
		if got.ApprovalPolicy != "manual" {
			t.Fatalf("GetRun() ApprovalPolicy = %q, want %q", got.ApprovalPolicy, "manual")
		}
	})
}

func assertRunsHasApprovalPolicyColumn(t *testing.T, s *Store) {
	t.Helper()
	var name string
	err := s.db.QueryRow(`SELECT name FROM pragma_table_info('runs') WHERE name = 'approval_policy'`).Scan(&name)
	if err != nil {
		t.Fatalf("runs.approval_policy column not found: %v", err)
	}
}

// preChatsSchema recreates the schema shape from immediately before
// docs/decisions/0016-multiple-chats-per-task.md: no chats table, no
// runs.chat_id -- otherwise identical to today's schema.sql (including
// approval_policy, so this exercises the chats migration in isolation from
// the older approval_policy one). This is what a real pre-chats smind.db
// looked like on disk.
const preChatsSchema = `
CREATE TABLE workspaces (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    path TEXT NOT NULL,
    title TEXT NOT NULL,
    routing_policy TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE TABLE tasks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id INTEGER NOT NULL REFERENCES workspaces(id),
    space_id INTEGER,
    title TEXT NOT NULL,
    status TEXT NOT NULL,
    worktree_path TEXT,
    branch TEXT,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    archived_at TIMESTAMP
);

CREATE TABLE runs (
    id TEXT PRIMARY KEY,
    task_id INTEGER NOT NULL REFERENCES tasks(id),
    provider TEXT NOT NULL,
    prompt TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at TIMESTAMP NOT NULL,
    finished_at TIMESTAMP,
    stop_reason TEXT NOT NULL DEFAULT '',
    err_msg TEXT NOT NULL DEFAULT '',
    approval_policy TEXT NOT NULL DEFAULT 'manual'
);
`

// seedPreChatsDB creates a database file at path with the pre-chats schema,
// two tasks, and five runs split across them (three for the first, two for
// the second) -- the exact shape the plan's migration test scenario names:
// "a DB with 2 tasks and 5 runs gets 2 default chats with every run linked".
func seedPreChatsDB(t *testing.T, path string) (task1ID, task2ID int64) {
	t.Helper()

	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatalf("open raw db error = %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(preChatsSchema); err != nil {
		t.Fatalf("apply pre-chats schema error = %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	if _, err := db.Exec(
		`INSERT INTO workspaces (id, path, title, routing_policy, created_at, updated_at) VALUES (1, '/repo', 'repo', 'hard', ?, ?)`,
		now, now,
	); err != nil {
		t.Fatalf("seed workspace error = %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO tasks (id, workspace_id, title, status, created_at, updated_at) VALUES (1, 1, 'task one', 'done', ?, ?)`,
		now, now,
	); err != nil {
		t.Fatalf("seed task 1 error = %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO tasks (id, workspace_id, title, status, created_at, updated_at) VALUES (2, 1, 'task two', 'done', ?, ?)`,
		now, now,
	); err != nil {
		t.Fatalf("seed task 2 error = %v", err)
	}

	runTasks := []int64{1, 1, 1, 2, 2}
	for i, taskID := range runTasks {
		if _, err := db.Exec(
			`INSERT INTO runs (id, task_id, provider, prompt, status, started_at) VALUES (?, ?, 'glm', 'hi', 'done', ?)`,
			fmt.Sprintf("run-%d", i), taskID, now,
		); err != nil {
			t.Fatalf("seed run %d error = %v", i, err)
		}
	}

	return 1, 2
}

// TestMigrate_BackfillsDefaultChatsForPreExistingTasksAndRuns is the
// migration test scenario named in the plan: "a DB with 2 tasks and 5 runs
// gets 2 default chats with every run linked".
func TestMigrate_BackfillsDefaultChatsForPreExistingTasksAndRuns(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "pre-chats.db")
	task1ID, task2ID := seedPreChatsDB(t, path)

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() on pre-chats database error = %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	for _, taskID := range []int64{task1ID, task2ID} {
		chats, err := s.ListChatsByTask(taskID, true)
		if err != nil {
			t.Fatalf("ListChatsByTask(%d) error = %v", taskID, err)
		}
		if len(chats) != 1 {
			t.Fatalf("ListChatsByTask(%d) = %+v, want exactly 1 default chat", taskID, chats)
		}
		if chats[0].Title != "Chat" {
			t.Fatalf("default chat title = %q, want %q", chats[0].Title, "Chat")
		}
		if chats[0].Provider != nil {
			t.Fatalf("default chat Provider = %v, want nil", chats[0].Provider)
		}
	}

	task1Chats, _ := s.ListChatsByTask(task1ID, true)
	task2Chats, _ := s.ListChatsByTask(task2ID, true)

	wantChatID := map[string]int64{
		"run-0": task1Chats[0].ID, "run-1": task1Chats[0].ID, "run-2": task1Chats[0].ID,
		"run-3": task2Chats[0].ID, "run-4": task2Chats[0].ID,
	}
	for runID, wantID := range wantChatID {
		got, err := s.GetRun(runID)
		if err != nil {
			t.Fatalf("GetRun(%q) error = %v", runID, err)
		}
		if got.ChatID != wantID {
			t.Fatalf("GetRun(%q).ChatID = %d, want %d (its task's default chat)", runID, got.ChatID, wantID)
		}
	}
}

// TestMigrate_ChatsBackfillIdempotentAcrossRepeatedOpen proves re-running
// the chats backfill migration (either against a fresh schema.sql database
// or one that already went through the pre-chats backfill once) is a safe
// no-op -- no duplicate default chats, no runs repointed away from their
// already-assigned chat.
func TestMigrate_ChatsBackfillIdempotentAcrossRepeatedOpen(t *testing.T) {
	t.Parallel()

	t.Run("fresh database", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "fresh-chats.db")

		for i := 0; i < 3; i++ {
			s, err := Open(path)
			if err != nil {
				t.Fatalf("Open() call %d error = %v", i, err)
			}
			if err := s.Close(); err != nil {
				t.Fatalf("Close() call %d error = %v", i, err)
			}
		}

		s, err := Open(path)
		if err != nil {
			t.Fatalf("final Open() error = %v", err)
		}
		t.Cleanup(func() {
			if err := s.Close(); err != nil {
				t.Errorf("Close() error = %v", err)
			}
		})

		var count int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM chats`).Scan(&count); err != nil {
			t.Fatalf("count chats error = %v", err)
		}
		if count != 0 {
			t.Fatalf("chats count on a fresh, task-less database = %d, want 0", count)
		}
	})

	t.Run("pre-existing database migrated repeatedly", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "pre-chats-repeat.db")
		task1ID, task2ID := seedPreChatsDB(t, path)

		for i := 0; i < 3; i++ {
			s, err := Open(path)
			if err != nil {
				t.Fatalf("Open() call %d error = %v", i, err)
			}
			if err := s.Close(); err != nil {
				t.Fatalf("Close() call %d error = %v", i, err)
			}
		}

		s, err := Open(path)
		if err != nil {
			t.Fatalf("final Open() error = %v", err)
		}
		t.Cleanup(func() {
			if err := s.Close(); err != nil {
				t.Errorf("Close() error = %v", err)
			}
		})

		var count int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM chats`).Scan(&count); err != nil {
			t.Fatalf("count chats error = %v", err)
		}
		if count != 2 {
			t.Fatalf("chats count after repeated migration = %d, want 2 (one per task, not duplicated)", count)
		}

		for _, taskID := range []int64{task1ID, task2ID} {
			chats, err := s.ListChatsByTask(taskID, true)
			if err != nil {
				t.Fatalf("ListChatsByTask(%d) error = %v", taskID, err)
			}
			if len(chats) != 1 {
				t.Fatalf("ListChatsByTask(%d) = %+v, want exactly 1 chat", taskID, chats)
			}
		}
	})
}

// seedPreApprovalPolicyDB creates a database file at path with the pre-#93
// schema (no runs.approval_policy) and one pre-existing run row, using a raw
// connection rather than Store/Open -- Open is the thing under test.
func seedPreApprovalPolicyDB(t *testing.T, path string) {
	t.Helper()

	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatalf("open raw db error = %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(preApprovalPolicySchema); err != nil {
		t.Fatalf("apply pre-#93 schema error = %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	if _, err := db.Exec(
		`INSERT INTO workspaces (id, path, title, routing_policy, created_at, updated_at) VALUES (1, '/repo', 'repo', 'hard', ?, ?)`,
		now, now,
	); err != nil {
		t.Fatalf("seed workspace error = %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO tasks (id, workspace_id, title, status, created_at, updated_at) VALUES (1, 1, 'do the thing', 'done', ?, ?)`,
		now, now,
	); err != nil {
		t.Fatalf("seed task error = %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO runs (id, task_id, provider, prompt, status, started_at) VALUES ('old-run', 1, 'glm', 'hi', 'done', ?)`,
		now,
	); err != nil {
		t.Fatalf("seed pre-#93 run error = %v", err)
	}
}
