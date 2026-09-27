package mcpservers

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return New(s)
}

func TestRegistry_CreateAcceptsValidStdio(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)

	// The ADR's Playwright worked example.
	created, err := r.Create(store.McpServer{
		Name: "playwright", Transport: "stdio", Command: "npx",
		Args: `["-y","@playwright/mcp@latest","--headless"]`, Env: `{}`,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !created.Enabled {
		t.Fatalf("Create() Enabled = false, want enabled by default")
	}
	if created.Args != `["-y","@playwright/mcp@latest","--headless"]` || created.Env != `{}` {
		t.Fatalf("Create() = %+v, want args/env carried through verbatim", created)
	}
}

func TestRegistry_CreateAcceptsValidHTTP(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)

	created, err := r.Create(store.McpServer{
		Name: "pplx", Transport: "http", URL: "https://mcp.pplx.ai/mcp", Headers: `{"Authorization":"Bearer x"}`,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.URL != "https://mcp.pplx.ai/mcp" {
		t.Fatalf("Create() URL = %q, want carried through", created.URL)
	}
}

func TestRegistry_CreateAcceptsSSE(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	if _, err := r.Create(store.McpServer{Name: "sse-server", Transport: "sse", URL: "https://s.example/mcp"}); err != nil {
		t.Fatalf("Create() sse error = %v", err)
	}
}

func TestRegistry_CreateRejectsInvalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		server store.McpServer
	}{
		{"empty name", store.McpServer{Name: "  ", Transport: "stdio", Command: "npx"}},
		{"unknown transport", store.McpServer{Name: "x", Transport: "websocket", URL: "https://x"}},
		{"stdio without command", store.McpServer{Name: "x", Transport: "stdio", URL: "https://ignored"}},
		{"http without url", store.McpServer{Name: "x", Transport: "http"}},
		{"sse without url", store.McpServer{Name: "x", Transport: "sse"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newTestRegistry(t)
			if _, err := r.Create(tt.server); err == nil {
				t.Fatalf("Create() error = nil, want error")
			}
		})
	}
}

func TestRegistry_UpdateAppliesSameValidation(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	created, err := r.Create(store.McpServer{Name: "x", Transport: "http", URL: "https://x.example/mcp"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if _, err := r.Update(store.McpServer{ID: created.ID, Name: "x", Transport: "stdio"}); err == nil {
		t.Fatalf("Update() stdio without command error = nil, want error")
	}
	if _, err := r.Update(store.McpServer{ID: created.ID, Name: "x", Transport: "bogus", URL: "https://x"}); err == nil {
		t.Fatalf("Update() unknown transport error = nil, want error")
	}
}

func TestRegistry_CreateDuplicateNameIsConflict(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	if _, err := r.Create(store.McpServer{Name: "playwright", Transport: "stdio", Command: "npx"}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, err := r.Create(store.McpServer{Name: "playwright", Transport: "http", URL: "https://x.example/mcp"})
	if !errors.Is(err, store.ErrMcpServerNameConflict) {
		t.Fatalf("Create() duplicate name error = %v, want store.ErrMcpServerNameConflict", err)
	}
}

func TestRegistry_SetEnabled(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	created, err := r.Create(store.McpServer{Name: "playwright", Transport: "stdio", Command: "npx"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	disabled, err := r.SetEnabled(created.ID, false)
	if err != nil {
		t.Fatalf("SetEnabled(false) error = %v", err)
	}
	if disabled.Enabled {
		t.Fatalf("SetEnabled(false) = %+v, want disabled", disabled)
	}

	// Disabled servers stay in List (ls/mcp.list) but vanish from the
	// workspace-applicable set (ADR-0018 resolved decision 7).
	list, err := r.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 1 || list[0].Enabled {
		t.Fatalf("List() = %+v, want the disabled row still listed", list)
	}
	ws, err := r.store.CreateWorkspace(store.Workspace{Path: "/repo", Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	applicable, err := r.ListForWorkspace(ws.ID)
	if err != nil {
		t.Fatalf("ListForWorkspace() error = %v", err)
	}
	if len(applicable) != 0 {
		t.Fatalf("ListForWorkspace() = %+v, want disabled server excluded", applicable)
	}
}

// fakeNotifier records every notification it receives, for the tests
// below -- mirrors internal/profiles' own notifier test doubles.
type fakeNotifier struct {
	created []store.McpServer
	updated []store.McpServer
	deleted []int64
}

func (f *fakeNotifier) NotifyMcpServerCreated(m store.McpServer) { f.created = append(f.created, m) }
func (f *fakeNotifier) NotifyMcpServerUpdated(m store.McpServer) { f.updated = append(f.updated, m) }
func (f *fakeNotifier) NotifyMcpServerDeleted(id int64)          { f.deleted = append(f.deleted, id) }

func TestRegistry_NotifierFiresOnMutation(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	n := &fakeNotifier{}
	r.SetNotifier(n)

	created, err := r.Create(store.McpServer{Name: "x", Transport: "stdio", Command: "npx"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if len(n.created) != 1 || n.created[0].ID != created.ID {
		t.Fatalf("NotifyMcpServerCreated calls = %+v, want one call for %+v", n.created, created)
	}

	if _, err := r.SetEnabled(created.ID, false); err != nil {
		t.Fatalf("SetEnabled() error = %v", err)
	}
	if _, err := r.Update(store.McpServer{ID: created.ID, Name: "y", Transport: "http", URL: "https://x.example/mcp"}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	// SetEnabled and Update both surface as NotifyMcpServerUpdated.
	if len(n.updated) != 2 || n.updated[0].ID != created.ID || n.updated[1].ID != created.ID {
		t.Fatalf("NotifyMcpServerUpdated calls = %+v, want two calls (SetEnabled + Update) for id %d", n.updated, created.ID)
	}

	if err := r.Delete(created.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if len(n.deleted) != 1 || n.deleted[0] != created.ID {
		t.Fatalf("NotifyMcpServerDeleted calls = %+v, want one call for id %d", n.deleted, created.ID)
	}
}

func TestRegistry_FailedMutationDoesNotNotify(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	n := &fakeNotifier{}
	r.SetNotifier(n)

	if _, err := r.Create(store.McpServer{Name: "x", Transport: "stdio"}); err == nil {
		t.Fatalf("Create() invalid error = nil, want error")
	}
	if _, err := r.Create(store.McpServer{Name: "x", Transport: "stdio", Command: "npx"}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := r.Create(store.McpServer{Name: "x", Transport: "stdio", Command: "npx"}); !errors.Is(err, store.ErrMcpServerNameConflict) {
		t.Fatalf("Create() duplicate error = %v, want conflict", err)
	}

	if len(n.created) != 1 {
		t.Fatalf("NotifyMcpServerCreated calls = %d, want exactly 1 (successful create only)", len(n.created))
	}
}

func TestRegistry_NilNotifierIsNoop(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	// No SetNotifier call -- Create/SetEnabled/Update/Delete must not panic.
	created, err := r.Create(store.McpServer{Name: "x", Transport: "stdio", Command: "npx"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := r.SetEnabled(created.ID, false); err != nil {
		t.Fatalf("SetEnabled() error = %v", err)
	}
	if _, err := r.Update(store.McpServer{ID: created.ID, Name: "y", Transport: "http", URL: "https://x.example/mcp"}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if err := r.Delete(created.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}

func TestRegistry_NilRegistrySetNotifierIsNoop(t *testing.T) {
	t.Parallel()
	var r *Registry
	r.SetNotifier(&fakeNotifier{}) // must not panic
}
