package pairing

import (
	"sync"
	"time"
)

const (
	// DefaultWindow is the pairing window duration used when Open is called
	// with a non-positive duration.
	DefaultWindow = 10 * time.Minute
	// DefaultSlots is the slot count used when Open is called with a
	// non-positive slot count: one invitee per window.
	DefaultSlots = 1
)

// Window is the human-in-the-loop pairing window: the short period during
// which an invitee may pair. A window has a fixed number of slots (one by
// default), and a candidate whose link was first seen before the window
// opened cannot consume a slot — an invite leaked before the human pressed
// "pair" must not work. The zero Window is closed. The clock is injectable
// for tests.
type Window struct {
	mu        sync.Mutex
	now       func() time.Time
	openedAt  time.Time
	expires   time.Time
	remaining int
}

// NewWindow returns a Window reading time from now, or the real clock when
// now is nil.
func NewWindow(now func() time.Time) *Window {
	if now == nil {
		now = time.Now
	}
	return &Window{now: now}
}

// Open opens (or reopens) a pairing window, replacing any open one. A
// non-positive duration or slot count falls back to DefaultWindow and
// DefaultSlots. It returns when the window opened and when it expires.
func (w *Window) Open(d time.Duration, slots int) (openedAt, expires time.Time) {
	if d <= 0 {
		d = DefaultWindow
	}
	if slots <= 0 {
		slots = DefaultSlots
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.openedAt = w.now()
	w.expires = w.openedAt.Add(d)
	w.remaining = slots
	return w.openedAt, w.expires
}

// Status reports whether the window is open, when it expires and how many
// slots remain. An expired or exhausted window reports open=false.
func (w *Window) Status() (open bool, expires time.Time, remaining int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	open = !w.openedAt.IsZero() && w.remaining > 0 && w.now().Before(w.expires)
	return open, w.expires, w.remaining
}

// Take consumes one pairing slot for a candidate link first seen at
// firstSeen. It returns true only when the window is open, unexpired, has
// slots left and the link was first seen at or after the window opened.
func (w *Window) Take(firstSeen time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.openedAt.IsZero() || w.remaining <= 0 || !w.now().Before(w.expires) {
		return false
	}
	if firstSeen.Before(w.openedAt) {
		return false
	}
	w.remaining--
	return true
}

// Close closes the window, releasing nothing else: reopening starts fresh.
func (w *Window) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.openedAt = time.Time{}
	w.expires = time.Time{}
	w.remaining = 0
}
