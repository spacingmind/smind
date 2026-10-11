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
	// A pre-#93 glm row migrates to the ACP agent's own default mode, no
	// auto-accept (ADR-0019's migration table for "manual").
	if got.PermissionMode != "" || got.AutoAccept {
		t.Fatalf("GetRun() PermissionMode/AutoAccept = %q/%v, want \"\"/false", got.PermissionMode, got.AutoAccept)
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
	if created.PermissionMode != "" || created.AutoAccept {
		t.Fatalf("CreateRun() PermissionMode/AutoAccept = %q/%v, want unset", created.PermissionMode, created.AutoAccept)
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
		if got.PermissionMode != "" || got.AutoAccept {
			t.Fatalf("GetRun() PermissionMode/AutoAccept = %q/%v, want unset", got.PermissionMode, got.AutoAccept)
		}
	})
}

func assertRunsHasApprovalPolicyColumn(t *testing.T, s *Store) {
	t.Helper()
	for _, col := range []string{"approval_policy", "permission_mode", "auto_accept"} {
		var name string
		err := s.db.QueryRow(`SELECT name FROM pragma_table_info('runs') WHERE name = ?`, col).Scan(&name)
		if err != nil {
			t.Fatalf("runs.%s column not found: %v", col, err)
		}
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

// TestMigrate_PermissionModesBackfill is S13: every legacy approval_policy
// x provider combination on both runs and agent_profiles maps to
// ADR-0019's migration table (always toward asking a human; only
// full-access becomes an auto-approving setting), and re-opening the
// database changes nothing.
func TestMigrate_PermissionModesBackfill(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "legacy-policies.db")

	// A database from just before ADR-0019: today's schema minus the new
	// columns. Built by opening fresh, then dropping them.
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	task := newTestTaskForRuns(t, s)
	for _, stmt := range []string{
		`ALTER TABLE runs DROP COLUMN permission_mode`, `ALTER TABLE runs DROP COLUMN auto_accept`,
		`ALTER TABLE agent_profiles DROP COLUMN permission_mode`, `ALTER TABLE agent_profiles DROP COLUMN auto_accept`,
	} {
		if _, err := s.db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	type want struct {
		mode       string
		autoAccept bool
	}
	cases := map[string]want{}
	now := time.Now().UTC()
	for _, provider := range []string{"claude-native", "codex-native", "glm", "kimi"} {
		for _, policy := range []string{"manual", "auto-safe", "full-access", ""} {
			var w want
			switch {
			case provider == "claude-native" && policy == "full-access":
				w = want{"bypassPermissions", false}
			case provider == "claude-native":
				w = want{"acceptEdits", false}
			case provider == "codex-native" && policy == "full-access":
				w = want{"full-access", false}
			case provider == "codex-native":
				w = want{"auto", false}
			case policy == "full-access":
				w = want{"", true}
			}
			id := provider + "/" + policy
			cases[id] = w
			// A legacy run row's approval_policy was never empty (column
			// default 'manual', and CreateRun coerced '' to it), so the ''
			// case only exists for profiles ("unset").
			if policy != "" {
				if _, err := s.db.Exec(`INSERT INTO runs (id, task_id, provider, prompt, status, started_at, approval_policy) VALUES (?, ?, ?, 'p', 'done', ?, ?)`,
					id, task.ID, provider, now, policy); err != nil {
					t.Fatalf("seed run %s: %v", id, err)
				}
			}
			if _, err := s.db.Exec(`INSERT INTO agent_profiles (name, provider, approval_policy, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
				id, provider, policy, now, now); err != nil {
				t.Fatalf("seed profile %s: %v", id, err)
			}
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	for pass := 0; pass < 2; pass++ {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("Open() pass %d error = %v", pass, err)
		}
		profiles, err := s.ListAgentProfiles()
		if err != nil {
			t.Fatalf("ListAgentProfiles() error = %v", err)
		}
		byName := map[string]AgentProfile{}
		for _, p := range profiles {
			byName[p.Name] = p
		}
		for id, w := range cases {
			if id[len(id)-1] != '/' {
				got, err := s.GetRun(id)
				if err != nil {
					t.Fatalf("GetRun(%s) error = %v", id, err)
				}
				if got.PermissionMode != w.mode || got.AutoAccept != w.autoAccept {
					t.Errorf("pass %d run %s = %q/%v, want %q/%v", pass, id, got.PermissionMode, got.AutoAccept, w.mode, w.autoAccept)
				}
			}
			p := byName[id]
			pw := w
			if id[len(id)-1] == '/' {
				pw = want{} // an unset profile policy stays unset
			}
			if p.PermissionMode != pw.mode || p.AutoAccept != pw.autoAccept {
				t.Errorf("pass %d profile %s = %q/%v, want %q/%v", pass, id, p.PermissionMode, p.AutoAccept, pw.mode, pw.autoAccept)
			}
		}
		if err := s.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}
}

// TestMigrate_PermissionModesBackfillRunsOnce is the regression test for
// the backfill re-running on every Open (approval_policy was never
// cleared): after migration, a user's edit to a legacy row -- unticking a
// migrated full-access GLM profile's autoAccept, resetting a migrated
// Claude bypass profile to the provider default "", or turning a legacy
// GLM run's autoAccept off -- must survive the next Open.
func TestMigrate_PermissionModesBackfillRunsOnce(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "legacy-edit.db")

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	task := newTestTaskForRuns(t, s)
	for _, stmt := range []string{
		`ALTER TABLE runs DROP COLUMN permission_mode`, `ALTER TABLE runs DROP COLUMN auto_accept`,
		`ALTER TABLE agent_profiles DROP COLUMN permission_mode`, `ALTER TABLE agent_profiles DROP COLUMN auto_accept`,
	} {
		if _, err := s.db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	now := time.Now().UTC()
	if _, err := s.db.Exec(`INSERT INTO agent_profiles (id, name, provider, approval_policy, created_at, updated_at) VALUES
		(1, 'glm-full', 'glm', 'full-access', ?, ?), (2, 'claude-full', 'claude-native', 'full-access', ?, ?)`, now, now, now, now); err != nil {
		t.Fatalf("seed profiles: %v", err)
	}
	if _, err := s.db.Exec(`INSERT INTO runs (id, task_id, provider, prompt, status, started_at, approval_policy) VALUES ('r1', ?, 'glm', 'p', 'done', ?, 'full-access')`, task.ID, now); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// First Open migrates; then the user edits the migrated rows.
	s, err = Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	glm, _ := s.GetAgentProfile(1)
	claude, _ := s.GetAgentProfile(2)
	if !glm.AutoAccept || claude.PermissionMode != "bypassPermissions" {
		t.Fatalf("migrated = %+v / %+v, want glm autoAccept and claude bypass", glm, claude)
	}
	glm.AutoAccept = false
	claude.PermissionMode = ""
	for _, p := range []AgentProfile{glm, claude} {
		if _, err := s.UpdateAgentProfile(p); err != nil {
			t.Fatalf("UpdateAgentProfile(%s) error = %v", p.Name, err)
		}
	}
	if _, err := s.db.Exec(`UPDATE runs SET auto_accept = 0 WHERE id = 'r1'`); err != nil {
		t.Fatalf("edit run: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Every later Open must leave the edits alone.
	for pass := 0; pass < 2; pass++ {
		s, err = Open(path)
		if err != nil {
			t.Fatalf("reopen %d error = %v", pass, err)
		}
		glm, _ = s.GetAgentProfile(1)
		claude, _ = s.GetAgentProfile(2)
		run, _ := s.GetRun("r1")
		if glm.AutoAccept || claude.PermissionMode != "" || run.AutoAccept {
			t.Fatalf("reopen %d re-applied the backfill: glm autoAccept=%v claude mode=%q run autoAccept=%v",
				pass, glm.AutoAccept, claude.PermissionMode, run.AutoAccept)
		}
		if err := s.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}
}

// TestMigrate_AccountsModelsColumn: a database whose accounts table predates
// accounts.models gets the column on Open, and its old rows read back as
// "no list".
func TestMigrate_AccountsModelsColumn(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "old-accounts.db")
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatalf("open raw db error = %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := db.Exec(`CREATE TABLE accounts (
		id INTEGER PRIMARY KEY AUTOINCREMENT, provider TEXT NOT NULL, label TEXT NOT NULL,
		credential_type TEXT NOT NULL, credential_data TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL)`); err != nil {
		t.Fatalf("create old accounts table: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO accounts (id, provider, label, credential_type, credential_data, created_at, updated_at)
		 VALUES (1, 'anthropic', 'old', 'api_key', '{"key":"k"}', ?, ?)`, now, now,
	); err != nil {
		t.Fatalf("seed old account: %v", err)
	}
	db.Close()

	for i := 0; i < 2; i++ { // second Open proves idempotence
		s, err := Open(path)
		if err != nil {
			t.Fatalf("Open() #%d error = %v", i, err)
		}
		got, err := s.GetAccount(1)
		if err != nil {
			t.Fatalf("GetAccount() error = %v", err)
		}
		if got.Models != "" || got.Label != "old" {
			t.Fatalf("migrated account = %+v, want no models", got)
		}
		s.Close()
	}
}
