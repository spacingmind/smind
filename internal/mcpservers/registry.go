// Package mcpservers provides McpServer CRUD on top of internal/store,
// validating each write against ADR-0018's transport rules (docs/decisions/
// 0018-agent-mcp-servers.md). It exists as its own package, mirroring
// internal/profiles, so transport-shape validation lives outside
// internal/store (which must not grow provider-protocol knowledge) and the
// wsapi handlers and any future non-wsapi caller share one validation path.
package mcpservers

import (
	"fmt"
	"strings"
	"sync"

	"github.com/spacingmind/smind/internal/store"
)

// Registry provides validated McpServer CRUD backed by a store.Store.
type Registry struct {
	store *store.Store

	// notifier, if set via SetNotifier, receives mcp-server lifecycle
	// notifications (create/update/delete) so the wsapi server can push
	// them as ADR-0009-shaped subscription events. Mirrors
	// internal/profiles.Registry's Notifier/SetNotifier pattern.
	notifierMu sync.Mutex
	notifier   Notifier
}

// Notifier receives mcp-server lifecycle notifications. Every method is
// called synchronously from the Registry method that made the change,
// after the underlying store write committed; implementations
// (internal/wsapi's bus adapter) must not block, and must redact
// env/headers before putting a server on any wire (ADR-0018 "Secrets").
type Notifier interface {
	NotifyMcpServerCreated(m store.McpServer)
	NotifyMcpServerUpdated(m store.McpServer)
	NotifyMcpServerDeleted(id int64)
}

// New returns a Registry backed by s.
func New(s *store.Store) *Registry {
	return &Registry{store: s}
}

// SetNotifier registers n. Nil-safe: notifications with no notifier
// registered are no-ops, and a nil *Registry accepts the call rather than
// panicking -- matches internal/profiles.Registry.SetNotifier's shape.
func (r *Registry) SetNotifier(n Notifier) {
	if r == nil {
		return
	}
	r.notifierMu.Lock()
	r.notifier = n
	r.notifierMu.Unlock()
}

func (r *Registry) getNotifier() Notifier {
	if r == nil {
		return nil
	}
	r.notifierMu.Lock()
	n := r.notifier
	r.notifierMu.Unlock()
	return n
}

// Create validates m and inserts it, firing NotifyMcpServerCreated on
// success.
func (r *Registry) Create(m store.McpServer) (store.McpServer, error) {
	if err := validate(m); err != nil {
		return store.McpServer{}, err
	}
	created, err := r.store.CreateMcpServer(m)
	if err != nil {
		return store.McpServer{}, err
	}
	if n := r.getNotifier(); n != nil {
		n.NotifyMcpServerCreated(created)
	}
	return created, nil
}

// Get returns the MCP server with the given id.
func (r *Registry) Get(id int64) (store.McpServer, error) {
	return r.store.GetMcpServer(id)
}

// List returns every MCP server, enabled and disabled alike, ordered by id.
func (r *Registry) List() ([]store.McpServer, error) {
	return r.store.ListMcpServers()
}

// ListForWorkspace returns the enabled MCP servers applicable to the given
// workspace (see store.ListMcpServersForWorkspace).
func (r *Registry) ListForWorkspace(workspaceID int64) ([]store.McpServer, error) {
	return r.store.ListMcpServersForWorkspace(workspaceID)
}

// SetEnabled toggles the enabled flag of the MCP server with the given id,
// firing NotifyMcpServerUpdated on success. A dedicated toggle rather than
// a generic update field, matching ADR-0018's wsapi surface.
func (r *Registry) SetEnabled(id int64, enabled bool) (store.McpServer, error) {
	updated, err := r.store.SetMcpServerEnabled(id, enabled)
	if err != nil {
		return store.McpServer{}, err
	}
	if n := r.getNotifier(); n != nil {
		n.NotifyMcpServerUpdated(updated)
	}
	return updated, nil
}

// Update validates m and replaces the stored server with id m.ID (a
// full-record replace -- see store.UpdateMcpServer), firing
// NotifyMcpServerUpdated on success.
func (r *Registry) Update(m store.McpServer) (store.McpServer, error) {
	if err := validate(m); err != nil {
		return store.McpServer{}, err
	}
	updated, err := r.store.UpdateMcpServer(m)
	if err != nil {
		return store.McpServer{}, err
	}
	if n := r.getNotifier(); n != nil {
		n.NotifyMcpServerUpdated(updated)
	}
	return updated, nil
}

// Delete removes the MCP server with the given id, firing
// NotifyMcpServerDeleted on success.
func (r *Registry) Delete(id int64) error {
	if err := r.store.DeleteMcpServer(id); err != nil {
		return err
	}
	if n := r.getNotifier(); n != nil {
		n.NotifyMcpServerDeleted(id)
	}
	return nil
}

// Transports accepted on the wire, per ADR-0018.
const (
	TransportStdio = "stdio"
	TransportHTTP  = "http"
	TransportSSE   = "sse"
)

// validate rejects an MCP server this package's callers should never be
// able to store, exactly ADR-0018's wsapi validation list: a non-empty
// name (uniqueness is the store's UNIQUE constraint, surfaced as
// store.ErrMcpServerNameConflict), a known transport, a command for stdio,
// and a url for http/sse.
func validate(m store.McpServer) error {
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("name is required")
	}
	switch m.Transport {
	case TransportStdio:
		if strings.TrimSpace(m.Command) == "" {
			return fmt.Errorf("stdio transport requires a command")
		}
	case TransportHTTP, TransportSSE:
		if strings.TrimSpace(m.URL) == "" {
			return fmt.Errorf("%s transport requires a url", m.Transport)
		}
	default:
		return fmt.Errorf("unknown transport %q", m.Transport)
	}
	return nil
}
