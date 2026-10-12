package quota

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/store"
)

// fakeClock is the one clock a test runs on.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Now().UTC().Truncate(time.Millisecond)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

// Paths of the fake provider server.
const (
	pathAnthropic = "/anthropic"
	pathOpenAI    = "/openai"
	pathToken     = "/token"
)

// usageServer is an httptest server standing in for the provider usage
// endpoints and the token endpoint. It counts every request.
type usageServer struct {
	srv *httptest.Server

	mu      sync.Mutex
	perPath map[string]int
	perTok  map[string]int
	total   int
	cur     int
	max     int
	handler map[string]http.HandlerFunc
}

func newUsageServer(t *testing.T) *usageServer {
	t.Helper()
	u := &usageServer{perPath: map[string]int{}, perTok: map[string]int{}, handler: map[string]http.HandlerFunc{}}
	u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		u.perPath[r.URL.Path]++
		u.total++
		u.perTok[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]++
		u.cur++
		if u.cur > u.max {
			u.max = u.cur
		}
		h := u.handler[r.URL.Path]
		u.mu.Unlock()
		defer func() {
			u.mu.Lock()
			u.cur--
			u.mu.Unlock()
		}()
		if h == nil {
			h = respondUsageOK
		}
		h(w, r)
	}))
	t.Cleanup(u.srv.Close)
	return u
}

func respondUsageOK(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte(fetcherUsageBody))
}

// respondStatus answers with a fixed status and optional headers.
func respondStatus(status int, hdr ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		for i := 0; i+1 < len(hdr); i += 2 {
			w.Header().Set(hdr[i], hdr[i+1])
		}
		w.WriteHeader(status)
	}
}

// Set installs the handler for a path ("" for the usage path of anthropic).
func (u *usageServer) Set(path string, h http.HandlerFunc) {
	u.mu.Lock()
	u.handler[path] = h
	u.mu.Unlock()
}

func (u *usageServer) Calls() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.total
}

func (u *usageServer) PathCalls(path string) int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.perPath[path]
}

// TokenCalls counts requests that carried the given bearer token.
func (u *usageServer) TokenCalls(token string) int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.perTok[token]
}

func (u *usageServer) MaxConcurrent() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.max
}

// fakeReportFetcher is a ReportFetcher whose behaviour a test scripts.
type fakeReportFetcher struct {
	calls atomic.Int32
	fn    func(ctx context.Context, a accounts.Account) (Report, error)
}

func (f *fakeReportFetcher) Fetch(ctx context.Context, a accounts.Account) (Report, error) {
	f.calls.Add(1)
	if f.fn != nil {
		return f.fn(ctx, a)
	}
	return availableTestReport(), nil
}

func (f *fakeReportFetcher) Calls() int { return int(f.calls.Load()) }

func availableTestReport() Report {
	used := 10.0
	return Report{Status: StatusAvailable, PlanLabel: "pro", Windows: []Window{newWindow("five_hour", "5-hour", "5h", true, &used, nil)}}
}

// env is a store, registry and Poller on a fake clock. restart() rebuilds
// everything over the same database file, like a daemon restart.
type env struct {
	t    testing.TB
	path string
	clk  *fakeClock
	mk   func(reg *accounts.Registry, clk *fakeClock) ReportFetcher
	opts []Option

	st  *store.Store
	reg *accounts.Registry
	p   *Poller
}

func newEnv(t testing.TB, mk func(reg *accounts.Registry, clk *fakeClock) ReportFetcher, opts ...Option) *env {
	t.Helper()
	e := &env{t: t, path: filepath.Join(t.TempDir(), "smind.db"), clk: newFakeClock(), mk: mk}
	e.opts = append([]Option{WithClock(e.clk.Now), WithJitter(func() float64 { return 0 })}, opts...)
	e.open()
	t.Cleanup(func() {
		e.p.Close()
		e.st.Close()
	})
	return e
}

func (e *env) open() {
	e.t.Helper()
	st, err := store.Open(e.path)
	if err != nil {
		e.t.Fatalf("store.Open() error = %v", err)
	}
	e.st = st
	e.reg = accounts.New(st)
	e.p = New(st, e.reg, e.mk(e.reg, e.clk), e.opts...)
}

// restart closes the poller and the store and reopens both at the same path.
func (e *env) restart() {
	e.t.Helper()
	e.p.Close()
	if err := e.st.Close(); err != nil {
		e.t.Fatalf("store.Close() error = %v", err)
	}
	e.open()
}

// addOAuth creates an anthropic OAuth account whose token is valid for years
// (so the fetcher never tries to refresh it).
func (e *env) addOAuth(token string) int64 {
	e.t.Helper()
	return e.addOAuthExpiring("anthropic", token, time.Now().Add(100000*time.Hour))
}

func (e *env) addOAuthExpiring(provider, token string, expires time.Time) int64 {
	e.t.Helper()
	a, err := e.reg.AddOAuth(provider, "acct", accounts.OAuthCredential{
		AccessToken: token, RefreshToken: "refresh-" + token, ExpiresAt: expires,
	})
	if err != nil {
		e.t.Fatalf("AddOAuth() error = %v", err)
	}
	return a.ID
}

func (e *env) state(id int64) store.QuotaPollState {
	e.t.Helper()
	st, _, err := e.st.GetQuotaPollState(id)
	if err != nil {
		e.t.Fatalf("GetQuotaPollState() error = %v", err)
	}
	return st
}

func (e *env) touch(id int64) Result   { return e.p.Touch(context.Background(), id) }
func (e *env) refresh(id int64) Result { return e.p.Refresh(context.Background(), id) }
