package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// migration describes a single additive schema change needed to bring a
// database created before the change landed in schema.sql up to date.
// Migrations run every time Open applies the schema, after the base
// schema.sql's CREATE TABLE IF NOT EXISTS statements, and must be
// idempotent: safe to run against a database that already has the change
// (because it was created fresh from the current schema.sql) as well as
// one that's missing it (because it predates the change).
//
// This intentionally does not pull in an external migration library --
// schema.sql plus this additive-column mechanism covers the only case
// smind has needed so far (a NOT NULL column with a constant default
// added to an existing table). If a future change needs something this
// can't express (renames, drops, backfills from other tables), extend
// the migration type rather than reaching for a new dependency.
type migration struct {
	name  string
	apply func(*sql.DB) error
}

// migrations lists schema changes that new databases already get from
// schema.sql but pre-existing databases need applied explicitly. Append
// new entries here whenever schema.sql adds a column to an existing
// table; do not remove or reorder existing entries, since a database may
// still be missing an old one.
var migrations = []migration{
	{
		// Added by PR #93 (2026-09-13). Databases created before that PR
		// lack this column, which runs rehydrate reads unconditionally --
		// see docs/plans/active/task-permission-ux.md's Validation notes.
		name: "runs.approval_policy",
		apply: func(db *sql.DB) error {
			return addColumnIfMissing(db, "runs", "approval_policy", "TEXT NOT NULL DEFAULT 'manual'")
		},
	},
	{
		// Added for docs/decisions/0016-multiple-chats-per-task.md. schema.sql
		// already creates the chats table for every database (fresh or
		// pre-existing, via CREATE TABLE IF NOT EXISTS, applied before migrate
		// runs -- see Open); what a pre-existing database still needs is the
		// runs.chat_id column (addColumnIfMissing can't express this one:
		// unlike approval_policy's constant default, each task's default chat
		// id is different, so there's nothing constant to default the column
		// to) plus, for every task, exactly one default chat with every one
		// of that task's chat_id-less runs repointed at it. See
		// backfillDefaultChats.
		name: "chats.default_chat_backfill",
		apply: func(db *sql.DB) error {
			if err := addColumnIfMissing(db, "runs", "chat_id", "INTEGER REFERENCES chats(id)"); err != nil {
				return err
			}
			// Only safe to create once the column above is guaranteed to
			// exist -- see schema.sql's comment on why this index isn't
			// declared there.
			if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_runs_chat_id ON runs(chat_id)`); err != nil {
				return fmt.Errorf("create idx_runs_chat_id: %w", err)
			}
			return backfillDefaultChats(db)
		},
	},
}

// backfillDefaultChats gives every task exactly one default chat
// (provider/agent_session NULL, title "Chat") and repoints every one of
// that task's runs still missing a chat_id at it. Idempotent: a task that
// already has at least one chat (whether from a prior run of this
// migration, or because it was created after chats shipped) is left alone,
// and a run that already has a chat_id is left alone -- so running this
// twice, or against a fresh database with no tasks at all, is a no-op.
// Transactional: either every task gets its default chat and every run
// gets repointed, or (on any failure) none of it is applied, since this
// runs against the user's real smind.db on every daemon startup and a
// half-applied backfill (some tasks migrated, others not) would be worse
// than simply failing to start.
func backfillDefaultChats(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("backfill default chats: begin: %w", err)
	}
	defer tx.Rollback()

	taskRows, err := tx.Query(`SELECT id FROM tasks`)
	if err != nil {
		return fmt.Errorf("backfill default chats: list tasks: %w", err)
	}
	var taskIDs []int64
	for taskRows.Next() {
		var id int64
		if err := taskRows.Scan(&id); err != nil {
			taskRows.Close()
			return fmt.Errorf("backfill default chats: scan task id: %w", err)
		}
		taskIDs = append(taskIDs, id)
	}
	if err := taskRows.Err(); err != nil {
		taskRows.Close()
		return fmt.Errorf("backfill default chats: read task ids: %w", err)
	}
	taskRows.Close()

	for _, taskID := range taskIDs {
		var chatID int64
		err := tx.QueryRow(`SELECT id FROM chats WHERE task_id = ? ORDER BY id LIMIT 1`, taskID).Scan(&chatID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			now := time.Now().UTC()
			res, err := tx.Exec(
				`INSERT INTO chats (task_id, title, provider, agent_session, created_at) VALUES (?, 'Chat', NULL, NULL, ?)`,
				taskID, now,
			)
			if err != nil {
				return fmt.Errorf("backfill default chats: create default chat for task %d: %w", taskID, err)
			}
			chatID, err = res.LastInsertId()
			if err != nil {
				return fmt.Errorf("backfill default chats: default chat id for task %d: %w", taskID, err)
			}
		case err != nil:
			return fmt.Errorf("backfill default chats: find chat for task %d: %w", taskID, err)
		}

		if _, err := tx.Exec(
			`UPDATE runs SET chat_id = ? WHERE task_id = ? AND chat_id IS NULL`, chatID, taskID,
		); err != nil {
			return fmt.Errorf("backfill default chats: repoint runs for task %d: %w", taskID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("backfill default chats: commit: %w", err)
	}
	return nil
}

// migrate applies any pending migrations to db. It runs on every Open;
// each migration is a no-op if its change is already present, so this is
// cheap and safe for fresh and already-migrated databases alike.
func migrate(db *sql.DB) error {
	for _, m := range migrations {
		if err := m.apply(db); err != nil {
			return fmt.Errorf("migration %s: %w", m.name, err)
		}
	}
	return nil
}

// addColumnIfMissing adds column to table with the given type/constraint
// clause (e.g. "TEXT NOT NULL DEFAULT 'manual'") if it isn't already
// present. table and column must be trusted (compile-time) identifiers,
// never user input, since they're interpolated into the SQL text --
// PRAGMA and ALTER TABLE don't support bound parameters for identifiers.
func addColumnIfMissing(db *sql.DB, table, column, ddl string) error {
	rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return fmt.Errorf("inspect %s: %w", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			colType    string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultVal, &pk); err != nil {
			return fmt.Errorf("scan %s columns: %w", table, err)
		}
		if name == column {
			return nil // already present, nothing to do
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read %s columns: %w", table, err)
	}

	if _, err := db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, ddl)); err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	return nil
}
