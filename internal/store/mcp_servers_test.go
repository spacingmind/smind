package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// schemaWithoutMcpServers reproduces schema.sql as it was before the
// mcp_servers tables were added: the embedded schema with the two new
// CREATE TABLE blocks and the join table's index stripped. This is what a
// real pre-change smind.db had on disk, derived from the live schema so
// this test can't drift from it.
func schemaWithoutMcpServers() string {
	stripped := schema
	for _, block := range []struct{ start, end string }{
		{"CREATE TABLE IF NOT EXISTS mcp_servers (", ");"},
		{"CREATE TABLE IF NOT EXISTS workspace_mcp_servers (", ");"},
	} {
		i := strings.Index(stripped, block.start)
		if i < 0 {
			panic("schema.sql no longer contains " + block.start)
		}
		j := strings.Index(stripped[i:], block.end)
		if j < 0 {
			panic("schema.sql block " + block.start + " has no terminator")
		}
		stripped = stripped[:i] + strings.TrimRight(stripped[i+j+len(block.end):], "\n")
	}
	return strings.Replace(stripped,
		"CREATE INDEX IF NOT EXISTS idx_workspace_mcp_servers_mcp_server_id ON workspace_mcp_servers(mcp_server_id);\n", "", 1)
}

func newTestWorkspace(t *testing.T, s *Store, path string) Workspace {
	t.Helper()
	ws, err := s.CreateWorkspace(Workspace{Path: path, Title: path, RoutingPolicy: "hard"})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	return ws
}

func newTestMcpServer(t *testing.T, s *Store, m McpServer) McpServer {
	t.Helper()
	created, err := s.CreateMcpServer(m)
	if err != nil {
		t.Fatalf("CreateMcpServer() error = %v", err)
	}
	return created
}

func assertMcpServersEqual(t *testing.T, got, want McpServer) {
	t.Helper()
	if got.ID != want.ID || got.Name != want.Name || got.Transport != want.Transport ||
		got.Command != want.Command || got.Args != want.Args || got.Env != want.Env ||
		got.URL != want.URL || got.Headers != want.Headers || got.Enabled != want.Enabled {
		t.Errorf("mcp server fields = %+v, want %+v", got, want)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Errorf("mcp server timestamps = %+v, want %+v", got, want)
	}
}

func TestStore_McpServers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		server McpServer
	}{
		{
			name: "stdio server",
			server: McpServer{
				Name: "playwright", Transport: "stdio", Command: "npx",
				Args: `["-y","@playwright/mcp@latest","--headless"]`,
				Env:  `{"PLAYWRIGHT_TOKEN":"secret"}`,
			},
		},
		{
			name: "http server",
			server: McpServer{
				Name: "pplx", Transport: "http", URL: "https://mcp.pplx.ai/mcp",
				Headers: `{"Authorization":"Bearer secret"}`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestStore(t)

			created, err := s.CreateMcpServer(tt.server)
			if err != nil {
				t.Fatalf("CreateMcpServer() error = %v", err)
			}
			if created.ID == 0 {
				t.Fatalf("CreateMcpServer() ID = 0, want nonzero")
			}
			if !created.Enabled {
				t.Fatalf("CreateMcpServer() Enabled = false, want the column's default true")
			}

			got, err := s.GetMcpServer(created.ID)
			if err != nil {
				t.Fatalf("GetMcpServer() error = %v", err)
			}
			assertMcpServersEqual(t, got, created)

			list, err := s.ListMcpServers()
			if err != nil {
				t.Fatalf("ListMcpServers() error = %v", err)
			}
			if len(list) != 1 {
				t.Fatalf("ListMcpServers() = %+v, want 1 server", list)
			}
			assertMcpServersEqual(t, list[0], created)
		})
	}
}

func TestStore_ListMcpServersEmpty(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	list, err := s.ListMcpServers()
	if err != nil {
		t.Fatalf("ListMcpServers() error = %v", err)
	}
	if list == nil || len(list) != 0 {
		t.Fatalf("ListMcpServers() = %+v, want empty non-nil slice", list)
	}
}

