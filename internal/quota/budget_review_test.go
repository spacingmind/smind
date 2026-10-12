package quota

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/store"
)

// Regression tests for the independent review of the poller (ADR-0028 PR-A2).

// A token the fetch itself refreshed, then refused with 401, is the rejected
// credential. Recording the pre-fetch token instead would make every later
// Touch look like a changed credential and re-fetch.
func Test_budget_rejected_fingerprint_is_the_token_the_fetch_sent(t *testing.T) {
	t.Parallel()
	r := b3bNew(t)
	r.srv.Set(pathAnthropic, respondStatus(http.StatusUnauthorized))
	id := r.e.addOAuthExpiring("anthropic", "tok-old", time.Now().Add(-time.Hour))

	res := r.e.touch(id)
	b3bCounts(t, r.srv, 1, 1) // one refresh, one usage request
	if r.srv.TokenCalls("b3b-refreshed-access") != 1 {
		t.Fatalf("usage request carried the refreshed token %d times, want 1", r.srv.TokenCalls("b3b-refreshed-access"))
	}
	if res.RefreshBlocked != BlockRejected {
		t.Errorf("RefreshBlocked = %q, want %q", res.RefreshBlocked, BlockRejected)
	}
	r.spread(id)
	b3bCounts(t, r.srv, 1, 1)
}

// A credential replaced while the request is in flight was not the one the
// provider refused: it must stay eligible for its own attempt.
func Test_budget_credential_replaced_mid_flight_stays_eligible(t *testing.T) {
	t.Parallel()
	r := b3bNew(t)
	inside := make(chan struct{})
	release := make(chan struct{})
	first := make(chan struct{}, 1)
	first <- struct{}{}
	r.srv.Set(pathAnthropic, func(w http.ResponseWriter, req *http.Request) {
		select {
		case <-first: // only the first request blocks and is refused
			close(inside)
			<-release
			w.WriteHeader(http.StatusUnauthorized)
		default:
			respondUsageOK(w, req)
		}
	})
	id := r.e.addOAuth("tok1")

	done := make(chan Result, 1)
	go func() { done <- r.e.touch(id) }()
	<-inside
	r.replace(t, id, "tok2", b3bFar) // the proxy or the user swaps the credential
	close(release)
	res := <-done
	b3bCounts(t, r.srv, 1, 0)
	if res.Report == nil || res.Report.Problem == nil || res.Report.Problem.Kind != ProblemRejected {
		t.Fatalf("report = %+v, want unavailable/rejected for the old token", res.Report)
	}
	if res.RefreshBlocked == BlockRejected {
		t.Fatalf("RefreshBlocked = rejected: the new credential was never tried")
	}

	r.e.clk.Advance(touchTTLFloor)
	r.e.touch(id)
	b3bCounts(t, r.srv, 2, 0)
	if r.srv.TokenCalls("tok2") != 1 {
		t.Errorf("new token used %d times, want 1", r.srv.TokenCalls("tok2"))
	}
}

// After a clock jump backwards, forced attempts stamped "in the future" must
// still count: a spent budget does not reopen.
func Test_budget_future_forced_attempts_clamped_not_ignored(t *testing.T) {
	t.Parallel()
	r := b3bNew(t)
	id := r.e.addOAuth("tok")
	future := r.e.clk.Now().Add(2 * time.Hour).UnixMilli()
	if err := r.e.st.SaveQuotaPollState(store.QuotaPollState{
		AccountID: id, ForcedAt: []int64{future, future, future, future, future, future},
	}); err != nil {
		t.Fatalf("SaveQuotaPollState() error = %v", err)
	}
	res := r.e.refresh(id)
	b3bCounts(t, r.srv, 0, 0)
	if res.RefreshBlocked != BlockBudget {
		t.Errorf("RefreshBlocked = %q, want %q", res.RefreshBlocked, BlockBudget)
	}
	if res.NextRefreshAt == nil || res.NextRefreshAt.After(r.e.clk.Now().Add(time.Hour+time.Second)) {
		t.Errorf("NextRefreshAt = %v, want at most an hour away", res.NextRefreshAt)
	}
}

// A fetcher that panics must not wedge the account or the in-flight cap.
func Test_budget_fetcher_panic_is_a_transient_failure(t *testing.T) {
	t.Parallel()
	var f *fakeReportFetcher
	f = &fakeReportFetcher{fn: func(context.Context, accounts.Account) (Report, error) {
		if f.Calls() == 1 {
			panic("boom")
		}
		return availableTestReport(), nil
	}}
	e := fakeEnv(t, f)
	id := e.addOAuth("tok")

	res := e.touch(id)
	if res.LastError == "" || res.InFlight {
		t.Fatalf("result = %+v, want a failure and no flight left open", res)
	}
	if st := e.state(id); st.ConsecutiveFailures != 1 || st.NextAllowedAt.IsZero() {
		t.Errorf("state = %+v, want 1 failure with a backoff", st)
	}
	// The in-flight slot was released: another account fetches fine, and the
	// panicking one recovers after its backoff.
	other := e.addOAuth("tok-other")
	if res := e.touch(other); res.Report == nil || res.Report.Status != StatusAvailable {
		t.Errorf("other account result = %+v, want available", res)
	}
	e.clk.Advance(time.Hour)
	if res := e.touch(id); res.Report == nil || res.Report.Status != StatusAvailable {
		t.Errorf("after backoff result = %+v, want available", res)
	}
	if f.Calls() != 3 {
		t.Errorf("fetches = %d, want 3", f.Calls())
	}
}

// A huge Retry-After parks the account for the cap, not for the minimum.
func Test_budget_429_retry_after_absurd_is_capped_not_minimum(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"1e12", "Inf", "99999999999999"} {
		b3bRateLimited(t, func(*fakeClock) http.HandlerFunc {
			return respondStatus(http.StatusTooManyRequests, "Retry-After", v)
		}, retryAfterCap)
	}
}

// An explainable answer (401) is not a transient failure: it ends the
// failure streak, so the next transient failure after a re-login starts at 1 min.
func Test_budget_unavailable_resets_failure_streak(t *testing.T) {
	t.Parallel()
	r := b3bNew(t)
	r.srv.Set(pathAnthropic, respondStatus(http.StatusServiceUnavailable))
	id := r.e.addOAuth("tok1")
	r.e.touch(id)
	r.e.clk.Advance(touchTTLFloor)
	r.e.touch(id)
	if st := r.e.state(id); st.ConsecutiveFailures != 2 {
		t.Fatalf("failures = %d, want 2", st.ConsecutiveFailures)
	}
	r.srv.Set(pathAnthropic, respondStatus(http.StatusUnauthorized))
	r.e.clk.Advance(time.Hour)
	r.e.touch(id)
	if st := r.e.state(id); st.ConsecutiveFailures != 0 || !st.NextAllowedAt.IsZero() || st.RejectedFP == "" {
		t.Errorf("state = %+v, want no failures, no backoff, rejected", st)
	}
}
