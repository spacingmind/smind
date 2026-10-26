package host

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/plugins/host/hosttest"
)

func TestMain(m *testing.M) {
	if hosttest.Main() {
		return
	}
	os.Exit(m.Run())
}

// fastPolicy is DefaultPolicy with timings short enough for tests.
func fastPolicy() Policy {
	p := DefaultPolicy()
	p.StartTimeout = 5 * time.Second
	p.RequestTimeout = 1500 * time.Millisecond
	p.StopGrace = 400 * time.Millisecond
	p.BackoffBase, p.BackoffCap = 10*time.Millisecond, 40*time.Millisecond
	return p
}

// recorder is a Registry that remembers notifications and refuses requests.
type recorder struct {
	mu    sync.Mutex
	notes []note
	reqs  []string
}

type note struct {
	plugin, method string
	params         json.RawMessage
}

func (r *recorder) Request(_ context.Context, _, method string, _ json.RawMessage) (any, *RPCError) {
	r.mu.Lock()
	r.reqs = append(r.reqs, method)
	r.mu.Unlock()
	return DenyAll.Request(context.Background(), "", method, nil)
}

func (r *recorder) Notify(plugin, method string, params json.RawMessage) {
	r.mu.Lock()
	r.notes = append(r.notes, note{plugin, method, params})
	r.mu.Unlock()
}

func (r *recorder) wait(t *testing.T, method string) json.RawMessage {
	t.Helper()
	var got json.RawMessage
	waitFor(t, 10*time.Second, method+" notification", func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		for _, n := range r.notes {
			if n.method == method {
				got = n.params
				return true
			}
		}
		return false
	})
	return got
}

func waitFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func waitState(t *testing.T, h *Host, id string, want State) Status {
	t.Helper()
	var st Status
	waitFor(t, 10*time.Second, "state "+string(want), func() bool {
		var ok bool
		st, ok = h.Status(id)
		return ok && st.State == want
	})
	return st
}

// spec builds a Spec running the helper in mode inside fresh directories.
func spec(t *testing.T, id, mode string, args ...string) Spec {
	t.Helper()
	dir := t.TempDir()
	data := t.TempDir()
	return Spec{ID: id, Version: "1.0.0", Dir: dir, DataDir: data, DaemonVersion: "9.9.9", Command: hosttest.Command(mode, args...)}
}

// fakeClock is a Clock the test advances by hand.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []fakeWaiter
}

type fakeWaiter struct {
	at time.Time
	ch chan time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.waiters = append(c.waiters, fakeWaiter{at: c.now.Add(d), ch: ch})
	return ch
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	keep := c.waiters[:0]
	for _, w := range c.waiters {
		if !w.at.After(c.now) {
			w.ch <- c.now
		} else {
			keep = append(keep, w)
		}
	}
	c.waiters = keep
	c.mu.Unlock()
}

func (c *fakeClock) pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.waiters)
}

// safeWriter serializes writes into a buffer shared with the test goroutine.
type safeWriter struct {
	mu sync.Mutex
	w  *bytes.Buffer
}

func (s *safeWriter) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.String()
}

func (s *safeWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// sameDir compares two directories after resolving symlinks (macOS /var).
func sameDir(t *testing.T, a, b string) bool {
	t.Helper()
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

// waitRestarts waits for the plugin to be backing off after its n-th crash.
func waitRestarts(t *testing.T, h *Host, id string, n int) Status {
	t.Helper()
	var st Status
	waitFor(t, 10*time.Second, "crash "+itoa(n), func() bool {
		var ok bool
		st, ok = h.Status(id)
		return ok && st.State == StateRestarting && st.Restarts == n
	})
	return st
}