func TestStore_CreateMcpServerDuplicateName(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	created := newTestMcpServer(t, s, McpServer{Name: "playwright", Transport: "stdio", Command: "npx"})

	_, err := s.CreateMcpServer(McpServer{Name: "playwright", Transport: "http", URL: "https://x.example/mcp"})
	if !errors.Is(err, ErrMcpServerNameConflict) {
		t.Fatalf("CreateMcpServer() duplicate name error = %v, want ErrMcpServerNameConflict", err)
	}
	if got := err.Error(); !strings.Contains(got, "playwright") {
		t.Fatalf("CreateMcpServer() duplicate name error = %q, want it to name the conflicting server", got)
	}

	// The conflict must not have overwritten or disturbed the original row.
	// And the driver's raw error text must not leak: the wrapped error is
	// the domain error, not "UNIQUE constraint failed".
	got, err := s.GetMcpServer(created.ID)
	if err != nil {
		t.Fatalf("GetMcpServer() after conflict error = %v", err)
	}
	if got.Transport != "stdio" || got.Command != "npx" {
		t.Fatalf("original row after conflict = %+v, want untouched stdio/npx", got)
	}
}

func TestStore_UpdateMcpServer(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	created := newTestMcpServer(t, s, McpServer{
		Name: "playwright", Transport: "stdio", Command: "npx",
		Args: `["--headless"]`, Env: `{"A":"1"}`,
	})

	updated, err := s.UpdateMcpServer(McpServer{
		ID: created.ID, Name: "playwright", Transport: "http", URL: "https://mcp.pplx.ai/mcp",
		Headers: `{"Authorization":"Bearer x"}`, Enabled: true,
	})
	if err != nil {
		t.Fatalf("UpdateMcpServer() error = %v", err)
	}
	if !updated.CreatedAt.Equal(created.CreatedAt) {
		t.Errorf("UpdateMcpServer() CreatedAt = %v, want unchanged %v", updated.CreatedAt, created.CreatedAt)
	}
	if updated.UpdatedAt.Equal(created.UpdatedAt) {
		t.Errorf("UpdateMcpServer() UpdatedAt unchanged, want a newer timestamp")
	}

	got, err := s.GetMcpServer(created.ID)
	if err != nil {
		t.Fatalf("GetMcpServer() error = %v", err)
	}
	if got.Transport != "http" || got.URL != "https://mcp.pplx.ai/mcp" || got.Headers != `{"Authorization":"Bearer x"}` {
		t.Fatalf("GetMcpServer() after update = %+v, want fields replaced", got)
	}
	if got.Command != "" || got.Args != "" || got.Env != "" {
		t.Fatalf("GetMcpServer() after update = %+v, want stdio fields cleared by the full-record replace", got)
	}
}

func TestStore_UpdateMcpServerMissing(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	if _, err := s.UpdateMcpServer(McpServer{ID: 999, Name: "x", Transport: "http", URL: "https://x"}); err == nil {
		t.Fatalf("UpdateMcpServer(missing) error = nil, want error")
	}
}

func TestStore_UpdateMcpServerDuplicateName(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	first := newTestMcpServer(t, s, McpServer{Name: "a", Transport: "stdio", Command: "npx"})
	second := newTestMcpServer(t, s, McpServer{Name: "b", Transport: "stdio", Command: "npx"})

	_, err := s.UpdateMcpServer(McpServer{ID: second.ID, Name: "a", Transport: "stdio", Command: "npx"})
	if !errors.Is(err, ErrMcpServerNameConflict) {
		t.Fatalf("UpdateMcpServer() duplicate name error = %v, want ErrMcpServerNameConflict", err)
	}
	if _, err := s.GetMcpServer(first.ID); err != nil {
		t.Fatalf("GetMcpServer(first) after conflict error = %v", err)
	}
}

func TestStore_SetMcpServerEnabled(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	created := newTestMcpServer(t, s, McpServer{Name: "playwright", Transport: "stdio", Command: "npx"})

	disabled, err := s.SetMcpServerEnabled(created.ID, false)
	if err != nil {
		t.Fatalf("SetMcpServerEnabled(false) error = %v", err)
	}
	if disabled.Enabled {
		t.Fatalf("SetMcpServerEnabled(false) Enabled = true, want false")
	}
	if !disabled.UpdatedAt.After(created.UpdatedAt) {
		t.Errorf("SetMcpServerEnabled() UpdatedAt not advanced, want a newer timestamp")
	}

	// The toggle must touch only enabled/updated_at -- the command field
	// set at create is still there.
	if disabled.Command != "npx" {
		t.Fatalf("SetMcpServerEnabled() Command = %q, want %q untouched", disabled.Command, "npx")
	}

	enabled, err := s.SetMcpServerEnabled(created.ID, true)
	if err != nil {
		t.Fatalf("SetMcpServerEnabled(true) error = %v", err)
	}
	if !enabled.Enabled {
		t.Fatalf("SetMcpServerEnabled(true) Enabled = false, want true")
	}
}

