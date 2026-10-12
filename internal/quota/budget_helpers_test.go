package quota

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
)

// a3aFetcher builds the REAL AccountFetcher pointed at the fake usage server
// (anthropic path only), on the test's fake clock.
func a3aFetcher(srv *usageServer, reg *accounts.Registry, clk *fakeClock) ReportFetcher {
	src := newAnthropicSource(srv.srv.Client())
	src.url = srv.srv.URL + pathAnthropic
	return newAccountFetcher(reg, map[string]providerSpec{
		"anthropic": {source: src, refresher: &fetcherRefresher{}},
	}, clk.Now)
}

// a3aEnv is an env whose Poller drives the real AccountFetcher against a
// counting httptest usage server.
func a3aEnv(t *testing.T, opts ...Option) (*env, *usageServer) {
	t.Helper()
	srv := newUsageServer(t)
	e := newEnv(t, func(reg *accounts.Registry, clk *fakeClock) ReportFetcher {
		return a3aFetcher(srv, reg, clk)
	}, opts...)
	return e, srv
}

// a3aGate is an http handler that parks every request until opened and
// reports how many requests are parked.
type a3aGate struct {
	once    sync.Once
	open    chan struct{}
	entered chan struct{}
}

func newA3aGate(buffer int) *a3aGate {
	return &a3aGate{open: make(chan struct{}), entered: make(chan struct{}, buffer)}
}

func (g *a3aGate) Open() { g.once.Do(func() { close(g.open) }) }

func (g *a3aGate) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		select {
		case g.entered <- struct{}{}:
		default:
		}
		select {
		case <-g.open:
		case <-r.Context().Done():
			return
		}
		respondUsageOK(w, r)
	}
}

// a3aWait receives n signals from ch or fails after 5 s.
func a3aWait(t *testing.T, ch <-chan struct{}, n int, what string) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for i := 0; i < n; i++ {
		select {
		case <-ch:
		case <-timeout:
			t.Fatalf("timed out waiting for %s (%d/%d)", what, i, n)
		}
	}
}

// a3aEventually polls cond until it holds or 5 s pass.
func a3aEventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// a3aWantCalls fails unless the server has seen exactly want requests.
func a3aWantCalls(t *testing.T, srv *usageServer, want int, when string) {
	t.Helper()
	if got := srv.Calls(); got != want {
		t.Fatalf("%s: usage requests = %d, want %d", when, got, want)
	}
}

// a3aStubSource serves one fixed account, so a test can hand the Poller an
// account the store has no row for.
type a3aStubSource struct{ acc accounts.Account }

func (s a3aStubSource) Get(int64) (accounts.Account, error) { return s.acc, nil }
