package store

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func browserSettingsWorkspace(t *testing.T, s *Store) int64 {
	t.Helper()
	w, err := s.CreateWorkspace(Workspace{Path: t.TempDir(), Title: "W"})
	if err != nil {
		t.Fatal(err)
	}
	return w.ID
}

func TestBrowserSettings_MissingRowIsOff(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws := browserSettingsWorkspace(t, s)
	got, err := s.GetBrowserSettings(ws)
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkspaceID != ws || got.Enabled || got.ProfileMode != "ephemeral" || got.UpdatedBy != "" {
		t.Fatalf("missing row = %+v, want the OFF zero value", got)
	}
	if got.AllowHosts == nil || len(got.AllowHosts) != 0 {
		t.Fatalf("missing row AllowHosts = %#v, want non-nil and empty", got.AllowHosts)
	}
}

func TestBrowserSettings_RoundTrip(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws := browserSettingsWorkspace(t, s)
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	if err := s.SaveBrowserSettings(BrowserSettings{WorkspaceID: ws, Enabled: true, AllowHosts: []string{"docs.example.com", "api.example.com"}, UpdatedAt: now, UpdatedBy: "local"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetBrowserSettings(ws)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.ProfileMode != "ephemeral" || got.UpdatedBy != "local" || !got.UpdatedAt.Equal(now) {
		t.Fatalf("read back = %+v", got)
	}
	if len(got.AllowHosts) != 2 || got.AllowHosts[0] != "docs.example.com" || got.AllowHosts[1] != "api.example.com" {
		t.Fatalf("read back AllowHosts = %#v", got.AllowHosts)
	}
	// A second save replaces the hosts (and stamps updated_at itself when
	// the caller leaves it zero).
	if err := s.SaveBrowserSettings(BrowserSettings{WorkspaceID: ws, Enabled: true, AllowHosts: []string{"only.example.com"}, UpdatedBy: "device:abc"}); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetBrowserSettings(ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.AllowHosts) != 1 || got.AllowHosts[0] != "only.example.com" {
		t.Fatalf("after a re-save AllowHosts = %#v, want the new hosts", got.AllowHosts)
	}
	if got.UpdatedBy != "device:abc" || got.UpdatedAt.IsZero() {
		t.Fatalf("after a re-save = %+v, want the new actor and a stamped updated_at", got)
	}
}

func TestBrowserSettings_NilHostsStoredAsEmptyArray(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws := browserSettingsWorkspace(t, s)
	if err := s.SaveBrowserSettings(BrowserSettings{WorkspaceID: ws, Enabled: false, AllowHosts: nil, ProfileMode: ""}); err != nil {
		t.Fatal(err)
	}
	var stored, mode string
	if err := s.db.QueryRow(`SELECT allow_hosts, profile_mode FROM browser_settings WHERE workspace_id = ?`, ws).Scan(&stored, &mode); err != nil {
		t.Fatal(err)
	}
	if stored != "[]" {
		t.Fatalf("allow_hosts = %q, want \"[]\" (never \"null\")", stored)
	}
	if mode != "ephemeral" {
		t.Fatalf("profile_mode = %q, want the ephemeral default", mode)
	}
	got, err := s.GetBrowserSettings(ws)
	if err != nil {
		t.Fatal(err)
	}
	if got.AllowHosts == nil || len(got.AllowHosts) != 0 || got.ProfileMode != "ephemeral" {
		t.Fatalf("read back = %+v, want an empty non-nil allowlist and ephemeral", got)
	}
}

func TestBrowserSettings_UnknownWorkspaceIsAnError(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	err := s.SaveBrowserSettings(BrowserSettings{WorkspaceID: 999, Enabled: true, AllowHosts: []string{"a.example.com"}})
	if err == nil {
		t.Fatal("save for a workspace that does not exist = nil, want the foreign key error")
	}
	if n := countRows(t, s, `SELECT COUNT(*) FROM browser_settings`); n != 0 {
		t.Fatalf("rows written for an unknown workspace = %d", n)
	}
}

func TestBrowserSettings_CascadesWithTheWorkspace(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws := browserSettingsWorkspace(t, s)
	if err := s.SaveBrowserSettings(BrowserSettings{WorkspaceID: ws, Enabled: true, AllowHosts: []string{"a.example.com"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteWorkspace(ws); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetBrowserSettings(ws)
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled || got.AllowHosts == nil || len(got.AllowHosts) != 0 || got.ProfileMode != "ephemeral" {
		t.Fatalf("after the workspace is gone = %+v, want the zero value", got)
	}
	if n := countRows(t, s, `SELECT COUNT(*) FROM browser_settings`); n != 0 {
		t.Fatalf("orphaned rows = %d, want 0", n)
	}
}

func TestBrowserSettings_DeleteIsIdempotent(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws := browserSettingsWorkspace(t, s)
	if err := s.SaveBrowserSettings(BrowserSettings{WorkspaceID: ws, Enabled: true, AllowHosts: []string{"a.example.com"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteBrowserSettings(ws); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteBrowserSettings(ws); err != nil {
		t.Fatalf("second DeleteBrowserSettings = %v, want a quiet no-op", err)
	}
	if n := countRows(t, s, `SELECT COUNT(*) FROM browser_settings`); n != 0 {
		t.Fatalf("rows after delete = %d, want 0", n)
	}
}

func TestBrowserSettings_CorruptAllowHostsNamesTheWorkspace(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws := browserSettingsWorkspace(t, s)
	if err := s.SaveBrowserSettings(BrowserSettings{WorkspaceID: ws, Enabled: true, AllowHosts: []string{"a.example.com"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE browser_settings SET allow_hosts = 'not json' WHERE workspace_id = ?`, ws); err != nil {
		t.Fatal(err)
	}
	_, err := s.GetBrowserSettings(ws)
	if err == nil {
		t.Fatal("a row with corrupt allow_hosts read back with no error")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("workspace %d", ws)) {
		t.Fatalf("error %q does not name the workspace id", err)
	}
}

// TestSchemaHasBrowserSettings: a fresh store has the table, and applying
// the schema again (a second Open) is a safe no-op -- the way
// migrate_test.go proves migration idempotence.
func TestSchemaHasBrowserSettings(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "browser.db")
	for i := 0; i < 3; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("Open #%d error = %v", i, err)
		}
		var name string
		if err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'browser_settings'`).Scan(&name); err != nil {
			t.Fatalf("Open #%d: browser_settings missing: %v", i, err)
		}
		s.Close()
	}
}

// A database written before browser_settings existed (the table is absent, the
// rest is the previous schema) opens with the new code: the table appears and
// the old rows are untouched.
func TestSchemaOlderDatabaseGainsBrowserSettings(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "older.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ws := browserSettingsWorkspace(t, s)
	if _, err := s.db.Exec(`DROP TABLE browser_settings`); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'browser_settings'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("the older database must not have the table: %d %v", n, err)
	}
	s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatalf("opening the older database: %v", err)
	}
	defer s.Close()
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'browser_settings'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("browser_settings must exist after Open: %d %v", n, err)
	}
	if w, err := s.GetWorkspace(ws); err != nil || w.Title != "W" {
		t.Fatalf("old data intact: %+v %v", w, err)
	}
	if got, err := s.GetBrowserSettings(ws); err != nil || got.Enabled {
		t.Fatalf("a workspace of the older database is off: %+v %v", got, err)
	}
	if err := s.SaveBrowserSettings(BrowserSettings{WorkspaceID: ws, Enabled: true, AllowHosts: []string{"a.test"}, ProfileMode: "ephemeral", UpdatedBy: "local", UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("the new table is usable: %v", err)
	}
}