func TestStore_SetMcpServerEnabledMissing(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	if _, err := s.SetMcpServerEnabled(999, false); err == nil {
		t.Fatalf("SetMcpServerEnabled(missing) error = nil, want error")
	}
}

func TestStore_DeleteMcpServer(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ws := newTestWorkspace(t, s, "/repo")
	created := newTestMcpServer(t, s, McpServer{Name: "playwright", Transport: "stdio", Command: "npx"})
	if err := s.AddWorkspaceMcpServer(ws.ID, created.ID); err != nil {
		t.Fatalf("AddWorkspaceMcpServer() error = %v", err)
	}

	if err := s.DeleteMcpServer(created.ID); err != nil {
		t.Fatalf("DeleteMcpServer() error = %v", err)
	}
	if _, err := s.GetMcpServer(created.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetMcpServer() after delete error = %v, want sql.ErrNoRows", err)
	}

	// The workspace restriction row must be gone with it (cascade), so a
	// re-created same-named server starts unrestricted.
	recreated := newTestMcpServer(t, s, McpServer{Name: "playwright", Transport: "stdio", Command: "npx"})
	got, err := s.ListMcpServersForWorkspace(ws.ID)
	if err != nil {
		t.Fatalf("ListMcpServersForWorkspace() after recreate error = %v", err)
	}
	if len(got) != 1 || got[0].ID != recreated.ID || !got[0].Enabled {
		t.Fatalf("ListMcpServersForWorkspace() after recreate = %+v, want only the recreated enabled row", got)
	}
}

func TestStore_DeleteMcpServerMissing(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	if err := s.DeleteMcpServer(999); err == nil {
		t.Fatalf("DeleteMcpServer(missing) error = nil, want error")
	}
}

func TestStore_RejectsMissingWorkspaceMcpServerReferences(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ws := newTestWorkspace(t, s, "/repo")
	server := newTestMcpServer(t, s, McpServer{Name: "playwright", Transport: "stdio", Command: "npx"})

	if err := s.AddWorkspaceMcpServer(999, server.ID); err == nil {
		t.Fatal("AddWorkspaceMcpServer() with missing workspace error = nil, want foreign key error")
	}
	if err := s.AddWorkspaceMcpServer(ws.ID, 999); err == nil {
		t.Fatal("AddWorkspaceMcpServer() with missing server error = nil, want foreign key error")
	}
}

