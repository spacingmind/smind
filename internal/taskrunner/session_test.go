package taskrunner

import "testing"

// TestMemorySessionStore proves the default SessionStore's basic
// get/set/replace contract.
func TestMemorySessionStore(t *testing.T) {
	t.Parallel()
	s := NewMemorySessionStore()

	if _, ok := s.Get(1); ok {
		t.Fatal("Get() on empty store ok = true, want false")
	}

	s.Set(1, SessionHandle{Provider: ProviderGLM, SessionID: "a"})
	got, ok := s.Get(1)
	if !ok || got.SessionID != "a" {
		t.Fatalf("Get(1) = %+v, %v, want SessionID %q, true", got, ok, "a")
	}

	s.Set(1, SessionHandle{Provider: ProviderGLM, SessionID: "b"})
	got, ok = s.Get(1)
	if !ok || got.SessionID != "b" {
		t.Fatalf("Get(1) after replace = %+v, %v, want SessionID %q, true", got, ok, "b")
	}

	if _, ok := s.Get(2); ok {
		t.Fatal("Get(2) ok = true, want false (different key)")
	}
}
