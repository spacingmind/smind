package store

import (
	"database/sql"
	"fmt"
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
