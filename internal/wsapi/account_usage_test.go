package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/quota"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
)

func TestUsage_AccountJSONKeysAllowlist(t *testing.T) {
	t.Parallel()
	b, err := json.Marshal(AccountUsage{})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	_ = json.Unmarshal(b, &m)
	var got []string
	for k := range m {
		got = append(got, k)
	}
	sort.Strings(got)
	want := []string{"accountId", "credentialType", "displayName", "inFlight", "key", "lastError",
		"nextRefreshAt", "provider", "refreshBlocked", "report", "stale"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}
	// with a cooldown, exactly one more key
	b, _ = json.Marshal(AccountUsage{Cooldown: &usageCooldown{}})
	m = nil
	_ = json.Unmarshal(b, &m)
	if _, ok := m["cooldown"]; !ok || len(m) != len(want)+1 {
		t.Fatalf("cooldown key missing or extra keys: %v", m)
	}
}

func TestUsage_AccountCooldownAndObservedFieldsRoundTrip(t *testing.T) {
	t.Parallel()
	in := AccountUsage{
		AccountID: 1, Key: "anthropic-1",
		Cooldown: &usageCooldown{Until: time.Unix(0, 0).UTC().Format(time.RFC3339), Scope: "model", Model: "opus", Reason: "rate_limited"},
		Report:   &quota.Report{Version: 2, Windows: []quota.Window{{ID: "w", Source: "observed", Status: "limited"}}},
	}
	b, _ := json.Marshal(in)
	var out AccountUsage
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Cooldown == nil || *out.Cooldown != *in.Cooldown {
		t.Fatalf("cooldown = %+v", out.Cooldown)
	}
	w := out.Report.Windows[0]
	if w.Source != "observed" || w.Status != "limited" {
		t.Fatalf("window = %+v", w)
	}
	// omitempty: absent when unset
	b, _ = json.Marshal(quota.Window{ID: "x"})
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if _, ok := m["source"]; ok {
		t.Fatal("source should be omitted")
	}
	if _, ok := m["status"]; ok {
		t.Fatal("status should be omitted")
	}
}

// ---- account.usage RPC (ADR-0028 PR-C) ----
//
// These tests are deliberately serial: some shorten the package var
// usageWaitTimeout, which parallel tests must not observe mid-change.

type usageClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *usageClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *usageClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// usageFetcher is a counting quota.ReportFetcher. fn, when set, decides the
// outcome (and may block); the default answers an available report.
type usageFetcher struct {
	calls atomic.Int32
	fn    func(ctx context.Context, a accounts.Account) (quota.Report, error)
}

func (f *usageFetcher) Fetch(ctx context.Context, a accounts.Account) (quota.Report, error) {
	f.calls.Add(1)
	if f.fn != nil {
		return f.fn(ctx, a)
	}
	used := 25.0
	return quota.Report{Status: quota.StatusAvailable, PlanLabel: "pro",
		Windows: []quota.Window{{ID: "five_hour", Label: "5-hour", UsedPct: &used, Tone: quota.ToneOK}}}, nil
}

func (f *usageFetcher) Calls() int { return int(f.calls.Load()) }

type usageHarness struct {
	t       *testing.T
	api     *API
	srv     *httptest.Server
	reg     *accounts.Registry
	poller  *quota.Poller
	fetcher *usageFetcher
	clk     *usageClock
	ids     []int64
	nextReq int
}

func newUsageHarness(t *testing.T, accountCount int, opts ...quota.Option) *usageHarness {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	reg := accounts.New(db)
	h := &usageHarness{t: t, reg: reg, fetcher: &usageFetcher{}, clk: &usageClock{t: time.Now().UTC().Truncate(time.Second)}}
	for i := 0; i < accountCount; i++ {
		a, err := reg.AddAPIKey("anthropic", fmt.Sprintf("Label %d", i+1), fmt.Sprintf("sk-ant-secret-%d", i+1))
		if err != nil {
			t.Fatalf("AddAPIKey() error = %v", err)
		}
		h.ids = append(h.ids, a.ID)
	}
	opts = append([]quota.Option{quota.WithClock(h.clk.Now), quota.WithJitter(func() float64 { return 0 })}, opts...)
	h.poller = quota.New(db, reg, h.fetcher, opts...)
	api, err := New(workspace.New(db), reg, nil, db, "tok")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	api.AttachUsage(h.poller, reg)
	h.api = api
	h.srv = httptest.NewServer(api.Handler)
	t.Cleanup(h.srv.Close) // runs before the store closes
	t.Cleanup(h.poller.Close)
	return h
}

