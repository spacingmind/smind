package wsapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
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
	// the cooldown's own keys: model and window are optional
	keys := func(c usageCooldown) []string {
		b, _ := json.Marshal(c)
		var m map[string]json.RawMessage
		_ = json.Unmarshal(b, &m)
		var out []string
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	if got, want := keys(usageCooldown{}), []string{"reason", "scope", "until"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cooldown keys = %v, want %v", got, want)
	}
	if got, want := keys(usageCooldown{Model: "m", Window: "weekly"}), []string{"model", "reason", "scope", "until", "window"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("full cooldown keys = %v, want %v", got, want)
	}
}

func TestUsage_AccountCooldownAndObservedFieldsRoundTrip(t *testing.T) {
	t.Parallel()
	in := AccountUsage{
		AccountID: 1, Key: "anthropic-1",
		Cooldown: &usageCooldown{Until: time.Unix(0, 0).UTC().Format(time.RFC3339), Scope: "model", Model: "opus", Reason: "rejected", Window: "weekly_model"},
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
	passive *quota.Passive // set by newPassiveUsageHarness
}

func newUsageHarness(t *testing.T, accountCount int, opts ...quota.Option) *usageHarness {
	t.Helper()
	return buildUsageHarness(t, accountCount, false, opts...)
}

// newPassiveUsageHarness is newUsageHarness with a real quota.Passive (fixed
// 5 s grace) attached through AttachUsage, over the harness's store.
func newPassiveUsageHarness(t *testing.T, accountCount int, opts ...quota.Option) *usageHarness {
	t.Helper()
	return buildUsageHarness(t, accountCount, true, opts...)
}

func buildUsageHarness(t *testing.T, accountCount int, withPassive bool, opts ...quota.Option) *usageHarness {
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
	if withPassive {
		h.passive = quota.NewPassive(db, quota.WithPassiveGrace(func() time.Duration { return passiveTestGrace }))
		t.Cleanup(h.passive.Close) // runs before the store closes
		api.AttachUsage(h.poller, reg, h.passive)
	} else {
		api.AttachUsage(h.poller, reg, nil)
	}
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
	// An allowlisted method name: a run-scoped conn refuses any other.
	hs := map[string]handlerFunc{"run.list": func(_ context.Context, rc *requestContext, _ json.RawMessage) (any, error) {
		got.Store(rc.runScoped)
		return "ok", nil
	}}
	client, server := newPipeTransportPair()
	c := newConn(server, hs)
	c.runScoped = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.serve(ctx)
	data, _ := json.Marshal(envelope{ID: "1", Method: "run.list"})
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

// ---- account.usage events ----

func usageEventConn(t *testing.T, h *usageHarness, topics ...string) *eventConn {
	t.Helper()
	c := newEventConn(t, dialWS(t, h.srv, "tok"))
	c.subscribe("sub", topics...)
	return c
}

func decodeUsageEvent(t *testing.T, e eventNotification) AccountUsage {
	t.Helper()
	if e.Topic != TopicAccountUsage {
		t.Fatalf("event topic = %q, want %q", e.Topic, TopicAccountUsage)
	}
	b, _ := json.Marshal(e.Payload)
	var p accountUsagePayload
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	return p.Usage
}

func TestUsage_EventAfterEveryFetchNotOnCacheHit(t *testing.T) {
	h := newUsageHarness(t, 1)
	c := usageEventConn(t, h, TopicAccountUsage)

	h.mustDirect(usageParams("touch")) // fetch #1: success
	u := decodeUsageEvent(t, c.nextEvent(5*time.Second))
	if u.AccountID != h.ids[0] || u.Report == nil || u.Report.PlanLabel != "pro" {
		t.Fatalf("event usage = %+v", u)
	}

	h.mustDirect(usageParams("touch"))   // cache hit
	h.mustDirect(usageParams("cached"))  // peek
	h.mustDirect(usageParams("refresh")) // refused: under the floor
	// (a read deadline breaks the ws, so absence is proven by ordering: the
	// next event must be the next real fetch's, below.)
	if h.fetcher.Calls() != 1 {
		t.Fatalf("calls = %d, want 1", h.fetcher.Calls())
	}

	// error and unavailable outcomes also emit
	h.clk.Advance(time.Minute)
	h.fetcher.fn = func(context.Context, accounts.Account) (quota.Report, error) {
		return quota.Report{Status: quota.StatusError, Error: "timeout"}, errors.New("x")
	}
	h.mustDirect(usageParams("refresh"))
	if u := decodeUsageEvent(t, c.nextEvent(5*time.Second)); u.LastError == nil {
		t.Fatalf("error fetch event = %+v, want lastError", u)
	}
	h.clk.Advance(2 * time.Minute)
	h.fetcher.fn = func(context.Context, accounts.Account) (quota.Report, error) {
		return quota.Report{Status: quota.StatusUnavailable, Problem: &quota.Problem{Kind: quota.ProblemNoQuota}}, nil
	}
	h.mustDirect(usageParams("refresh"))
	if u := decodeUsageEvent(t, c.nextEvent(5*time.Second)); u.Report == nil || u.Report.Status != quota.StatusUnavailable {
		t.Fatalf("unavailable fetch event = %+v", u)
	}
}

func TestUsage_RefreshAllEmitsOneEventPerAccountAsEachSettles(t *testing.T) {
	h := newUsageHarness(t, 3)
	slow := h.ids[0]
	release := make(chan struct{})
	h.fetcher.fn = func(ctx context.Context, a accounts.Account) (quota.Report, error) {
		if a.ID == slow {
			select {
			case <-release:
			case <-ctx.Done():
				return quota.Report{}, ctx.Err()
			}
		}
		return usageFetcherDefault(), nil
	}
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	c := usageEventConn(t, h, TopicAccountUsage)

	sendRequest(t, c.ws, "all", "account.usage", usageParams("refresh"))
	seen := map[int64]bool{}
	for i := 0; i < 2; i++ {
		u := decodeUsageEvent(t, c.nextEvent(5*time.Second))
		if u.AccountID == slow || seen[u.AccountID] {
			t.Fatalf("unexpected event for account %d (seen %v)", u.AccountID, seen)
		}
		seen[u.AccountID] = true
	}
	if len(c.responses) != 0 {
		t.Fatal("the RPC answered before the slow account settled")
	}
	close(release)
	if u := decodeUsageEvent(t, c.nextEvent(5*time.Second)); u.AccountID != slow {
		t.Fatalf("third event account = %d, want %d", u.AccountID, slow)
	}
	resp := c.nextResponse("all", 5*time.Second)
	var res accountUsageResult
	mustDecode(t, resp.Result, &res)
	if len(res.Reports) != 3 {
		t.Fatalf("reports = %d, want 3", len(res.Reports))
	}
	c.expectNoEvent(200 * time.Millisecond)
}

func TestUsage_TopicSubscribable(t *testing.T) {
	h := newUsageHarness(t, 0)
	c := newEventConn(t, dialWS(t, h.srv, "tok"))
	c.subscribe("s1", TopicAccountUsage)
	c.unsubscribe("u1", TopicAccountUsage)
	if !knownTopics["account.usage"] {
		t.Fatal("account.usage is not a known topic")
	}
}

func TestUsage_AccountRemovedEventFlow(t *testing.T) {
	h := newUsageHarness(t, 2)
	gate := make(chan struct{})
	h.fetcher.fn = func(ctx context.Context, a accounts.Account) (quota.Report, error) {
		if a.ID == h.ids[0] {
			select {
			case <-gate:
			case <-ctx.Done():
				return quota.Report{}, ctx.Err()
			}
		}
		return usageFetcherDefault(), nil
	}
	t.Cleanup(func() {
		select {
		case <-gate:
		default:
			close(gate)
		}
	})
	c := usageEventConn(t, h, TopicAccountUsage, TopicAccountRemoved)

	// account 2 fetches normally, account 1's fetch is held open
	h.mustDirect(usageParams("touch", h.ids[1]))
	if u := decodeUsageEvent(t, c.nextEvent(5*time.Second)); u.AccountID != h.ids[1] {
		t.Fatalf("usage event account = %d", u.AccountID)
	}
	sendRequest(t, c.ws, "t", "account.usage", usageParams("touch", h.ids[0]))
	time.Sleep(100 * time.Millisecond) // let the fetch start
	if err := h.reg.Delete(h.ids[0]); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	e := c.nextEvent(5 * time.Second)
	if e.Topic != TopicAccountRemoved {
		t.Fatalf("event = %q, want %q", e.Topic, TopicAccountRemoved)
	}
	close(gate)
	// the removed account's fetch settles: no usage event for it
	c.expectNoEvent(400 * time.Millisecond)
}

func TestUsage_NoSecretInWSAPIPayload(t *testing.T) {
	const (
		access  = "SENTINEL_ACCESS_TOKEN_9f3a"
		refresh = "SENTINEL_REFRESH_TOKEN_77c1"
		apiKey  = "sk-ant-SENTINEL_KEY_55d0"
		body    = "SENTINEL_PROVIDER_BODY_e12"
	)
	h := newUsageHarness(t, 0)
	oauth, err := h.reg.AddOAuth("anthropic", "oauth", accounts.OAuthCredential{
		AccessToken: access, RefreshToken: refresh, ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	keyed, err := h.reg.AddAPIKey("openai", "keyed", apiKey)
	if err != nil {
		t.Fatal(err)
	}
	h.fetcher.fn = func(_ context.Context, a accounts.Account) (quota.Report, error) {
		if a.ID == keyed.ID {
			return quota.Report{Status: quota.StatusError, Error: "timeout"}, errors.New(body)
		}
		return usageFetcherDefault(), nil
	}
	c := usageEventConn(t, h, TopicAccountUsage)
	sendRequest(t, c.ws, "r", "account.usage", usageParams("refresh"))
	var raw [][]byte
	for i := 0; i < 2; i++ {
		e := c.nextEvent(5 * time.Second)
		b, _ := json.Marshal(e)
		raw = append(raw, b)
	}
	resp := c.nextResponse("r", 5*time.Second)
	raw = append(raw, resp.Result)
	// also a cached re-read
	sendRequest(t, c.ws, "c", "account.usage", usageParams("cached"))
	raw = append(raw, c.nextResponse("c", 5*time.Second).Result)

	banned := []string{access, refresh, apiKey, body, "credential_data", "credentialData", "access_token",
		"accessToken", "refresh_token", "refreshToken", "apiKey", "fingerprint"}
	for _, b := range raw {
		for _, s := range banned {
			if strings.Contains(string(b), s) {
				t.Fatalf("payload leaks %q: %s", s, b)
			}
		}
	}
	_ = oauth
}

func TestUsage_NextRefreshAtRoundsUpToWholeSecond(t *testing.T) {
	acc := accounts.Account{ID: 1, Provider: "anthropic"}
	at := time.Date(2026, 10, 12, 12, 0, 30, 700_000_000, time.UTC)
	got := usageFrom(acc, quota.Result{NextRefreshAt: &at}).NextRefreshAt
	if got == nil || *got != "2026-10-12T12:00:31Z" {
		t.Fatalf("nextRefreshAt = %v, want 2026-10-12T12:00:31Z", got)
	}
	exact := time.Date(2026, 10, 12, 12, 0, 30, 0, time.UTC)
	got = usageFrom(acc, quota.Result{NextRefreshAt: &exact}).NextRefreshAt
	if got == nil || *got != "2026-10-12T12:00:30Z" {
		t.Fatalf("exact-second nextRefreshAt = %v, want unchanged", got)
	}
}

func TestUsage_AccountRemovedDuringWaitIsOmitted(t *testing.T) {
	h := newUsageHarness(t, 2)
	h.fetcher.fn = func(_ context.Context, a accounts.Account) (quota.Report, error) {
		if a.ID == h.ids[0] {
			_ = h.reg.Delete(a.ID)
		}
		return usageFetcherDefault(), nil
	}
	res := h.mustDirect(usageParams("touch"))
	if len(res.Reports) != 1 || res.Reports[0].AccountID != h.ids[1] {
		t.Fatalf("reports = %+v, want only the surviving account %d", res.Reports, h.ids[1])
	}
}

// ---- passive cooldown and observed windows (ADR-0028 §7, PR-F surfaces) ----

const (
	passiveTestGrace = 5 * time.Second
	unifiedPrefix    = "Anthropic-Ratelimit-Unified-"
)

func hdrKV(kv ...string) http.Header {
	h := http.Header{}
	for i := 0; i+1 < len(kv); i += 2 {
		h.Set(kv[i], kv[i+1])
	}
	return h
}

func epochOf(t time.Time) string { return strconv.FormatInt(t.Unix(), 10) }

// rejected5h declares a 5h rejection that resets at reset.
func rejected5h(reset time.Time) http.Header {
	return hdrKV(unifiedPrefix+"Status", "rejected", unifiedPrefix+"5h-Status", "rejected", unifiedPrefix+"5h-Reset", epochOf(reset))
}

// rejectedOIOnly is the 7d_oi-only (model-scoped) rejection, deadline from Retry-After.
func rejectedOIOnly(retryAfterSec int) http.Header {
	return hdrKV(unifiedPrefix+"Status", "rejected", unifiedPrefix+"5h-Status", "allowed", unifiedPrefix+"7d-Status", "allowed",
		unifiedPrefix+"7d_oi-Status", "rejected", "Retry-After", strconv.Itoa(retryAfterSec))
}

func (h *usageHarness) observe(id int64, model string, status int, header http.Header) {
	h.passive.Observe(quota.Response{AccountID: id, Provider: "anthropic", Model: model, Status: status, Header: header, At: time.Now()})
}

func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func (res accountUsageResult) byID(t *testing.T, id int64) AccountUsage {
	t.Helper()
	for _, r := range res.Reports {
		if r.AccountID == id {
			return r
		}
	}
	t.Fatalf("no report for account %d in %+v", id, res.Reports)
	return AccountUsage{}
}

func TestUsage_CooldownAccountAndModelScopeInRPC(t *testing.T) {
	h := newPassiveUsageHarness(t, 4)
	a, b, c, d := h.ids[0], h.ids[1], h.ids[2], h.ids[3]
	at := time.Now()
	reset := at.Add(2 * time.Hour)

	h.observe(a, "claude-opus-5", http.StatusTooManyRequests, rejectedOIOnly(10*60))     // model scope only
	h.observe(b, "claude-opus-5", http.StatusTooManyRequests, rejectedOIOnly(10*60))     // model scope, then
	h.observe(b, "claude-sonnet-4", http.StatusTooManyRequests, rejected5h(reset))       // an account-wide one: it wins
	h.observe(c, "claude-haiku", http.StatusTooManyRequests, hdrKV("Retry-After", "30")) // transient back-off: not shown
	h.observe(d, "m", http.StatusTooManyRequests, hdrKV(unifiedPrefix+"Status", "rejected", unifiedPrefix+"5h-Status", "rejected", unifiedPrefix+"5h-Reset", "garbage"))

	res := h.mustDirect(usageParams("cached"))
	ra := res.byID(t, a)
	if ra.Cooldown == nil || ra.Cooldown.Scope != "model" || ra.Cooldown.Model != "claude-opus-5" ||
		ra.Cooldown.Reason != "rejected" || ra.Cooldown.Window != "weekly_model" {
		t.Fatalf("model-scoped cooldown = %+v", ra.Cooldown)
	}
	if want := rfc3339(at.Add(10*time.Minute + passiveTestGrace)); ra.Cooldown.Until != want {
		t.Errorf("model cooldown until = %s, want %s", ra.Cooldown.Until, want)
	}
	rb := res.byID(t, b)
	if rb.Cooldown == nil || rb.Cooldown.Scope != "account" || rb.Cooldown.Model != "" ||
		rb.Cooldown.Reason != "rejected" || rb.Cooldown.Window != "five_hour" {
		t.Fatalf("account-wide cooldown = %+v", rb.Cooldown)
	}
	if want := rfc3339(time.Unix(reset.Unix(), 0).Add(passiveTestGrace)); rb.Cooldown.Until != want {
		t.Errorf("account cooldown until = %s, want %s", rb.Cooldown.Until, want)
	}
	if rc := res.byID(t, c); rc.Cooldown != nil {
		t.Errorf("a bare-429 back-off is shown as a cooldown: %+v", rc.Cooldown)
	}
	rd := res.byID(t, d)
	if rd.Cooldown == nil || rd.Cooldown.Reason != "fallback" || rd.Cooldown.Window != "five_hour" || rd.Cooldown.Scope != "account" {
		t.Fatalf("fallback cooldown = %+v", rd.Cooldown)
	}
	if h.fetcher.Calls() != 0 {
		t.Errorf("fetcher calls = %d, want 0", h.fetcher.Calls())
	}

	// The same rows arrive over the wire with exactly these JSON keys.
	raw, _ := json.Marshal(rb)
	if !strings.Contains(string(raw), `"cooldown":{"until":"`) || !strings.Contains(string(raw), `"window":"five_hour"`) {
		t.Errorf("wire row = %s", raw)
	}
}

func TestUsage_EventPublishedOnCooldownStartAndClear(t *testing.T) {
	h := newPassiveUsageHarness(t, 2)
	id := h.ids[0]
	c := usageEventConn(t, h, TopicAccountUsage)

	h.observe(id, "m", http.StatusTooManyRequests, rejected5h(time.Now().Add(time.Hour)))
	u := decodeUsageEvent(t, c.nextEvent(5*time.Second))
	if u.AccountID != id || u.Cooldown == nil || u.Cooldown.Window != "five_hour" || u.Cooldown.Scope != "account" {
		t.Fatalf("start event = %+v cooldown %+v", u, u.Cooldown)
	}

	first := u.Cooldown.Until
	h.observe(id, "m", http.StatusTooManyRequests, rejected5h(time.Now().Add(3*time.Hour))) // extension
	u = decodeUsageEvent(t, c.nextEvent(5*time.Second))
	if u.Cooldown == nil || u.Cooldown.Until <= first {
		t.Fatalf("extension event = %+v, want a later end than %s", u.Cooldown, first)
	}

	h.passive.ClearCooldown(id) // credential replaced
	u = decodeUsageEvent(t, c.nextEvent(5*time.Second))
	if u.AccountID != id || u.Cooldown != nil {
		t.Fatalf("clear event = %+v cooldown %+v, want none", u, u.Cooldown)
	}
	// a removed account publishes nothing, and nothing fetched anything
	if err := h.reg.Delete(h.ids[1]); err != nil {
		t.Fatal(err)
	}
	h.observe(h.ids[1], "m", http.StatusTooManyRequests, rejected5h(time.Now().Add(time.Hour)))
	c.expectNoEvent(300 * time.Millisecond)
	if h.fetcher.Calls() != 0 {
		t.Errorf("fetcher calls = %d, want 0", h.fetcher.Calls())
	}
}

func TestUsage_CachedModeStillMakesNoHTTPCall(t *testing.T) {
	h := newPassiveUsageHarness(t, 2)
	c := usageEventConn(t, h, TopicAccountUsage)
	h.observe(h.ids[0], "m", http.StatusTooManyRequests, rejected5h(time.Now().Add(time.Hour)))
	h.observe(h.ids[1], "m", http.StatusOK, hdrKV(unifiedPrefix+"5h-Status", "allowed", unifiedPrefix+"5h-Utilization", "0.4"))
	decodeUsageEvent(t, c.nextEvent(5*time.Second))
	decodeUsageEvent(t, c.nextEvent(5*time.Second))
	for i := 0; i < 5; i++ {
		h.mustDirect(usageParams("cached"))
		h.mustDirect(usageParams(""))
	}
	h.passive.ClearCooldown(h.ids[0])
	if got := h.fetcher.Calls(); got != 0 {
		t.Fatalf("usage fetcher calls = %d, want 0 (cooldowns, observations and events never fetch)", got)
	}
	// A typed-nil passive is the same as no passive.
	var none *quota.Passive
	h2 := newUsageHarness(t, 1)
	h2.api.AttachUsage(h2.poller, h2.reg, none)
	if res := h2.mustDirect(usageParams("cached")); len(res.Reports) != 1 || res.Reports[0].Cooldown != nil {
		t.Fatalf("typed-nil passive result = %+v", res)
	}
	if h2.fetcher.Calls() != 0 {
		t.Fatalf("fetcher calls with a typed-nil passive = %d", h2.fetcher.Calls())
	}
}

func TestUsage_DisplayRuleObservedWhenNoFreshFetch(t *testing.T) {
	obsHeader := func(util string) http.Header {
		return hdrKV(unifiedPrefix+"5h-Status", "allowed_warning", unifiedPrefix+"5h-Utilization", util,
			unifiedPrefix+"5h-Reset", epochOf(time.Now().Add(time.Hour)), unifiedPrefix+"7d-Status", "allowed", unifiedPrefix+"7d-Utilization", "0.10")
	}
	window := func(t *testing.T, r AccountUsage, id string) quota.Window {
		t.Helper()
		if r.Report != nil {
			for _, w := range r.Report.Windows {
				if w.ID == id {
					return w
				}
			}
		}
		t.Fatalf("no %s window in %+v", id, r.Report)
		return quota.Window{}
	}

	h := newPassiveUsageHarness(t, 1)
	id := h.ids[0]

	// No fetched report: the observation is the report, flagged observed.
	h.observe(id, "m", http.StatusOK, obsHeader("0.80"))
	r := h.mustDirect(usageParams("cached")).byID(t, id)
	if w := window(t, r, "five_hour"); w.Source != "observed" || w.Status != "allowed_warning" || w.UsedPct == nil || *w.UsedPct != 80 {
		t.Fatalf("observed-only window = %+v", w)
	}

	// A fresh fetched report keeps its numbers; the observation never overrides them.
	h.mustDirect(usageParams("touch"))
	h.observe(id, "m", http.StatusOK, obsHeader("0.95"))
	w := window(t, h.mustDirect(usageParams("cached")).byID(t, id), "five_hour")
	if w.Source == "observed" || w.UsedPct == nil || *w.UsedPct != 25 {
		t.Fatalf("fresh fetched window = %+v, want the fetched 25%%", w)
	}

	// Once the fetched report is older than the TTL and a newer observation
	// exists, the observed window replaces the fetched one.
	h.clk.Advance(10 * time.Minute)
	h.observe(id, "m", http.StatusOK, obsHeader("0.96"))
	r = h.mustDirect(usageParams("cached")).byID(t, id)
	if w := window(t, r, "five_hour"); w.Source != "observed" || w.UsedPct == nil || *w.UsedPct != 96 {
		t.Fatalf("stale fetched + newer observation: window = %+v", w)
	}
	if w := window(t, r, "weekly"); w.Source != "observed" {
		t.Fatalf("observed weekly window not added: %+v", w)
	}
	if h.fetcher.Calls() != 1 {
		t.Fatalf("fetcher calls = %d, want exactly the one explicit touch", h.fetcher.Calls())
	}
}

func TestUsage_EventPublishDoesNotBlockObserve(t *testing.T) {
	h := newPassiveUsageHarness(t, 1)
	id := h.ids[0]
	// A subscriber whose lock is held makes bus.Publish block until released.
	// Observe (the proxy goroutine) must not care.
	slow := newSubscriber()
	slow.subscribe([]string{TopicAccountUsage})
	h.api.bus.register(slow)
	slow.mu.Lock()
	released := false
	release := func() {
		if !released {
			released = true
			slow.mu.Unlock()
		}
	}
	t.Cleanup(release)

	done := make(chan time.Duration, 1)
	go func() {
		start := time.Now()
		for i := 1; i <= 200; i++ {
			h.observe(id, "m", http.StatusTooManyRequests, rejected5h(time.Now().Add(time.Duration(i)*time.Minute)))
		}
		done <- time.Since(start)
	}()
	select {
	case d := <-done:
		if d > 2*time.Second {
			t.Fatalf("200 Observe calls took %v with a blocked subscriber", d)
		}
	case <-time.After(5 * time.Second):
		release() // unblock the goroutine so the test can end
		t.Fatal("Observe blocked behind a subscriber that is not draining")
	}
	release()
	deadline := time.Now().Add(5 * time.Second)
	for {
		slow.mu.Lock()
		n := len(slow.queue)
		slow.mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no event reached the subscriber after it was released")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// 200 changes did not become 200 events: bursts coalesce per account.
	slow.mu.Lock()
	n := len(slow.queue)
	slow.mu.Unlock()
	if n >= 200 {
		t.Errorf("queued %d events for 200 changes of one account, want coalescing", n)
	}
}

func TestUsage_NoSecretsInCooldownPayload(t *testing.T) {
	const sentinel = "SENTINEL_HEADER_VALUE_3b7e"
	var logs bytes.Buffer
	oldOut := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(oldOut) })

	h := newPassiveUsageHarness(t, 1)
	id := h.ids[0]
	c := usageEventConn(t, h, TopicAccountUsage)
	header := hdrKV(
		unifiedPrefix+"Status", "rejected",
		unifiedPrefix+"5h-Status", "rejected",
		unifiedPrefix+"5h-Reset", sentinel+"_RESET", // unparseable: falls back, never echoed
		unifiedPrefix+"Representative-Claim", sentinel+"_CLAIM",
		unifiedPrefix+"Overage-Disabled-Reason", sentinel+"_OVERAGE",
		"Retry-After", sentinel+"_RA",
		"Anthropic-Workspace-Id", sentinel+"_WS",
		"Set-Cookie", "sid="+sentinel+"_COOKIE",
		"Authorization", "Bearer "+sentinel+"_AUTH",
		"X-Request-Id", sentinel+"_REQ",
	)
	h.observe(id, "m", http.StatusTooManyRequests, header)
	ev, _ := json.Marshal(c.nextEvent(5 * time.Second))
	rpc, _ := json.Marshal(h.mustDirect(usageParams("cached")))
	for name, b := range map[string]string{"event": string(ev), "rpc": string(rpc), "log": logs.String()} {
		if strings.Contains(b, sentinel) {
			t.Fatalf("%s leaks a header value: %s", name, b)
		}
	}
	if !strings.Contains(string(rpc), `"cooldown"`) {
		t.Fatalf("the rejection did not produce a cooldown: %s", rpc)
	}
}

func TestUsage_AccountUsageCooldownPicksLatestEndingScope(t *testing.T) {
	h := newPassiveUsageHarness(t, 3)
	a, b, c := h.ids[0], h.ids[1], h.ids[2]
	at := time.Now()

	// a: a short account-wide cooldown, a longer model-scoped one: the model one.
	h.observe(a, "m", http.StatusTooManyRequests, rejected5h(at.Add(10*time.Minute)))
	h.observe(a, "claude-opus-5", http.StatusTooManyRequests, rejectedOIOnly(3600))
	// b: the account-wide one ends last: it shows.
	h.observe(b, "claude-opus-5", http.StatusTooManyRequests, rejectedOIOnly(600))
	h.observe(b, "m", http.StatusTooManyRequests, rejected5h(at.Add(2*time.Hour)))
	// c: two model-scoped ones: the later one.
	h.observe(c, "claude-opus-5", http.StatusTooManyRequests, rejectedOIOnly(600))
	h.observe(c, "claude-sonnet-4", http.StatusTooManyRequests, rejectedOIOnly(1800))

	res := h.mustDirect(usageParams("cached"))
	ra := res.byID(t, a)
	if ra.Cooldown == nil || ra.Cooldown.Scope != "model" || ra.Cooldown.Model != "claude-opus-5" {
		t.Fatalf("a: cooldown = %+v, want the longer model-scoped one", ra.Cooldown)
	}
	if want := rfc3339(at.Add(time.Hour + passiveTestGrace)); ra.Cooldown.Until != want {
		t.Errorf("a: until = %s, want %s", ra.Cooldown.Until, want)
	}
	if rb := res.byID(t, b); rb.Cooldown == nil || rb.Cooldown.Scope != "account" || rb.Cooldown.Window != "five_hour" {
		t.Fatalf("b: cooldown = %+v, want the account-wide one", rb.Cooldown)
	}
	if rc := res.byID(t, c); rc.Cooldown == nil || rc.Cooldown.Model != "claude-sonnet-4" {
		t.Fatalf("c: cooldown = %+v, want the later model-scoped one", rc.Cooldown)
	}
}