// TestStore_McpServersForWorkspace is the plan's Workspace-restriction test
// scenario, at the store layer: a server with a workspace_mcp_servers
// restriction row is included only for that workspace's runs, dropped for
// every other workspace; a server with no restriction row is included for
// every workspace; disabled servers are never returned.
func TestStore_McpServersForWorkspace(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	wsA := newTestWorkspace(t, s, "/repo-a")
	wsB := newTestWorkspace(t, s, "/repo-b")

	global := newTestMcpServer(t, s, McpServer{Name: "global", Transport: "http", URL: "https://g.example/mcp"})
	onlyA := newTestMcpServer(t, s, McpServer{Name: "only-a", Transport: "stdio", Command: "npx"})
	disabled := newTestMcpServer(t, s, McpServer{Name: "disabled", Transport: "http", URL: "https://d.example/mcp"})
	if _, err := s.SetMcpServerEnabled(disabled.ID, false); err != nil {
		t.Fatalf("SetMcpServerEnabled() error = %v", err)
	}
	if err := s.AddWorkspaceMcpServer(wsA.ID, onlyA.ID); err != nil {
		t.Fatalf("AddWorkspaceMcpServer() error = %v", err)
	}

	assertIDs := func(label string, got []McpServer, wantIDs ...int64) {
		t.Helper()
		if len(got) != len(wantIDs) {
			t.Fatalf("%s = %+v, want %d rows", label, got, len(wantIDs))
		}
		for i, id := range wantIDs {
			if got[i].ID != id {
				t.Fatalf("%s = %+v, want id %d at position %d", label, got, id, i)
			}
		}
	}

	// Workspace A: the global server plus its own restricted one; the
	// disabled global server is invisible.
	assertIDs("ListMcpServersForWorkspace(wsA)", mustListFor(t, s, wsA.ID), global.ID, onlyA.ID)
	// Workspace B: only the global server -- onlyA is restricted elsewhere,
	// disabled is dropped.
	assertIDs("ListMcpServersForWorkspace(wsB)", mustListFor(t, s, wsB.ID), global.ID)

	// Disabling the global server empties every workspace's result.
	if _, err := s.SetMcpServerEnabled(global.ID, false); err != nil {
		t.Fatalf("SetMcpServerEnabled(global) error = %v", err)
	}
	assertIDs("ListMcpServersForWorkspace(wsA) after disable", mustListFor(t, s, wsA.ID), onlyA.ID)
	assertIDs("ListMcpServersForWorkspace(wsB) after disable", mustListFor(t, s, wsB.ID))

	// Un-restricting onlyA makes it global again.
	if err := s.RemoveWorkspaceMcpServer(wsA.ID, onlyA.ID); err != nil {
		t.Fatalf("RemoveWorkspaceMcpServer() error = %v", err)
	}
	assertIDs("ListMcpServersForWorkspace(wsB) after un-restrict", mustListFor(t, s, wsB.ID), onlyA.ID)
}

func mustListFor(t *testing.T, s *Store, workspaceID int64) []McpServer {
	t.Helper()
	got, err := s.ListMcpServersForWorkspace(workspaceID)
	if err != nil {
		t.Fatalf("ListMcpServersForWorkspace(%d) error = %v", workspaceID, err)
	}
	return got
}

// TestOpen_McpServersTableOnPreExistingDatabase proves a database created
// before the mcp_servers change gets both new tables from schema.sql's
// CREATE TABLE IF NOT EXISTS on its next Open -- the ADR-0014-derived
// precedent (new tables need no migrate.go entry, only new columns do).
// The seeded database here is a pre-change store.Open output: every table
// from before, none of the new ones.
func TestOpen_McpServersTableOnPreExistingDatabase(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "pre-mcp.db")
	seedPreMcpServersDB(t, path)

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() on pre-mcp-servers database error = %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	for _, table := range []string{"mcp_servers", "workspace_mcp_servers"} {
		var name string
		if err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name); err != nil {
			t.Fatalf("table %s not found after Open on pre-existing database: %v", table, err)
		}
	}

	// And the pre-existing row the seed wrote is still readable.
	got, err := s.GetAccount(1)
	if err != nil {
		t.Fatalf("GetAccount() after migration error = %v", err)
	}
	if got.Label != "pre-existing" {
		t.Fatalf("GetAccount() Label = %q, want %q preserved", got.Label, "pre-existing")
	}

	// A server inserted post-Open reads back normally, exercising the new
	// table on a database that didn't have it at create time.
	created, err := s.CreateMcpServer(McpServer{Name: "playwright", Transport: "stdio", Command: "npx"})
	if err != nil {
		t.Fatalf("CreateMcpServer() error = %v", err)
	}
	if _, err := s.GetMcpServer(created.ID); err != nil {
		t.Fatalf("GetMcpServer() error = %v", err)
	}
}

// seedPreMcpServersDB creates a database shaped like smind's just before
// the mcp_servers tables existed: an old schema.sql (everything except
// mcp_servers/workspace_mcp_servers) plus one account row to prove Open
// preserves pre-existing data while adding the new tables.
func seedPreMcpServersDB(t *testing.T, path string) {
	t.Helper()

	old := schemaWithoutMcpServers()
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatalf("open raw db error = %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(old); err != nil {
		t.Fatalf("apply pre-mcp schema error = %v", err)
	}
	now := time.Now().UTC()
	if _, err := db.Exec(
		`INSERT INTO accounts (id, provider, label, credential_type, credential_data, created_at, updated_at)
		 VALUES (1, 'anthropic', 'pre-existing', 'oauth', 'x', ?, ?)`, now, now,
	); err != nil {
		t.Fatalf("seed account error = %v", err)
	}
}
