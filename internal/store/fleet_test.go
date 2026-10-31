package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func openFleetStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// fleetTables is the closed set of fleet tables (ADR-0048 F1a): the store
// holds exactly these and nothing shaped like a mesh or cluster registry.
var fleetTables = []string{
	"fleet_hosts", "fleet_links", "fleet_audit",
	"fleet_watches", "fleet_idem", "fleet_remote_tasks",
}

func tableExists(t *testing.T, s *Store, table string) bool {
	t.Helper()
	var name string
	err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return false
	}
	if err != nil {
		t.Fatal(err)
	}
	return true
}

func columnExists(t *testing.T, s *Store, table, column string) bool {
	t.Helper()
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		if name == column {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return false
}

func TestFleetMigration_TablesAndColumns(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "smind.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	for _, table := range fleetTables {
		if !tableExists(t, s, table) {
			t.Fatalf("table %s missing", table)
		}
	}
	for _, c := range []struct{ table, column string }{
		{"tasks", "origin_host"},
		{"tasks", "origin_trace"},
		{"runs", "origin_host"},
	} {
		if !columnExists(t, s, c.table, c.column) {
			t.Fatalf("column %s.%s missing", c.table, c.column)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Re-opening the same file must succeed: every statement is IF NOT
	// EXISTS / addColumnIfMissing, so the second pass is a no-op.
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	defer s2.Close()
	if !tableExists(t, s2, "fleet_watches") {
		t.Fatal("fleet_watches missing after reopen")
	}
}

func TestFleetMigration_ExistingRowsKeepEmptyOrigin(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ws, err := s.CreateWorkspace(Workspace{Path: "/repo", Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.CreateTask(Task{WorkspaceID: ws.ID, Title: "do the thing", Status: "created"})
	if err != nil {
		t.Fatal(err)
	}

	var originHost, originTrace string
	if err := s.db.QueryRow(`SELECT origin_host, origin_trace FROM tasks WHERE id = ?`, task.ID).Scan(&originHost, &originTrace); err != nil {
		t.Fatal(err)
	}
	if originHost != "" || originTrace != "" {
		t.Fatalf("origin = %q/%q, want empty: a local task has no fleet origin", originHost, originTrace)
	}
}
