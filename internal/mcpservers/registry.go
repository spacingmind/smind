// Package mcpservers provides McpServer CRUD on top of internal/store,
// validating each write against ADR-0018's transport rules (docs/decisions/
// 0018-agent-mcp-servers.md). It exists as its own package, mirroring
// internal/profiles, so transport-shape validation lives outside
// internal/store (which must not grow provider-protocol knowledge) and the
// wsapi handlers and any future non-wsapi caller share one validation path.
package mcpservers

import (
	"database/sql"
	"encoding/json"
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
	if err := normalizeValidate(&m); err != nil {
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

// ownedMcp returns the owned MCP server with the given id, if its plugin is
// active.
func (r *Registry) ownedMcp(id int64) (store.McpServer, bool) {
	if !store.IsOwnedID(id) {
		return store.McpServer{}, false
	}
	if o := r.store.Overlay(); o != nil {
		for _, m := range o.OwnedMcp() {
			if m.ID == id {
				return m, true
			}
		}
	}
	return store.McpServer{}, false
}

// refuseOwned is the guard of every write by id: a server a plugin owns is
// read-only for every caller, whether it is a person or an agent.
func (r *Registry) refuseOwned(id int64) error {
	if m, ok := r.ownedMcp(id); ok {
		return &store.OwnedRowError{Plugin: m.OwnerPlugin, Kind: "mcp server", Name: m.Name}
	}
	return nil
}

// Get returns the MCP server with the given id.
func (r *Registry) Get(id int64) (store.McpServer, error) {
	if store.IsOwnedID(id) {
		if m, ok := r.ownedMcp(id); ok {
			return m, nil
		}
		return store.McpServer{}, fmt.Errorf("get mcp server %d: %w", id, sql.ErrNoRows)
	}
	return r.store.GetMcpServer(id)
}

// List returns every MCP server, enabled and disabled alike, ordered by id,
// then the servers enabled plugins contribute (owned, read-only; their env and
// headers still hold ${setting.KEY} placeholders, never a secret).
func (r *Registry) List() ([]store.McpServer, error) {
	rows, err := r.store.ListMcpServers()
	if err != nil {
		return nil, err
	}
	if o := r.store.Overlay(); o != nil {
		rows = append(rows, o.OwnedMcp()...)
	}
	return rows, nil
}

// ListForWorkspace returns the enabled MCP servers applicable to the given
// workspace (see store.ListMcpServersForWorkspace), followed by the servers of
// enabled plugins: exactly what a new run is injected with. An owned server
// whose run name equals a user server's is left out (the user's wins). A run
// keeps what it was started with; the next run sees the current plugin state.
func (r *Registry) ListForWorkspace(workspaceID int64) ([]store.McpServer, error) {
	rows, err := r.store.ListMcpServersForWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	if o := r.store.Overlay(); o != nil {
		taken := map[string]bool{}
		for _, m := range rows {
			taken[m.Name] = true
		}
		for _, m := range o.OwnedMcpForRun() {
			if !taken[m.Name] {
				rows = append(rows, m)
			}
		}
	}
	return rows, nil
}

// SetEnabled toggles the enabled flag of the MCP server with the given id,
// firing NotifyMcpServerUpdated on success. A dedicated toggle rather than
// a generic update field, matching ADR-0018's wsapi surface.
func (r *Registry) SetEnabled(id int64, enabled bool) (store.McpServer, error) {
	if err := r.refuseOwned(id); err != nil {
		return store.McpServer{}, err
	}
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
	if err := r.refuseOwned(m.ID); err != nil {
		return store.McpServer{}, err
	}
	if err := normalizeValidate(&m); err != nil {
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
	if err := r.refuseOwned(id); err != nil {
		return err
	}
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

// validMcpServerName is the charset a server name may draw from. Name is
// the wire identity every downstream protocol keys a server by (Claude's
// mcpServers map key, Codex's [mcp_servers.<name>] TOML table key), so it
// must survive those embeddings unchanged: no whitespace, dots (a TOML
// table-name separator), or other punctuation with structural meaning.
const validMcpServerName = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-"

// normalize and validate m in place. Validation is exactly ADR-0018's
// wsapi list -- a non-empty charset-restricted name (uniqueness is the
// store's UNIQUE constraint, surfaced as store.ErrMcpServerNameConflict),
// a known transport, a command for stdio, a url for http/sse -- plus two
// normalizations the ADR's column shapes imply: empty args/env/headers
// become their canonical "[]"/"{}" (the SQL DEFAULT never applies, since
// inserts pass every column explicitly), and each JSON blob must decode
// as its declared shape (a []string for args, string->string objects for
// env/headers) so a malformed blob can't reach a run's session setup.
func normalizeValidate(m *store.McpServer) error {
	// Trim first, then hold the trimmed name to the charset: surrounding
	// whitespace is a copy/paste margin worth tolerating, but any interior
	// character outside the set survives the trim and rejects -- a name is
	// the wire key downstream protocols embed verbatim (Claude's
	// mcpServers key, Codex's [mcp_servers.<name>] TOML table key).
	m.Name = strings.TrimSpace(m.Name)
	if m.Name == "" {
		return fmt.Errorf("name is required")
	}
	if strings.Contains(m.Name, ":") {
		return fmt.Errorf("name %q: names with ':' are reserved for servers contributed by plugins (<plugin>:<name>)", m.Name)
	}
	if strings.Trim(m.Name, validMcpServerName) != "" {
		return fmt.Errorf("name %q may only contain letters, digits, '_' and '-'", m.Name)
	}

	var err error
	switch m.Transport {
	case TransportStdio:
		if strings.TrimSpace(m.Command) == "" {
			return fmt.Errorf("stdio transport requires a command")
		}
		if m.Args, err = normalizeJSONShape(m.Args, "args", &[]string{}); err != nil {
			return err
		}
		if m.Env, err = normalizeJSONShape(m.Env, "env", &map[string]string{}); err != nil {
			return err
		}
		// A stdio server has no url/headers; drop any that arrived with a
		// transport-flip so the stored row matches its transport exactly.
		m.URL, m.Headers = "", ""
	case TransportHTTP, TransportSSE:
		if strings.TrimSpace(m.URL) == "" {
			return fmt.Errorf("%s transport requires a url", m.Transport)
		}
		if m.Headers, err = normalizeJSONShape(m.Headers, "headers", &map[string]string{}); err != nil {
			return err
		}
		m.Command, m.Args, m.Env = "", "", ""
	default:
		return fmt.Errorf("unknown transport %q", m.Transport)
	}
	return nil
}

// normalizeJSONShape canonicalizes an optional JSON blob: an empty/blank
// string becomes example's zero-value encoding ("[]" or "{}"), and a
// non-empty string must unmarshal into example's shape, returning it
// re-encoded so insignificant formatting differences don't multiply row
// representations of the same value.
func normalizeJSONShape(raw, field string, example any) (string, error) {
	if strings.TrimSpace(raw) == "" {
		canon, err := json.Marshal(example)
		if err != nil {
			return "", fmt.Errorf("%s: %w", field, err)
		}
		return string(canon), nil
	}
	if err := json.Unmarshal([]byte(raw), example); err != nil {
		return "", fmt.Errorf("%s: %w", field, err)
	}
	canon, err := json.Marshal(example)
	if err != nil {
		return "", fmt.Errorf("%s: %w", field, err)
	}
	return string(canon), nil
}