// direct calls the registered handler without a websocket.
func (h *usageHarness) direct(params any, runScoped bool) (accountUsageResult, error) {
	h.t.Helper()
	raw, _ := json.Marshal(params)
	out, err := h.api.hs["account.usage"](context.Background(), &requestContext{runScoped: runScoped}, raw)
	if err != nil {
		return accountUsageResult{}, err
	}
	return out.(accountUsageResult), nil
}

func (h *usageHarness) mustDirect(params any) accountUsageResult {
	h.t.Helper()
	res, err := h.direct(params, false)
	if err != nil {
		h.t.Fatalf("account.usage error = %v", err)
	}
	return res
}

func usageParams(mode string, ids ...int64) map[string]any {
	p := map[string]any{}
	if mode != "" {
		p["mode"] = mode
	}
	if len(ids) > 0 {
		p["accountIds"] = ids
	}
	return p
}

func TestUsage_All(t *testing.T) {
	h := newUsageHarness(t, 3)
	res := h.mustDirect(usageParams(""))
	if !res.Enabled || res.TTLSeconds != 300 || len(res.Reports) != 3 {
		t.Fatalf("result = %+v", res)
	}
	for i, r := range res.Reports {
		if r.AccountID != h.ids[i] {
			t.Fatalf("report %d accountId = %d, want %d (id order)", i, r.AccountID, h.ids[i])
		}
		if want := fmt.Sprintf("anthropic-%d", r.AccountID); r.Key != want || r.Provider != "anthropic" {
			t.Fatalf("report %d key/provider = %q/%q", i, r.Key, r.Provider)
		}
		if r.DisplayName != fmt.Sprintf("Label %d", i+1) {
			t.Fatalf("displayName = %q", r.DisplayName)
		}
	}
}

func TestUsage_IDsDedupedInRequestOrder(t *testing.T) {
	h := newUsageHarness(t, 3)
	res := h.mustDirect(usageParams("cached", h.ids[2], h.ids[0], h.ids[2]))
	if len(res.Reports) != 2 || res.Reports[0].AccountID != h.ids[2] || res.Reports[1].AccountID != h.ids[0] {
		t.Fatalf("reports = %+v", res.Reports)
	}
}

func TestUsage_UnknownIDErrors(t *testing.T) {
	h := newUsageHarness(t, 1)
	_, err := h.direct(usageParams("touch", h.ids[0], 9999), false)
	if err == nil || err.Error() != "account.usage: unknown account 9999" {
		t.Fatalf("error = %v", err)
	}
	if h.fetcher.Calls() != 0 {
		t.Fatalf("fetch calls = %d, want 0", h.fetcher.Calls())
	}
	if _, err := h.direct(usageParams("bogus"), false); err == nil {
		t.Fatal("unknown mode: error = nil")
	}
}

func TestUsage_DisabledReturnsEnabledFalse(t *testing.T) {
	h := newUsageHarness(t, 2, quota.WithEnabled(false))
	for _, mode := range []string{"cached", "touch", "refresh"} {
		res := h.mustDirect(usageParams(mode))
		if res.Enabled || res.Reports == nil || len(res.Reports) != 0 {
			t.Fatalf("%s: result = %+v, want enabled=false and empty reports", mode, res)
		}
	}
	if h.fetcher.Calls() != 0 {
		t.Fatalf("fetch calls = %d, want 0", h.fetcher.Calls())
	}
}

func TestUsage_DefaultModeIsCachedZeroCalls(t *testing.T) {
	h := newUsageHarness(t, 2)
	// over the real websocket, with no params at all
	ws := dialWS(t, h.srv, "tok")
	sendRequest(t, ws, "u", "account.usage", nil)
	resp := readEnvelopeFor(t, ws, "u", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("error = %v", resp.Error.Message)
	}
	var res accountUsageResult
	mustDecode(t, resp.Result, &res)
	if len(res.Reports) != 2 || res.Reports[0].Report != nil {
		t.Fatalf("result = %+v", res)
	}
	if h.fetcher.Calls() != 0 {
		t.Fatalf("fetch calls = %d, want 0", h.fetcher.Calls())
	}
}

