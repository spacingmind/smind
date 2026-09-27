package taskrunner

import (
	"encoding/json"
	"log"

	"github.com/spacingmind/smind/internal/store"
)

// ChatSessionStore persists SessionHandle values on chats.agent_session,
// keyed by chat id (docs/decisions/0016-multiple-chats-per-task.md) --
// unlike MemorySessionStore, a chat's resumable session survives a daemon
// restart. A migrated or brand-new chat's agent_session starts NULL, so its
// next run finds no handle (Get reports ok=false) and starts a fresh
// session, exactly like MemorySessionStore's zero value would; the run
// after that resumes, once Set has written one.
//
// Every read/write failure here (a malformed stored value, a store error)
// degrades to "no handle" or is logged and dropped rather than returned as
// an error: RunPrompt's resume call sites (newOrResumeACPSession,
// newClaudeClientWithResume, newOrResumeCodexThread) already treat "no
// handle" as "start a fresh session", which is exactly the right fallback
// for a session store problem too -- a caller ready to handle a SessionStore
// error doesn't exist, and there is no correct value to invent instead.
type ChatSessionStore struct {
	st *store.Store
}

// NewChatSessionStore returns a ChatSessionStore backed by st.
func NewChatSessionStore(st *store.Store) *ChatSessionStore {
	return &ChatSessionStore{st: st}
}

// Get implements SessionStore. key is a chat id. A handle whose stored
// Provider doesn't match the chat's own bound provider column is treated
// as absent -- the same rule SessionHandle.Provider's doc comment already
// states, enforced again here (in addition to every RunPrompt call site's
// own handle.Provider-vs-this-turn's-provider check) as defense in depth
// against chats.agent_session and chats.provider ever drifting apart.
func (s *ChatSessionStore) Get(key int64) (SessionHandle, bool) {
	chat, err := s.st.GetChat(key)
	if err != nil {
		return SessionHandle{}, false
	}
	if chat.AgentSession == nil || chat.Provider == nil {
		return SessionHandle{}, false
	}

	var handle SessionHandle
	if err := json.Unmarshal([]byte(*chat.AgentSession), &handle); err != nil {
		log.Printf("taskrunner: chat %d: stored agent_session is not valid JSON, ignoring: %v", key, err)
		return SessionHandle{}, false
	}
	if string(handle.Provider) != *chat.Provider {
		return SessionHandle{}, false
	}
	return handle, true
}

// Set implements SessionStore. key is a chat id.
func (s *ChatSessionStore) Set(key int64, handle SessionHandle) {
	data, err := json.Marshal(handle)
	if err != nil {
		log.Printf("taskrunner: chat %d: marshal session handle: %v", key, err)
		return
	}
	if _, err := s.st.SetChatAgentSession(key, string(data)); err != nil {
		log.Printf("taskrunner: chat %d: persist session handle: %v", key, err)
	}
}
