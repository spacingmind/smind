package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/quota"
	"github.com/spacingmind/smind/internal/routing"
	"github.com/spacingmind/smind/internal/store"
)

var usageTestNow = time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

// countingUsageFetcher is the poller's ReportFetcher in the proxy usage
// tests: every call is counted, and calls on blocked accounts hang until the
// poller shuts down. The proxy must cause none.
type countingUsageFetcher struct {
	calls   atomic.Int64
	blockID int64
	entered chan struct{}
}

func (f *countingUsageFetcher) Fetch(ctx context.Context, a accounts.Account) (quota.Report, error) {
	f.calls.Add(1)
	if a.ID == f.blockID {
		select {
		case f.entered <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return quota.Report{}, ctx.Err()
	}
	return quota.Report{}, errors.New("boom")
}

// newEnabledRouting builds a registry and a Router over a real, enabled
// Poller on a fixed clock whose fetcher is f.
func newEnabledRouting(t *testing.T, s *store.Store, f quota.ReportFetcher) (*accounts.Registry, *routing.Router, *quota.Poller) {
	t.Helper()
	reg := accounts.New(s)
	now := func() time.Time { return usageTestNow }
	poller := quota.New(s, reg, f, quota.WithEnabled(true), quota.WithTTL(5*time.Minute),
		quota.WithClock(now), quota.WithJitter(func() float64 { return 0 }))
	t.Cleanup(poller.Close)
	return reg, routing.New(s, reg, poller, routing.WithClock(now)), poller
}

func seedUsageReport(t *testing.T, s *store.Store, accountID int64, polledAt time.Time, windows ...quota.Window) {
	t.Helper()
	data, err := quota.EncodeReport(quota.Report{Status: quota.StatusAvailable, Windows: windows, FetchedAt: polledAt})
	if err != nil {
		t.Fatalf("EncodeReport() error = %v", err)
	}
	if _, err := s.CreateQuotaSnapshot(store.QuotaSnapshot{AccountID: accountID, UsageData: data, PolledAt: polledAt, ExpiresAt: polledAt.Add(5 * time.Minute)}); err != nil {
		t.Fatalf("CreateQuotaSnapshot() error = %v", err)
	}
}

func usageWindow(id string, used float64, resets time.Time, scope *quota.Scope) quota.Window {
	rem := 100 - used
	if rem < 0 {
		rem = 0
	}
	return quota.Window{ID: id, UsedPct: &used, RemainingPct: &rem, ResetsAt: &resets, Scope: scope}
}

// keyRecordingUpstream answers 200 and records the x-api-key of each request.
func keyRecordingUpstream(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		keys = append(keys, r.Header.Get("x-api-key"))
		mu.Unlock()
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), keys...)
	}
}

// send posts a messages request as its own session (distinct credential).
func sendModel(p *proxy, session, model string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"`+model+`"}`))
	req.Header.Set("Authorization", "Bearer "+session)
	w := httptest.NewRecorder()
	p.handleAnthropic(w, req)
	return w
}

func TestProxy_PassesModelToRouter(t *testing.T) {
	t.Parallel()

	up, keys := keyRecordingUpstream(t)
	s := newTestStoreForProxy(t)
	reg, router, _ := newEnabledRouting(t, s, &countingUsageFetcher{blockID: -1})
	p := newProxy(reg, router, s, withAnthropicHTTPClient(testHTTPClient(t, up)))
	t.Cleanup(p.Close)

	a1 := addAPIKeyAccount(t, reg, providerAnthropic, "a1", "sk-account-1")
	addAPIKeyAccount(t, reg, providerAnthropic, "a2", "sk-account-2")
	week := usageTestNow.Add(48 * time.Hour)
	seedUsageReport(t, s, a1, usageTestNow, usageWindow("weekly_model_opus", 100, week, &quota.Scope{Kind: quota.ScopeModel, Name: "Opus"}))

	if w := sendModel(p, "opus", "claude-opus-4-1"); w.Code != http.StatusOK {
		t.Fatalf("opus status = %d; body = %s", w.Code, w.Body.String())
	}
	if w := sendModel(p, "sonnet", "claude-sonnet-4-5"); w.Code != http.StatusOK {
		t.Fatalf("sonnet status = %d; body = %s", w.Code, w.Body.String())
	}
	got := keys()
	if len(got) != 2 || got[0] != "sk-account-2" || got[1] != "sk-account-1" {
		t.Errorf("upstream keys = %v, want [sk-account-2 sk-account-1]", got)
	}
}

func TestProxy_ExhaustedPoolReturns503WithResetMessage(t *testing.T) {
	t.Parallel()

	s := newTestStoreForProxy(t)
	reg, router, _ := newEnabledRouting(t, s, &countingUsageFetcher{blockID: -1})
	p := newProxy(reg, router, s)
	t.Cleanup(p.Close)

	o1 := addAPIKeyAccount(t, reg, providerOpenAI, "o1", "sk-openai-real")
	resets := usageTestNow.Add(3 * time.Hour)
	seedUsageReport(t, s, o1, usageTestNow, usageWindow("five_hour", 100, resets, nil))

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	p.handleOpenAI(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", w.Code, w.Body.String())
	}
	var body struct {
		Error struct {
			Message string  `json:"message"`
			Type    string  `json:"type"`
			Code    *string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v; body = %s", err, w.Body.String())
	}
	if body.Error.Code != nil || !strings.HasPrefix(body.Error.Message, "route request: ") {
		t.Errorf("body = %+v, want openai error shape with null code and route request prefix", body)
	}
	if !strings.Contains(body.Error.Message, resets.Format(time.RFC3339)) {
		t.Errorf("message = %q, want reset time %s", body.Error.Message, resets.Format(time.RFC3339))
	}

	// Anthropic error shape stays intact too.
	a1 := addAPIKeyAccount(t, reg, providerAnthropic, "a1", "sk-account-1")
	seedUsageReport(t, s, a1, usageTestNow, usageWindow("five_hour", 100, resets, nil))
	aw := sendModel(p, "x", "claude-sonnet-4-5")
	var ab struct {
		Type  string `json:"type"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if aw.Code != http.StatusServiceUnavailable || json.Unmarshal(aw.Body.Bytes(), &ab) != nil || ab.Type != "error" || !strings.Contains(ab.Error.Message, resets.Format(time.RFC3339)) {
		t.Errorf("anthropic: status = %d body = %s", aw.Code, aw.Body.String())
	}
}

