package taskrunner

import (
	"encoding/json"
	"sync"
)

// SessionHandle is a resumable agent session: returned by a runner after a
// prompt turn, and read back before the next one so the same provider
// subprocess/conversation can be resumed instead of starting fresh. Modeled
// on Paseo's AgentPersistenceHandle
// (refs/paseo/packages/protocol/src/agent-types.ts:168-174).
type SessionHandle struct {
	// Provider is the provider this handle was created for. A stored handle
	// whose Provider doesn't match the run's own provider is never used to
	// resume -- see ADR-0016 section 3 (provider binds once, per chat).
	Provider Provider

	// SessionID is the provider-native session/thread id: the ACP
	// sessionId, the claude-native CLI's --resume id, or the Codex thread
	// id.
	SessionID string

	// NativeHandle is an additional provider-specific handle distinct from
	// SessionID, when a provider needs one (mirroring
	// AgentPersistenceHandle.nativeHandle). Unused by any of the three
	// runners today; reserved for a future provider that needs it.
	NativeHandle string

	// Metadata is an opaque, provider-defined payload carried alongside the
	// handle. Unused by any of the three runners today; reserved the same
	// way NativeHandle is.
	Metadata json.RawMessage
}

// SessionStore persists one SessionHandle per key across runs, so a runner
// can resume where the previous run on that key left off. Keyed by task ID
// today -- the same key config_options.go's acpSessions map already uses
// for a task's live ACP session bookkeeping. Once chats land (ADR-0016 P1),
// the key becomes chat ID and a store backed by chats.agent_session
// replaces MemorySessionStore.
type SessionStore interface {
	// Get returns the handle stored for key, if any.
	Get(key int64) (SessionHandle, bool)

	// Set stores handle for key, replacing any previous handle for that
	// key.
	Set(key int64, handle SessionHandle)
}

// MemorySessionStore is an in-memory SessionStore, safe for concurrent use.
// It is Runner's default store (see New), and is lost on daemon restart --
// a caller needing session continuity across restarts must supply its own
// SessionStore via WithSessionStore.
type MemorySessionStore struct {
	mu sync.Mutex
	m  map[int64]SessionHandle
}

// NewMemorySessionStore returns an empty MemorySessionStore.
func NewMemorySessionStore() *MemorySessionStore {
	return &MemorySessionStore{m: make(map[int64]SessionHandle)}
}

// Get implements SessionStore.
func (s *MemorySessionStore) Get(key int64) (SessionHandle, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.m[key]
	return h, ok
}

// Set implements SessionStore.
func (s *MemorySessionStore) Set(key int64, handle SessionHandle) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = handle
}
