package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"sync"
	"time"
)

// SessionTTL is how long a login stays valid. It bounds the damage of a
// leaked session token; a restart of the daemon invalidates every session
// (they live in memory only).
const SessionTTL = 24 * time.Hour

// maxSessions bounds memory: an unauthenticated caller cannot mint sessions
// (Issue runs only after a correct password), but a script holding the
// password could.
const maxSessions = 1024

// SessionStore holds the expiring session tokens issued by POST /api/login.
// Tokens are 256-bit random values; only their SHA-256 is kept, so a memory
// dump of the daemon does not hand out live tokens, and a lookup is a map hit
// on the hash (no timing signal about the token itself).
type SessionStore struct {
	mu       sync.Mutex
	now      func() time.Time
	ttl      time.Duration
	sessions map[[32]byte]time.Time
}

// NewSessionStore returns a store. now == nil means time.Now.
func NewSessionStore(ttl time.Duration, now func() time.Time) *SessionStore {
	if now == nil {
		now = time.Now
	}
	return &SessionStore{now: now, ttl: ttl, sessions: map[[32]byte]time.Time{}}
}

// Issue mints a session token and its expiry.
func (s *SessionStore) Issue() (token string, expires time.Time) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic("auth: no randomness: " + err.Error()) // not recoverable; never hand out a weak token
	}
	token = base64.RawURLEncoding.EncodeToString(raw[:])
	now := s.now()
	expires = now.Add(s.ttl)

	s.mu.Lock()
	defer s.mu.Unlock()
	for h, exp := range s.sessions {
		if !now.Before(exp) {
			delete(s.sessions, h)
		}
	}
	if len(s.sessions) >= maxSessions { // evict the soonest to expire
		var oldest [32]byte
		var oldestExp time.Time
		first := true
		for h, exp := range s.sessions {
			if first || exp.Before(oldestExp) {
				oldest, oldestExp, first = h, exp, false
			}
		}
		delete(s.sessions, oldest)
	}
	s.sessions[sha256.Sum256([]byte(token))] = expires
	return token, expires
}

// Valid reports whether token is a live session.
func (s *SessionStore) Valid(token string) bool {
	if token == "" || len(token) > 128 {
		return false
	}
	h := sha256.Sum256([]byte(token))
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.sessions[h]
	if !ok {
		return false
	}
	if !s.now().Before(exp) {
		delete(s.sessions, h)
		return false
	}
	return true
}

// RevokeAll drops every session.
func (s *SessionStore) RevokeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.sessions)
}

// Len is the number of live-or-unpurged sessions (tests).
func (s *SessionStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}