// TestProxy_1000RequestsZeroUsageCalls: a real enabled Poller with a counting
// fetcher sees no fetch from 1000 proxied requests over a cold account, a
// stale-cache account, an account in backoff, an exhausted account and one
// whose fetch is blocked in flight.
func TestProxy_1000RequestsZeroUsageCalls(t *testing.T) {
	t.Parallel()

	up, keys := keyRecordingUpstream(t)
	s := newTestStoreForProxy(t)
	f := &countingUsageFetcher{blockID: -1, entered: make(chan struct{}, 1)}
	reg, router, poller := newEnabledRouting(t, s, f)
	p := newProxy(reg, router, s, withAnthropicHTTPClient(testHTTPClient(t, up)))
	t.Cleanup(p.Close)

	// Candidates are walked in id order and the first non-exhausted one wins,
	// so every account before "cold" must be exhausted for a request to
	// peek it: each routed request then reads all five states, and none of
	// them may cause a fetch. Stale reports (polled hours ago) still count
	// until their ResetsAt.
	stale := addAPIKeyAccount(t, reg, providerAnthropic, "stale", "sk-stale")
	exhausted := addAPIKeyAccount(t, reg, providerAnthropic, "exhausted", "sk-exhausted")
	backoff := addAPIKeyAccount(t, reg, providerAnthropic, "backoff", "sk-backoff")
	inflight := addAPIKeyAccount(t, reg, providerAnthropic, "inflight", "sk-inflight")
	addAPIKeyAccount(t, reg, providerAnthropic, "cold", "sk-cold") // the only healthy account
	f.blockID = inflight

	hour := usageTestNow.Add(time.Hour)
	old := usageTestNow.Add(-3 * time.Hour)
	seedUsageReport(t, s, stale, old, usageWindow("five_hour", 100, hour, nil))
	seedUsageReport(t, s, exhausted, usageTestNow, usageWindow("five_hour", 100, hour, nil))
	seedUsageReport(t, s, backoff, old, usageWindow("five_hour", 100, hour, nil))
	seedUsageReport(t, s, inflight, old, usageWindow("five_hour", 100, hour, nil))

	// backoff: a failing Touch (the old report is stale, so it fetches) leaves
	// the last good report in place and the account in failure backoff.
	if res := poller.Touch(context.Background(), backoff); res.LastError == "" {
		t.Fatalf("Touch(backoff) LastError empty, want a recorded failure")
	}
	touchCtx, cancel := context.WithCancel(context.Background())
	touchDone := make(chan struct{})
	go func() { defer close(touchDone); poller.Touch(touchCtx, inflight) }()
	t.Cleanup(func() { cancel(); <-touchDone })
	select {
	case <-f.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("blocked fetch never started")
	}
	baseline := f.calls.Load()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ {
			model := "claude-sonnet-4-5"
			if i%2 == 1 {
				model = "claude-opus-4-1"
			}
			if w := sendModel(p, fmt.Sprintf("sess-%d", i%100), model); w.Code != http.StatusOK {
				t.Errorf("request %d status = %d; body = %s", i, w.Code, w.Body.String())
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(120 * time.Second):
		t.Fatal("requests did not complete promptly with a fetcher blocked in flight")
	}
	// Every request must have landed on the only healthy account, which also
	// proves the exhausted accounts before it were all peeked and skipped.
	if got := keys(); len(got) != 1000 {
		t.Errorf("upstream saw %d requests, want 1000", len(got))
	} else {
		for i, k := range got {
			if k != "sk-cold" {
				t.Fatalf("request %d went to key %q, want sk-cold", i, k)
			}
		}
	}
	if got := f.calls.Load(); got != baseline {
		t.Errorf("usage fetches during 1000 requests = %d, want 0", got-baseline)
	}
}