func TestUsage_TouchRespectsTTL(t *testing.T) {
	h := newUsageHarness(t, 1)
	first := h.mustDirect(usageParams("touch"))
	if first.Reports[0].Report == nil || first.Reports[0].Report.PlanLabel != "pro" {
		t.Fatalf("first touch report = %+v", first.Reports[0].Report)
	}
	h.mustDirect(usageParams("touch"))
	h.clk.Advance(4 * time.Minute)
	h.mustDirect(usageParams("touch"))
	if got := h.fetcher.Calls(); got != 1 {
		t.Fatalf("calls within TTL = %d, want 1", got)
	}
	h.clk.Advance(2 * time.Minute)
	h.mustDirect(usageParams("touch"))
	if got := h.fetcher.Calls(); got != 2 {
		t.Fatalf("calls after TTL = %d, want 2", got)
	}
}

func TestUsage_RefreshWaitsAndReturns(t *testing.T) {
	h := newUsageHarness(t, 2)
	h.fetcher.fn = func(ctx context.Context, a accounts.Account) (quota.Report, error) {
		time.Sleep(30 * time.Millisecond)
		return usageFetcherDefault(), nil
	}
	res := h.mustDirect(usageParams("refresh"))
	if h.fetcher.Calls() != 2 {
		t.Fatalf("calls = %d, want 2", h.fetcher.Calls())
	}
	for _, r := range res.Reports {
		if r.Report == nil || r.InFlight || r.Stale {
			t.Fatalf("report = %+v, want fresh and settled", r)
		}
	}
}

func usageFetcherDefault() quota.Report {
	used := 25.0
	return quota.Report{Status: quota.StatusAvailable, PlanLabel: "pro",
		Windows: []quota.Window{{ID: "five_hour", Label: "5-hour", UsedPct: &used, Tone: quota.ToneOK}}}
}

