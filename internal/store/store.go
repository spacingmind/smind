// Package store provides SQLite-backed persistence for accounts, routing
// decisions, and quota snapshots.
package store

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

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
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}
