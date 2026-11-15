package browser

import (
	"context"
	"errors"
	"fmt"
)

// Helpers shared by the read primitives (snapshot, screenshot, wait). They are
// session internals: the broker never sees a CDP session id.

// tabFor returns the ready tab with the given target id, or the first ready
// tab when id is empty. It never creates a tab.
func (s *Session) tabFor(id string) (*tabState, error) {
	if id == "" {
		t, ok := s.firstTab()
		if !ok {
			return nil, errors.New("browser: no page is open: use browser_open first")
		}
		return t, nil
	}
	t, ok := s.tabByID(id)
	if !ok {
		return nil, fmt.Errorf("browser: no such tab %q", id)
	}
	return t, nil
}

// tabCall sends a CDP method on the tab's own session.
func (s *Session) tabCall(ctx context.Context, t *tabState, method string, params, out any) error {
	return s.cl.Call(ctx, t.sessionID, method, params, out)
}

// setRefs records the refs of the latest snapshot of a tab (replacing the
// previous ones: a ref is valid only for the snapshot that produced it).
func (s *Session) setRefs(tabID string, refs map[string]int64) {
	s.mu.Lock()
	s.snapRefs[tabID] = refs
	s.mu.Unlock()
}

// refsFor returns a copy of a tab's current refs.
func (s *Session) refsFor(tabID string) map[string]int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]int64, len(s.snapRefs[tabID]))
	for k, v := range s.snapRefs[tabID] {
		out[k] = v
	}
	return out
}
