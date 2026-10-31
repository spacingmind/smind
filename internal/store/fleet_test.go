package store

import (
	"bytes"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
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

func fleetPair(hostID, alias string) (FleetHost, []FleetLink) {
	h := FleetHost{HostID: hostID, Alias: alias, Pub: []byte{1, 2, 3, 4}, Label: "laptop", State: "paired"}
	links := []FleetLink{
		{HostID: hostID, Direction: FleetOutbound, Enabled: true, GrantJSON: `{"tasks":["read"]}`, TransportJSON: `{"addr":"host:743"}`},
		{HostID: hostID, Direction: FleetInbound, Enabled: false, GrantJSON: "", TransportJSON: ""},
	}
	return h, links
}

func TestFleetPair_CreateGetListDelete(t *testing.T) {
	t.Parallel()

	s := openFleetStore(t)
	h, links := fleetPair("h1", "alice")
	createdAt := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	h.CreatedAt = createdAt
	if err := s.CreateFleetPair(h, links); err != nil {
		t.Fatalf("CreateFleetPair() error = %v", err)
	}
	if err := s.CreateFleetPair(FleetHost{HostID: "h2", Alias: "bob", Pub: []byte{9}, State: "paired"}, nil); err != nil {
		t.Fatalf("CreateFleetPair() error = %v", err)
	}

	got, err := s.GetFleetHost("h1")
	if err != nil {
		t.Fatal(err)
	}
	if got.HostID != "h1" || got.Alias != "alice" || got.Label != "laptop" || got.State != "paired" {
		t.Fatalf("host = %+v", got)
	}
	if !bytes.Equal(got.Pub, h.Pub) {
		t.Fatalf("pub = %v, want %v", got.Pub, h.Pub)
	}
	if !got.CreatedAt.Equal(createdAt) {
		t.Fatalf("CreatedAt = %v, want %v", got.CreatedAt, createdAt)
	}
	if _, err := s.GetFleetHostByAlias("alice"); err != nil {
		t.Fatalf("GetFleetHostByAlias() error = %v", err)
	}

	// A zero CreatedAt is stamped now, not left zero.
	stamped, err := s.GetFleetHost("h2")
	if err != nil {
		t.Fatal(err)
	}
	if stamped.CreatedAt.IsZero() || stamped.CreatedAt.After(time.Now().UTC().Add(time.Minute)) {
		t.Fatalf("CreatedAt = %v, want ~now", stamped.CreatedAt)
	}

	hosts, err := s.ListFleetHosts()
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 2 || hosts[0].Alias != "alice" || hosts[1].Alias != "bob" {
		t.Fatalf("hosts = %+v, want alice then bob", hosts)
	}
	if n, err := s.CountFleetHosts(); err != nil || n != 2 {
		t.Fatalf("CountFleetHosts() = %d, %v; want 2", n, err)
	}

	linkRows, err := s.ListFleetLinks("h1")
	if err != nil {
		t.Fatal(err)
	}
	if len(linkRows) != 2 || linkRows[0].Direction != FleetInbound || linkRows[1].Direction != FleetOutbound {
		t.Fatalf("links = %+v, want inbound then outbound", linkRows)
	}
	out, err := s.GetFleetLink("h1", FleetOutbound)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Enabled || out.GrantJSON != `{"tasks":["read"]}` || out.TransportJSON != `{"addr":"host:743"}` {
		t.Fatalf("outbound link = %+v", out)
	}
	if out.UpdatedAt.IsZero() {
		t.Fatal("link UpdatedAt zero, want stamped")
	}

	if err := s.DeleteFleetHost("h1"); err != nil {
		t.Fatalf("DeleteFleetHost() error = %v", err)
	}
	if _, err := s.GetFleetHost("h1"); !errors.Is(err, ErrFleetNotFound) {
		t.Fatalf("GetFleetHost() after delete = %v, want ErrFleetNotFound", err)
	}
	if _, err := s.GetFleetLink("h1", FleetOutbound); !errors.Is(err, ErrFleetNotFound) {
		t.Fatalf("GetFleetLink() after delete = %v, want ErrFleetNotFound", err)
	}
	if rows, err := s.ListFleetLinks("h1"); err != nil || len(rows) != 0 {
		t.Fatalf("ListFleetLinks() after delete = %d rows, %v; want 0", len(rows), err)
	}
	if err := s.DeleteFleetHost("h1"); !errors.Is(err, ErrFleetNotFound) {
		t.Fatalf("DeleteFleetHost() twice = %v, want ErrFleetNotFound", err)
	}
}

func TestFleetPair_AliasAndHostConflicts(t *testing.T) {
	t.Parallel()

	s := openFleetStore(t)
	h1, links := fleetPair("h1", "alice")
	if err := s.CreateFleetPair(h1, links); err != nil {
		t.Fatalf("CreateFleetPair() error = %v", err)
	}

	// Same alias, new host: rejected, and the new host left no rows behind.
	h2, links2 := fleetPair("h2", "alice")
	if err := s.CreateFleetPair(h2, links2); !errors.Is(err, ErrFleetAliasTaken) {
		t.Fatalf("CreateFleetPair() same alias = %v, want ErrFleetAliasTaken", err)
	}
	if _, err := s.GetFleetHost("h2"); !errors.Is(err, ErrFleetNotFound) {
		t.Fatalf("host h2 = %v, want not found", err)
	}
	if rows, err := s.ListFleetLinks("h2"); err != nil || len(rows) != 0 {
		t.Fatalf("h2 links = %d rows, %v; want 0", len(rows), err)
	}

	// Same host id, new alias: rejected the same way.
	h3, _ := fleetPair("h1", "carol")
	if err := s.CreateFleetPair(h3, nil); !errors.Is(err, ErrFleetHostExists) {
		t.Fatalf("CreateFleetPair() same host = %v, want ErrFleetHostExists", err)
	}
	if n, err := s.CountFleetHosts(); err != nil || n != 1 {
		t.Fatalf("CountFleetHosts() = %d, %v; want 1 (only h1)", n, err)
	}
}

func TestFleetPair_RollsBackOnBadLink(t *testing.T) {
	t.Parallel()

	s := openFleetStore(t)
	h, _ := fleetPair("h1", "alice")
	links := []FleetLink{{HostID: "h1", Direction: "sideways", Enabled: true}}
	if err := s.CreateFleetPair(h, links); err == nil {
		t.Fatal("CreateFleetPair() with a bad direction succeeded, want error")
	}
	if _, err := s.GetFleetHost("h1"); !errors.Is(err, ErrFleetNotFound) {
		t.Fatalf("host h1 = %v, want not found: the failed pair left nothing", err)
	}
}

func TestFleetPair_RollsBackOnMidInsertFailure(t *testing.T) {
	t.Parallel()

	s := openFleetStore(t)
	h, _ := fleetPair("h1", "alice")
	// The second link trips PRIMARY KEY(host_id, direction) only after the
	// host row and the first link were inserted inside the transaction, so
	// what is under test is the rollback itself, not a pre-check.
	links := []FleetLink{
		{HostID: "h1", Direction: FleetOutbound, Enabled: true},
		{HostID: "h1", Direction: FleetOutbound, Enabled: false},
	}
	if err := s.CreateFleetPair(h, links); err == nil {
		t.Fatal("CreateFleetPair() with a duplicated direction succeeded, want error")
	}
	if _, err := s.GetFleetHost("h1"); !errors.Is(err, ErrFleetNotFound) {
		t.Fatalf("host h1 = %v, want not found", err)
	}
	if rows, err := s.ListFleetLinks("h1"); err != nil || len(rows) != 0 {
		t.Fatalf("links = %d rows, %v; want 0", len(rows), err)
	}
	if n, err := s.CountFleetHosts(); err != nil || n != 0 {
		t.Fatalf("CountFleetHosts() = %d, %v; want 0", n, err)
	}
}

func TestFleetPair_RejectsLinkForOtherHost(t *testing.T) {
	t.Parallel()

	s := openFleetStore(t)
	h, _ := fleetPair("h1", "alice")
	links := []FleetLink{{HostID: "h2", Direction: FleetOutbound, Enabled: true}}
	err := s.CreateFleetPair(h, links)
	if err == nil || !strings.Contains(err.Error(), "link host") {
		t.Fatalf("CreateFleetPair() with a foreign link = %v, want an error naming the link host", err)
	}
	if _, err := s.GetFleetHost("h1"); !errors.Is(err, ErrFleetNotFound) {
		t.Fatalf("host h1 = %v, want not found", err)
	}
	if n, err := s.CountFleetHosts(); err != nil || n != 0 {
		t.Fatalf("CountFleetHosts() = %d, %v; want 0", n, err)
	}
}

func TestFleetLink_PutUpsertAndUnknownHost(t *testing.T) {
	t.Parallel()

	s := openFleetStore(t)
	h, _ := fleetPair("h1", "alice")
	if err := s.CreateFleetPair(h, nil); err != nil {
		t.Fatal(err)
	}

	if err := s.PutFleetLink(FleetLink{HostID: "nobody", Direction: FleetOutbound}); !errors.Is(err, ErrFleetNotFound) {
		t.Fatalf("PutFleetLink() unknown host = %v, want ErrFleetNotFound", err)
	}
	if err := s.PutFleetLink(FleetLink{HostID: "h1", Direction: "sideways"}); err == nil {
		t.Fatal("PutFleetLink() bad direction succeeded, want error")
	}

	if err := s.PutFleetLink(FleetLink{HostID: "h1", Direction: FleetOutbound, Enabled: true, GrantJSON: "g1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutFleetLink(FleetLink{HostID: "h1", Direction: FleetOutbound, Enabled: false, GrantJSON: "g2", TransportJSON: "t2"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetFleetLink("h1", FleetOutbound)
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled || got.GrantJSON != "g2" || got.TransportJSON != "t2" {
		t.Fatalf("link = %+v, want the upserted values", got)
	}
	if rows, err := s.ListFleetLinks("h1"); err != nil || len(rows) != 1 {
		t.Fatalf("links = %d rows, %v; want 1 (upsert, not a second row)", len(rows), err)
	}
	if _, err := s.GetFleetLink("h1", FleetInbound); !errors.Is(err, ErrFleetNotFound) {
		t.Fatalf("GetFleetLink() never put = %v, want ErrFleetNotFound", err)
	}
}

func TestFleetHost_UpdateAliasConflict(t *testing.T) {
	t.Parallel()

	s := openFleetStore(t)
	if err := s.CreateFleetPair(FleetHost{HostID: "h1", Alias: "alice", Pub: []byte{1}, State: "paired"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateFleetPair(FleetHost{HostID: "h2", Alias: "bob", Pub: []byte{2}, State: "paired"}, nil); err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateFleetHost("h1", "bob", "label", "state"); !errors.Is(err, ErrFleetAliasTaken) {
		t.Fatalf("UpdateFleetHost() onto bob's alias = %v, want ErrFleetAliasTaken", err)
	}
	if err := s.UpdateFleetHost("nobody", "carol", "label", "state"); !errors.Is(err, ErrFleetNotFound) {
		t.Fatalf("UpdateFleetHost() unknown host = %v, want ErrFleetNotFound", err)
	}

	if err := s.UpdateFleetHost("h1", "carol", "the label", "available"); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetFleetHostByAlias("carol")
	if err != nil {
		t.Fatal(err)
	}
	if got.HostID != "h1" || got.Label != "the label" || got.State != "available" {
		t.Fatalf("host = %+v, want h1 relabeled", got)
	}
	if _, err := s.GetFleetHostByAlias("alice"); !errors.Is(err, ErrFleetNotFound) {
		t.Fatalf("old alias = %v, want ErrFleetNotFound after rename", err)
	}
	// Keeping one's own alias is fine.
	if err := s.UpdateFleetHost("h1", "carol", "x", "y"); err != nil {
		t.Fatalf("UpdateFleetHost() keeping own alias = %v", err)
	}
}

func TestFleetHost_NoSchemaForMesh(t *testing.T) {
	t.Parallel()

	s := openFleetStore(t)
	rows, err := s.db.Query(`SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var fleet []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(name, "mesh") || strings.Contains(name, "cluster") {
			t.Fatalf("table %q exists; the fleet is host-to-host, not a mesh or cluster", name)
		}
		if strings.HasPrefix(name, "fleet_") {
			fleet = append(fleet, name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(fleet)
	want := append([]string(nil), fleetTables...)
	sort.Strings(want)
	if !slices.Equal(fleet, want) {
		t.Fatalf("fleet tables = %v, want exactly %v", fleet, want)
	}
}
