// Package store provides SQLite-backed persistence for accounts, routing
// decisions, and quota snapshots.
package store

import (
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	_ "modernc.org/sqlite"

	"github.com/spacingmind/smind/internal/config"
)

//go:embed schema.sql
var schema string

// Store is a SQLite-backed store for smind's routing data.
type Store struct {
	db     *sql.DB
	closed atomic.Bool
}

// DefaultPath returns the default smind.db location under the smind home dir.
func DefaultPath() string {
	return filepath.Join(config.Dir(), "smind.db")
}

// Open opens (creating if needed) the SQLite database at path and applies the
// schema. Use "file::memory:?cache=shared" or a temp file path for tests.
//
// A file database runs in WAL mode with synchronous=NORMAL (ADR-0034); an
// existing rollback-journal database switches on this first open. See
// journalMode for the one exception.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}
	mode, network := journalMode(path)
	if network {
		log.Printf("store: %s is on a network filesystem; using the rollback journal instead of WAL (slower, and writers may see SQLITE_BUSY under load)", path)
	}
	db, err := sql.Open("sqlite", dsnFor(path, mode))
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
	if mode == "WAL" {
		// SQLite answers an unsupported journal_mode request with the mode it
		// kept, not an error: surface it, the daemon still works in that mode.
		var got string
		if err := db.QueryRow("PRAGMA journal_mode").Scan(&got); err == nil && !strings.EqualFold(got, mode) {
			log.Printf("store: %s: journal_mode is %q, not WAL; continuing without it", path, got)
		}
	}
	return &Store{db: db}, nil
}

// Close checkpoints the write-ahead log into the main database file and closes
// every connection. The checkpoint is what makes smind.db alone a complete
// copy after a clean stop; it is best effort (another process holding a read
// transaction leaves the tail of the log for the next opener), so its failure
// is returned only alongside, never instead of, the close. Closing twice is a
// no-op, as database/sql's own Close is.
func (s *Store) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	_, cpErr := s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return errors.Join(cpErr, s.db.Close())
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

// SynchronousEnv, when set to off, normal, full or extra, overrides SQLite's
// `synchronous` pragma for every connection. The daemon does not set it: unset,
// a WAL database runs NORMAL (ADR-0034), a rollback-journal one SQLite's FULL.
// It is for test runs that want no fsync at all (off) or want to pin a mode.
// Any other value is ignored.
const SynchronousEnv = "SMIND_SQLITE_SYNCHRONOUS"

// isNetworkFS is networkFS (netfs_*.go), a variable so tests can stand in a
// network filesystem.
var isNetworkFS = networkFS

// walJournalSizeLimit caps the -wal file kept on disk after a checkpoint
// (SQLite otherwise leaves it at its high-water mark): 64 MiB.
const walJournalSizeLimit = 64 << 20

// journalMode is the journal mode the store requests for path: "WAL", or
// "DELETE" (the rollback journal SQLite defaults to) for an in-memory database
// and for a database on a network filesystem. WAL needs shared memory and
// byte-range locks that NFS, SMB and 9p do not provide reliably, so there a
// database keeps the rollback journal rather than risk corruption. The fallback
// is logged by Open, which is why network says which case it was.
func journalMode(path string) (mode string, network bool) {
	if isMemoryDSN(path) || strings.HasPrefix(path, "file:") {
		return "DELETE", false
	}
	if isNetworkFS(filepath.Dir(path)) {
		return "DELETE", true
	}
	return "WAL", false
}

// isMemoryDSN reports whether path names an in-memory database.
func isMemoryDSN(path string) bool {
	return strings.Contains(path, ":memory:") || strings.Contains(path, "mode=memory")
}

// sqliteDSN returns path with the pragmas every connection must run: modernc
// applies _pragma parameters, in order, to each connection the pool opens, so
// they hold for all of them and not just the first. busy_timeout comes first so
// that the journal_mode switch of an existing database waits for a lock.
//
// _txlock=immediate makes every db.Begin() a BEGIN IMMEDIATE. A deferred
// transaction that reads first and then writes (the learning store has several)
// fails with SQLITE_BUSY the moment another writer commits in between, and
// busy_timeout never applies to that failure: 16 goroutines running such a
// transaction lost 277-383 of 480 of them, in the rollback journal and in WAL.
// Taking the write lock up front makes them queue under busy_timeout instead.
func sqliteDSN(path string) string {
	mode, _ := journalMode(path)
	return dsnFor(path, mode)
}

func dsnFor(path, mode string) string {
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	dsn := path + separator + "_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_txlock=immediate"
	synchronous := ""
	if mode == "WAL" {
		dsn += "&_pragma=journal_mode(WAL)&_pragma=journal_size_limit(" + fmt.Sprint(walJournalSizeLimit) + ")"
		synchronous = "NORMAL"
	}
	switch mode := strings.ToLower(os.Getenv(SynchronousEnv)); mode {
	case "off", "normal", "full", "extra":
		synchronous = strings.ToUpper(mode)
	}
	if synchronous != "" {
		dsn += "&_pragma=synchronous(" + synchronous + ")"
	}
	return dsn
}
