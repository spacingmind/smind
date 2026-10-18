// Package store provides SQLite-backed persistence for accounts, routing
// decisions, and quota snapshots.
package store

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/spacingmind/smind/internal/config"
)

//go:embed schema.sql
var schema string

// Store is a SQLite-backed store for smind's routing data.
type Store struct {
	db *sql.DB
}

// DefaultPath returns the default smind.db location under the smind home dir.
func DefaultPath() string {
	return filepath.Join(config.Dir(), "smind.db")
}

// Open opens (creating if needed) the SQLite database at path and applies the
// schema. Use "file::memory:?cache=shared" or a temp file path for tests.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := applySchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// applySchema runs schema.sql in ONE transaction. SQLite commits every statement
// of an autocommit script separately, and each commit is several fsyncs and a
// journal file created and deleted: about 40 commits for a fresh database, which
// cost 0.8 s on a Windows CI disk (C:, ~20 ms per commit) against 0.03 s as one
// transaction. It is also atomic: a failure leaves no half-created schema.
func applySchema(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(schema); err != nil {
		return err
	}
	return tx.Commit()
}

// SynchronousEnv, when set to off, normal, full or extra, sets SQLite's
// `synchronous` pragma for every connection. It exists for test runs on disks
// where a durable commit is slow (a Windows CI runner's C: drive: ~20 ms per
// commit, so the DB-heavy test packages needed minutes and hit SQLITE_BUSY under
// concurrent writers) and is NOT set by the daemon: unset, SQLite's default
// (FULL) applies, exactly as before. Any other value is ignored.
const SynchronousEnv = "SMIND_SQLITE_SYNCHRONOUS"

func sqliteDSN(path string) string {
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	dsn := path + separator + "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	switch mode := strings.ToLower(os.Getenv(SynchronousEnv)); mode {
	case "off", "normal", "full", "extra":
		dsn += "&_pragma=synchronous(" + strings.ToUpper(mode) + ")"
	}
	return dsn
}