func TestUsage_RefreshTimeoutReturnsCached(t *testing.T) {
	old := usageWaitTimeout
	usageWaitTimeout = 80 * time.Millisecond
	t.Cleanup(func() { usageWaitTimeout = old })

	h := newUsageHarness(t, 1)
	gate := make(chan struct{})
	t.Cleanup(func() { close(gate) }) // runs before poller.Close (LIFO)
	h.fetcher.fn = func(ctx context.Context, a accounts.Account) (quota.Report, error) {
		select {
		case <-gate:
		case <-ctx.Done():
			return quota.Report{}, ctx.Err()
		}
		return usageFetcherDefault(), nil
	}
	start := time.Now()
	res, err := h.direct(usageParams("refresh"), false)
	if err != nil {
		t.Fatalf("error = %v, want a result", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("did not return at the wait timeout")
	}
	r := res.Reports[0]
	if !r.InFlight || r.Report != nil {
		t.Fatalf("report = %+v, want inFlight with the (empty) cache", r)
	}
}

func TestUsage_RefreshBlockedReturnsCachedWithReasonNotError(t *testing.T) {
	step := func(h *usageHarness, want string, wantNext bool) {
		t.Helper()
		before := h.fetcher.Calls()
		res, err := h.direct(usageParams("refresh"), false)
		if err != nil {
			t.Fatalf("blocked refresh returned error %v", err)
		}
		r := res.Reports[0]
		if r.RefreshBlocked == nil || *r.RefreshBlocked != want {
			t.Fatalf("refreshBlocked = %v, want %q", r.RefreshBlocked, want)
		}
		if (r.NextRefreshAt != nil) != wantNext {
			t.Fatalf("nextRefreshAt = %v, want present=%v", r.NextRefreshAt, wantNext)
		}
		if h.fetcher.Calls() != before {
			t.Fatalf("a blocked refresh made %d fetch(es)", h.fetcher.Calls()-before)
		}
	}
	t.Run("floor", func(t *testing.T) {
		h := newUsageHarness(t, 1)
		h.mustDirect(usageParams("refresh"))
		// first refresh succeeded; the immediate second is under the floor
		res, err := h.direct(usageParams("refresh"), false)
		if err != nil || res.Reports[0].Report == nil || res.Reports[0].RefreshBlocked == nil || *res.Reports[0].RefreshBlocked != "floor" {
			t.Fatalf("res = %+v err = %v", res, err)
		}
		if h.fetcher.Calls() != 1 {
			t.Fatalf("calls = %d, want 1", h.fetcher.Calls())
		}
		step(h, "floor", true)
	})
	t.Run("budget", func(t *testing.T) {
		h := newUsageHarness(t, 1)
		for i := 0; i < 6; i++ {
			h.mustDirect(usageParams("refresh"))
			h.clk.Advance(31 * time.Second)
		}
		if h.fetcher.Calls() != 6 {
			t.Fatalf("calls = %d, want 6", h.fetcher.Calls())
		}
		step(h, "budget", true)
	})
	t.Run("backoff", func(t *testing.T) {
		h := newUsageHarness(t, 1)
		h.fetcher.fn = func(context.Context, accounts.Account) (quota.Report, error) {
			return quota.Report{Status: quota.StatusError, Error: "timeout"}, errors.New("boom")
		}
		h.mustDirect(usageParams("refresh"))
		h.clk.Advance(31 * time.Second)
		step(h, "backoff", true)
	})
	t.Run("rejected", func(t *testing.T) {
		h := newUsageHarness(t, 1)
		h.fetcher.fn = func(context.Context, accounts.Account) (quota.Report, error) {
			return quota.Report{Status: quota.StatusUnavailable, Problem: &quota.Problem{Kind: quota.ProblemRejected, Status: 401}}, nil
		}
		h.mustDirect(usageParams("refresh"))
		h.clk.Advance(31 * time.Second)
		step(h, "rejected", false)
	})
}

func TestUsage_RefreshFromRunScopedCallerIsTouch(t *testing.T) {
	h := newUsageHarness(t, 1)
	for i := 0; i < 3; i++ {
		if _, err := h.direct(usageParams("refresh"), true); err != nil {
			t.Fatal(err)
		}
		h.clk.Advance(40 * time.Second) // past the refresh floor, inside the TTL
	}
	if got := h.fetcher.Calls(); got != 1 {
		t.Fatalf("run-scoped refresh calls = %d, want 1 (touch)", got)
	}
	// a normal caller's refresh in the same state does reach the network
	h.mustDirect(usageParams("refresh"))
	if got := h.fetcher.Calls(); got != 2 {
		t.Fatalf("user refresh calls = %d, want 2", got)
	}
}

func TestUsage_ConnRunScopedPlumbedToRequestContext(t *testing.T) {
	var got atomic.Bool
	hs := map[string]handlerFunc{"probe": func(_ context.Context, rc *requestContext, _ json.RawMessage) (any, error) {
		got.Store(rc.runScoped)
		return "ok", nil
	}}
	client, server := newPipeTransportPair()
	c := newConn(server, hs)
	c.runScoped = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.serve(ctx)
	data, _ := json.Marshal(envelope{ID: "1", Method: "probe"})
	if err := client.Send(data); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Receive(); err != nil {
		t.Fatal(err)
	}
	client.Close()
	if !got.Load() {
		t.Fatal("rc.runScoped = false on a runScoped conn")
	}
}

func TestUsage_ReconnectRelistCachedZeroCalls(t *testing.T) {
	h := newUsageHarness(t, 2)
	h.mustDirect(usageParams("touch"))
	base := h.fetcher.Calls()
	for i := 0; i < 3; i++ { // each iteration is a fresh connection
		ws := dialWS(t, h.srv, "tok")
		sendRequest(t, ws, "r", "account.usage", usageParams("cached"))
		resp := readEnvelopeFor(t, ws, "r", 5*time.Second)
		var res accountUsageResult
		mustDecode(t, resp.Result, &res)
		if len(res.Reports) != 2 || res.Reports[0].Report == nil {
			t.Fatalf("relist = %+v, want cached reports", res)
		}
		_ = ws.Close()
	}
	if h.fetcher.Calls() != base {
		t.Fatalf("relist made %d fetches", h.fetcher.Calls()-base)
	}
}

func TestUsage_TenClientsBurstOneCall(t *testing.T) {
	h := newUsageHarness(t, 1)
	h.fetcher.fn = func(context.Context, accounts.Account) (quota.Report, error) {
		time.Sleep(150 * time.Millisecond)
		return usageFetcherDefault(), nil
	}
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := h.direct(usageParams("touch"), false)
			if err == nil && res.Reports[0].Report == nil {
				err = errors.New("no report")
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := h.fetcher.Calls(); got != 1 {
		t.Fatalf("burst calls = %d, want 1", got)
	}
}
