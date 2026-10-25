package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// errOwnedProfile is what the stored-row writers return for an id in the
// owned range: those profiles are not rows, so there is nothing to write
// (defence in depth; the registries refuse first, with the plugin named).
var errOwnedProfile = errors.New("a plugin-owned profile is read-only and not stored")

// OwnedIDBase is the first id of the plugin-owned range (ADR-0042). Rows a
// plugin contributes (profiles, MCP servers) are not stored: they are derived
// from the plugin's manifest while it is enabled, and each gets a stable
// synthetic id at or above this base, so every id-keyed path (task.send
// --profile, seat admission, failover, run provenance, mcp.get) resolves it
// like any row. Autoincrement ids never reach the base, and JSON numbers stay
// exact in JavaScript (the range tops out below 2^53).
const OwnedIDBase int64 = 1 << 40

// OwnedIDSpan is the size of the owned range.
const OwnedIDSpan int64 = 1 << 40

// IsOwnedID reports whether id is in the plugin-owned range.
func IsOwnedID(id int64) bool { return id >= OwnedIDBase }

// Overlay supplies the rows plugins contribute. Its answers follow the plugin
// state at call time, so an owned row exists exactly while its plugin is
// enabled and the global switch is on, and it can never be written to the
// user's tables.
type Overlay interface {
	OwnedProfile(id int64) (AgentProfile, bool)
	OwnedProfiles() []AgentProfile
	// OwnedMcp lists owned MCP servers as a read path shows them: names are
	// "<plugin>:<name>" and env/headers still hold ${setting.KEY} placeholders.
	OwnedMcp() []McpServer
	// OwnedMcpForRun lists them as a run receives them: wire-safe names and
	// settings substituted into env and headers. Only injection may call it.
	OwnedMcpForRun() []McpServer
}

// SetOverlay installs the plugin overlay (nil removes it). Called at wiring
// time, before the store serves requests.
func (s *Store) SetOverlay(o Overlay) {
	s.overlayMu.Lock()
	s.overlay = o
	s.overlayMu.Unlock()
}

// Overlay returns the installed overlay, or nil.
func (s *Store) Overlay() Overlay {
	s.overlayMu.RLock()
	defer s.overlayMu.RUnlock()
	return s.overlay
}

// getOwnedProfile resolves an id in the owned range through the overlay; an
// unknown one (plugin disabled or removed) is the usual not-found error.
func (s *Store) getOwnedProfile(id int64) (AgentProfile, error) {
	if o := s.Overlay(); o != nil {
		if p, ok := o.OwnedProfile(id); ok {
			return p, nil
		}
	}
	return AgentProfile{}, fmt.Errorf("get agent profile %d: %w", id, sql.ErrNoRows)
}

// OwnedRowError reports a write to a row a plugin owns. Owned rows are
// read-only for every caller, human or agent: change the plugin, or disable
// it. wsapi maps it to the typed read_only_owned error.
type OwnedRowError struct {
	Plugin string
	Kind   string // "mcp server" | "profile"
	Name   string
}

func (e *OwnedRowError) Error() string {
	return fmt.Sprintf("%s %q is contributed by plugin %q and is read-only: edit the plugin, or disable it", e.Kind, e.Name, e.Plugin)
}
