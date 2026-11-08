package taskrunner

import (
	"encoding/json"
	"regexp"
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

	// UsageTotals is the cumulative usage the claude-native CLI reported at
	// the end of this session's last turn (nil for other providers, for a
	// session that has not finished a turn, and for sessions stored before
	// it existed). A resumed claude session carries its running totals into
	// the next result, so a turn's own usage is that result minus these
	// (claudeTurnUsage).
	UsageTotals *ClaudeUsageTotals `json:"usageTotals,omitempty"`

	// Fork, when set, marks a claude-native handle that is a pending fork
	// (ADR-0057): SessionID is the PARENT's session, and the chat's first turn
	// launches with --resume SessionID --fork-session --resume-session-at
	// Fork.EntryUUID. The marker is cleared as soon as the CLI reports the
	// forked session's own id, so a later turn resumes that session and never
	// forks again.
	Fork *ForkAt `json:"fork,omitempty"`
}

// ForkAt is where a pending fork cuts its parent's session.
type ForkAt struct {
	// EntryUUID is the transcript entry the fork resumes at (the last assistant
	// record of the anchor turn).
	EntryUUID string `json:"entryUuid"`
}

var forkEntryPattern = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z-]{7,63}$`)

// ValidForkEntry reports whether s has the shape of a transcript entry uuid. The
// value goes into one equals-joined CLI argument, so this is defence in depth
// against an edited transcript, not an injection fix.
func ValidForkEntry(s string) bool { return forkEntryPattern.MatchString(s) }

// TotalsLogger is implemented by a SessionStore that keeps the history of a
// chat's claude usage totals (ChatSessionStore does), so a fork can start its
// first turn's usage from the totals at its anchor.
type TotalsLogger interface {
	LogClaudeTotals(chatID int64, totals *ClaudeUsageTotals)
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
