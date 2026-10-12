package quota

// Call-budget failure handling (ADR-0028 §3 rule 5, plan AC49–AC52): every
// test drives the REAL AccountFetcher over an httptest server on a fake clock
// and asserts exact request counts per path. No test touches a real provider.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
)

// b3bRefresher is an accounts.OAuthRefresher whose token call is a real HTTP
// POST to the usage server's token path, so token-endpoint requests are
// counted per path. Its errors have the shape of the real refreshers'.
type b3bRefresher struct {
	url    string
	client *http.Client
}

func (r *b3bRefresher) Refresh(ctx context.Context, cred accounts.OAuthCredential) (accounts.OAuthCredential, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.url, strings.NewReader("grant_type=refresh_token"))
	if err != nil {
		return accounts.OAuthCredential{}, err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return accounts.OAuthCredential{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return accounts.OAuthCredential{}, fmt.Errorf("anthropic oauth refresh failed with status %d: %s", resp.StatusCode, body)
	}
	return accounts.OAuthCredential{
		AccessToken:  "b3b-refreshed-access",
		RefreshToken: cred.RefreshToken,
		ExpiresAt:    time.Now().Add(100000 * time.Hour),
	}, nil
}

// b3bRig is an env whose fetcher is the real AccountFetcher pointed at one
// usageServer (usage at pathAnthropic, token endpoint at pathToken).
type b3bRig struct {
	e         *env
	srv       *usageServer
	src       *anthropicSource
	refresher *b3bRefresher
}

func b3bNew(t *testing.T, opts ...Option) *b3bRig {
	t.Helper()
	srv := newUsageServer(t)
	r := &b3bRig{srv: srv, refresher: &b3bRefresher{url: srv.srv.URL + pathToken, client: srv.srv.Client()}}
	r.e = newEnv(t, func(reg *accounts.Registry, clk *fakeClock) ReportFetcher {
		src := newAnthropicSource(srv.srv.Client())
		src.url = srv.srv.URL + pathAnthropic
		src.now = clk.Now
		r.src = src
		return newAccountFetcher(reg, map[string]providerSpec{
			"anthropic": {source: src, refresher: r.refresher},
		}, clk.Now)
	}, opts...)
	// Whole seconds so an HTTP-date Retry-After is exact.
	r.e.clk.Set(r.e.clk.Now().Truncate(time.Second))
	return r
}

// useTimeout swaps the source's client for one with a short real timeout
// (the production requestTimeout is a constant). Call before any fetch.
func (r *b3bRig) useTimeout(d time.Duration) {
	r.src.client = &http.Client{
		Transport: r.srv.srv.Client().Transport,
		Timeout:   d,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func b3bBody(status int, body string, hdr ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		for i := 0; i+1 < len(hdr); i += 2 {
			w.Header().Set(hdr[i], hdr[i+1])
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

// b3bCounts asserts the exact request counts per path.
func b3bCounts(t *testing.T, srv *usageServer, usage, token int) {
	t.Helper()
	if got := srv.PathCalls(pathAnthropic); got != usage {
		t.Errorf("usage requests = %d, want %d", got, usage)
	}
	if got := srv.PathCalls(pathToken); got != token {
		t.Errorf("token requests = %d, want %d", got, token)
	}
}

func b3bTimeNear(t *testing.T, what string, got, want time.Time) {
	t.Helper()
	d := got.Sub(want)
	if d < -time.Millisecond || d > time.Millisecond {
		t.Errorf("%s = %v, want %v (off by %v)", what, got, want, d)
	}
}

type b3bEntry struct {
	name string
	call func(*env, int64) Result
}

var b3bEntries = []b3bEntry{
	{"touch", func(e *env, id int64) Result { return e.touch(id) }},
	{"refresh", func(e *env, id int64) Result { return e.refresh(id) }},
}

// b3bGate checks the backoff window of a failure that happened at the current
// fake time: one second short of wait, both Touch and Refresh send nothing;
// at wait, entry sends exactly one more usage request.
func b3bGate(t *testing.T, r *b3bRig, id int64, wait time.Duration, entry b3bEntry) {
	t.Helper()
	base := r.srv.PathCalls(pathAnthropic)
	r.e.clk.Advance(wait - time.Second)
	r.e.touch(id)
	r.e.refresh(id)
	if got := r.srv.PathCalls(pathAnthropic); got != base {
		t.Fatalf("usage requests at wait-1s = %d, want %d", got, base)
	}
	r.e.clk.Advance(time.Second)
	entry.call(r.e, id)
	if got := r.srv.PathCalls(pathAnthropic); got != base+1 {
		t.Fatalf("usage requests at wait = %d, want %d", got, base+1)
	}
}

// b3bRateLimited runs the common 429 scenario: one failure at the current
// fake time, then the gate at wait with each entry point.
func b3bRateLimited(t *testing.T, header func(clk *fakeClock) http.HandlerFunc, wantWait time.Duration) {
	t.Helper()
	for _, entry := range b3bEntries {
		t.Run(entry.name, func(t *testing.T) {
			t.Parallel()
			r := b3bNew(t)
			r.srv.Set(pathAnthropic, header(r.e.clk))
			id := r.e.addOAuth("tok")
			t0 := r.e.clk.Now()

			res := r.e.touch(id)
			b3bCounts(t, r.srv, 1, 0)
			st := r.e.state(id)
			if st.ConsecutiveFailures != 1 || st.LastError != "rate limited" || res.LastError != "rate limited" {
				t.Errorf("state = %+v, result error = %q, want 1 failure, rate limited", st, res.LastError)
			}
			b3bTimeNear(t, "NextAllowedAt", st.NextAllowedAt, t0.Add(wantWait))
			if res.RefreshBlocked != BlockBackoff {
				t.Errorf("RefreshBlocked = %q, want %q", res.RefreshBlocked, BlockBackoff)
			}
			b3bGate(t, r, id, wantWait, entry)
		})
	}
}

func Test_budget_429_retry_after_seconds(t *testing.T) {
	t.Parallel()
	b3bRateLimited(t, func(*fakeClock) http.HandlerFunc {
		return respondStatus(http.StatusTooManyRequests, "Retry-After", "600")
	}, 600*time.Second)
}

func Test_budget_429_retry_after_http_date(t *testing.T) {
	t.Parallel()
	b3bRateLimited(t, func(clk *fakeClock) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			// Relative to the fake clock, which does not move during the call.
			w.Header().Set("Retry-After", clk.Now().Add(600*time.Second).UTC().Format(http.TimeFormat))
			w.WriteHeader(http.StatusTooManyRequests)
		}
	}, 600*time.Second)
}

func Test_budget_429_absent_header_min_5m(t *testing.T) {
	t.Parallel()
	b3bRateLimited(t, func(*fakeClock) http.HandlerFunc {
		return respondStatus(http.StatusTooManyRequests)
	}, 5*time.Minute)
}

func Test_budget_429_retry_after_capped_6h(t *testing.T) {
	t.Parallel()
	b3bRateLimited(t, func(*fakeClock) http.HandlerFunc {
		return respondStatus(http.StatusTooManyRequests, "Retry-After", "999999")
	}, 6*time.Hour)
}

func Test_budget_429_jitter_never_shortens_retry_after(t *testing.T) {
	t.Parallel()
	const runs = 200
	// 600 s ≥ the first-failure backoff term (1 min), so the wait must be
	// Retry-After itself; 30 s is under the 5 min floor, so it must be ≥ 5 min.
	// The 20% jitter bound applies to the backoff term, here 1 min = 12 s.
	for _, tc := range []struct {
		name       string
		retryAfter string
		floor      time.Duration
	}{
		{"retry_after_600s", "600", 600 * time.Second},
		{"retry_after_30s_under_floor", "30", 5 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var jit atomic.Int64 // jitter in millionths, set before each run
			r := b3bNew(t, WithJitter(func() float64 { return float64(jit.Load()) / 1e6 }))
			r.srv.Set(pathAnthropic, respondStatus(http.StatusTooManyRequests, "Retry-After", tc.retryAfter))
			t0 := r.e.clk.Now()
			upper := tc.floor + time.Duration(float64(backoffBase)*jitterFrac)
			for i := range runs {
				jit.Store(int64(i) * 1e6 / runs) // 0 … 0.995, covers [0,1)
				id := r.e.addOAuth("tok-" + strconv.Itoa(i))
				r.e.touch(id)
				wait := r.e.state(id).NextAllowedAt.Sub(t0)
				if wait < tc.floor || wait > upper {
					t.Fatalf("run %d (jitter %.3f): wait = %v, want within [%v, %v]", i, float64(i)/runs, wait, tc.floor, upper)
				}
			}
			b3bCounts(t, r.srv, runs, 0)
		})
	}
}

// b3bFailStep makes one failing Refresh attempt (the clock is already at the
// attempt time), then proves the account is silent for the whole wait: one
// second short of NextAllowedAt both Touch and Refresh send nothing. It leaves
// the clock one second before NextAllowedAt and returns the wait it observed.
func b3bFailStep(t *testing.T, r *b3bRig, id int64, step int) time.Duration {
	t.Helper()
	t0 := r.e.clk.Now()
	r.e.refresh(id)
	b3bCounts(t, r.srv, step+1, 0)
	st := r.e.state(id)
	if st.ConsecutiveFailures != step+1 {
		t.Fatalf("step %d: ConsecutiveFailures = %d, want %d", step, st.ConsecutiveFailures, step+1)
	}
	wait := st.NextAllowedAt.Sub(t0)
	r.e.clk.Advance(wait - time.Second)
	r.e.touch(id)
	r.e.refresh(id)
	b3bCounts(t, r.srv, step+1, 0)
	return wait
}

func Test_budget_consecutive_failures_exponential_capped(t *testing.T) {
	t.Parallel()
	// Waits after failures 1..8. The rolling-hour forced budget (6/h) cannot
	// interfere: the attempts land at roughly minutes 0,1,3,7,15,31,61,91 and
	// never six within one hour, so the backoff itself is what gates them.
	waits := []time.Duration{1, 2, 4, 8, 16, 30, 30, 30}

	t.Run("no_jitter", func(t *testing.T) {
		t.Parallel()
		r := b3bNew(t)
		r.srv.Set(pathAnthropic, respondStatus(http.StatusServiceUnavailable))
		id := r.e.addOAuth("tok")
		for i, w := range waits {
			if got := b3bFailStep(t, r, id, i); got != w*time.Minute {
				t.Fatalf("step %d: wait = %v, want %v", i, got, w*time.Minute)
			}
			r.e.clk.Advance(time.Second) // reach NextAllowedAt exactly
		}
	})

	t.Run("jitter_within_20_percent", func(t *testing.T) {
		t.Parallel()
		r := b3bNew(t, WithJitter(func() float64 { return 0.999 }))
		r.srv.Set(pathAnthropic, respondStatus(http.StatusServiceUnavailable))
		id := r.e.addOAuth("tok")
		for i, w := range waits {
			base := w * time.Minute
			got := b3bFailStep(t, r, id, i)
			if got <= base || got > base+base/5 {
				t.Fatalf("step %d: wait = %v, want within (%v, %v]", i, got, base, base+base/5)
			}
			r.e.clk.Advance(time.Second)
		}
	})

	t.Run("success_resets_the_ladder", func(t *testing.T) {
		t.Parallel()
		r := b3bNew(t)
		r.srv.Set(pathAnthropic, respondStatus(http.StatusServiceUnavailable))
		id := r.e.addOAuth("tok")
		for i := range 3 {
			b3bFailStep(t, r, id, i)
			r.e.clk.Advance(time.Second)
		}
		r.srv.Set(pathAnthropic, respondUsageOK)
		r.e.refresh(id)
		b3bCounts(t, r.srv, 4, 0)
		if st := r.e.state(id); st.ConsecutiveFailures != 0 || !st.NextAllowedAt.IsZero() || st.LastError != "" {
			t.Fatalf("after success state = %+v, want failures reset", st)
		}

		r.srv.Set(pathAnthropic, respondStatus(http.StatusServiceUnavailable))
		r.e.clk.Advance(refreshFloor) // past the 30 s floor
		t0 := r.e.clk.Now()
		r.e.refresh(id)
		b3bCounts(t, r.srv, 5, 0)
		st := r.e.state(id)
		if st.ConsecutiveFailures != 1 {
			t.Fatalf("ConsecutiveFailures = %d, want 1", st.ConsecutiveFailures)
		}
		b3bTimeNear(t, "NextAllowedAt", st.NextAllowedAt, t0.Add(time.Minute))
	})
}

func Test_budget_timeout_network_malformed_html_empty_backoff(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		handler func(release <-chan struct{}) http.HandlerFunc
		timeout time.Duration
		want    string
	}{
		{"timeout", func(release <-chan struct{}) http.HandlerFunc {
			return func(_ http.ResponseWriter, r *http.Request) {
				select {
				case <-release:
				case <-r.Context().Done():
				}
			}
		}, 150 * time.Millisecond, "timeout"},
		{"network_error", func(<-chan struct{}) http.HandlerFunc {
			return func(w http.ResponseWriter, _ *http.Request) {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err == nil {
					conn.Close()
				}
			}
		}, 0, "network error"},
		{"html_200", func(<-chan struct{}) http.HandlerFunc {
			return b3bBody(http.StatusOK, "<html><body>Please sign in</body></html>", "Content-Type", "text/html")
		}, 0, "malformed response"},
		{"empty_object_200", func(<-chan struct{}) http.HandlerFunc {
			return b3bBody(http.StatusOK, `{}`)
		}, 0, "malformed response"},
		{"truncated_json_200", func(<-chan struct{}) http.HandlerFunc {
			return b3bBody(http.StatusOK, `{"five_hour":{"utilization":10,"resets_at":"2026-10-`)
		}, 0, "malformed response"},
		{"http_503", func(<-chan struct{}) http.HandlerFunc {
			return respondStatus(http.StatusServiceUnavailable)
		}, 0, "http 503"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := b3bNew(t)
			release := make(chan struct{})
			t.Cleanup(func() { close(release) }) // runs before the server closes
			if tc.timeout > 0 {
				r.useTimeout(tc.timeout)
			}
			r.srv.Set(pathAnthropic, tc.handler(release))
			id := r.e.addOAuth("tok")
			t0 := r.e.clk.Now()

			res := r.e.touch(id)
			b3bCounts(t, r.srv, 1, 0)
			st := r.e.state(id)
			if st.ConsecutiveFailures != 1 || st.LastError != tc.want || res.LastError != tc.want {
				t.Errorf("state = %+v, result error = %q, want 1 failure with %q", st, res.LastError, tc.want)
			}
			b3bTimeNear(t, "NextAllowedAt", st.NextAllowedAt, t0.Add(time.Minute))

			// A changed endpoint must not be hammered: nothing more inside
			// the window, via either entry point.
			r.e.clk.Advance(time.Minute - time.Second)
			r.e.touch(id)
			r.e.refresh(id)
			b3bCounts(t, r.srv, 1, 0)
		})
	}
}

// ---- Group 2: expired token and rejected credential (AC50/AC51) ----

var b3bFar = time.Now().Add(100000 * time.Hour)

// b3bCred is the JSON credential string accepted by Registry.ReplaceCredential.
func b3bCred(token string, expires time.Time) string {
	data, err := json.Marshal(accounts.OAuthCredential{AccessToken: token, RefreshToken: "refresh-" + token, ExpiresAt: expires})
	if err != nil {
		panic(err)
	}
	return string(data)
}

// spread runs Touch and Refresh 100 times over 7 fake days.
func (r *b3bRig) spread(id int64) {
	for range 100 {
		r.e.clk.Advance(7 * 24 * time.Hour / 100)
		r.e.touch(id)
		r.e.refresh(id)
	}
}

func (r *b3bRig) replace(t *testing.T, id int64, token string, expires time.Time) {
	t.Helper()
	if _, err := r.e.reg.ReplaceCredential(id, b3bCred(token, expires), ""); err != nil {
		t.Fatalf("ReplaceCredential() error = %v", err)
	}
}

func Test_budget_refresh_call_for_expired_token_obeys_backoff(t *testing.T) {
	t.Parallel()
	r := b3bNew(t)
	r.srv.Set(pathToken, respondStatus(http.StatusServiceUnavailable))
	id := r.e.addOAuthExpiring("anthropic", "tok", time.Now().Add(-time.Hour))

	res := r.e.touch(id)
	b3bCounts(t, r.srv, 0, 1)
	if st := r.e.state(id); st.ConsecutiveFailures != 1 || st.LastError != errRefresh || res.LastError != errRefresh {
		t.Fatalf("state = %+v, result error = %q, want 1 failure with %q", st, res.LastError, errRefresh)
	}

	// The token endpoint is gated by the same backoff as the usage endpoint.
	r.e.clk.Advance(time.Minute - time.Second)
	r.e.touch(id)
	r.e.refresh(id)
	b3bCounts(t, r.srv, 0, 1)

	r.e.clk.Advance(time.Second)
	r.e.refresh(id)
	b3bCounts(t, r.srv, 0, 2)

	// Second failure: the wait doubles, and again both paths stay silent.
	r.e.clk.Advance(2*time.Minute - time.Second)
	r.e.touch(id)
	r.e.refresh(id)
	b3bCounts(t, r.srv, 0, 2)
	r.e.clk.Advance(time.Second)
	r.e.refresh(id)
	b3bCounts(t, r.srv, 0, 3)
}

// b3bRejected is the shared 401/403 scenario.
func b3bRejected(t *testing.T, status int) {
	t.Helper()
	r := b3bNew(t)
	r.srv.Set(pathAnthropic, respondStatus(status))
	id := r.e.addOAuth("tok1")

	res := r.e.touch(id)
	b3bCounts(t, r.srv, 1, 0)
	if res.Report == nil || res.Report.Status != StatusUnavailable || res.Report.Problem == nil || res.Report.Problem.Kind != ProblemRejected {
		t.Fatalf("report = %+v, want unavailable/rejected", res.Report)
	}
	if res.RefreshBlocked != BlockRejected || res.NextRefreshAt != nil {
		t.Errorf("blocked = %q, next = %v, want rejected and no next time", res.RefreshBlocked, res.NextRefreshAt)
	}

	r.spread(id)
	b3bCounts(t, r.srv, 1, 0)

	// Re-saving the SAME access token (a new refresh token only) changes
	// nothing: the rejected fingerprint is that of the access token.
	if _, err := r.e.reg.ReplaceCredential(id, `{"access_token":"tok1","refresh_token":"other","expires_at":"`+b3bFar.Format(time.RFC3339)+`"}`, ""); err != nil {
		t.Fatalf("ReplaceCredential() error = %v", err)
	}
	r.spread(id)
	b3bCounts(t, r.srv, 1, 0)

	// A NEW token earns exactly one attempt once the TTL has passed.
	r.replace(t, id, "tok2", b3bFar)
	r.e.clk.Advance(touchTTLFloor)
	r.e.touch(id)
	b3bCounts(t, r.srv, 2, 0)
	r.spread(id)
	b3bCounts(t, r.srv, 2, 0)
	if r.srv.TokenCalls("tok1") != 1 || r.srv.TokenCalls("tok2") != 1 {
		t.Errorf("requests per token = %d/%d, want 1/1", r.srv.TokenCalls("tok1"), r.srv.TokenCalls("tok2"))
	}
}

func Test_budget_401_no_retry_until_credential_changes(t *testing.T) {
	t.Parallel()
	b3bRejected(t, http.StatusUnauthorized)
}

func Test_budget_403_same_as_401(t *testing.T) {
	t.Parallel()
	b3bRejected(t, http.StatusForbidden)
}

func Test_budget_invalid_grant_terminal_until_credential_changes(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			t.Parallel()
			r := b3bNew(t)
			r.srv.Set(pathToken, b3bBody(status, `{"error":"invalid_grant"}`))
			id := r.e.addOAuthExpiring("anthropic", "tok1", time.Now().Add(-time.Hour))

			res := r.e.touch(id)
			b3bCounts(t, r.srv, 0, 1)
			if res.Report == nil || res.Report.Status != StatusUnavailable || res.Report.Problem == nil || res.Report.Problem.Kind != ProblemExpired {
				t.Fatalf("report = %+v, want unavailable/expired", res.Report)
			}
			if res.RefreshBlocked != BlockRejected {
				t.Errorf("RefreshBlocked = %q, want %q", res.RefreshBlocked, BlockRejected)
			}
			r.spread(id)
			b3bCounts(t, r.srv, 0, 1)

			// A different (still expired) credential: exactly one more token
			// call, then terminal again.
			r.replace(t, id, "tok2", time.Now().Add(-time.Hour))
			r.e.clk.Advance(touchTTLFloor)
			r.e.touch(id)
			b3bCounts(t, r.srv, 0, 2)
			r.spread(id)
			b3bCounts(t, r.srv, 0, 2)

			// A valid credential goes straight to the usage endpoint.
			r.replace(t, id, "tok3", b3bFar)
			r.e.clk.Advance(touchTTLFloor)
			r.e.touch(id)
			b3bCounts(t, r.srv, 1, 2)
		})
	}
}

func Test_budget_proxy_refreshed_token_changes_fingerprint_allows_one_retry(t *testing.T) {
	t.Parallel()
	r := b3bNew(t)
	r.srv.Set(pathAnthropic, respondStatus(http.StatusUnauthorized))
	id := r.e.addOAuth("tok1")
	r.e.touch(id)
	b3bCounts(t, r.srv, 1, 0)

	store := func(token string, expires time.Time) {
		t.Helper()
		data, err := json.Marshal(accounts.OAuthCredential{AccessToken: token, RefreshToken: "refresh-tok1", ExpiresAt: expires})
		if err != nil {
			t.Fatal(err)
		}
		// What the proxy's EnsureFresh does after a token refresh.
		if _, err := r.e.st.UpdateAccountCredential(id, string(data)); err != nil {
			t.Fatalf("UpdateAccountCredential() error = %v", err)
		}
	}

	// Same access token, only the expiry moved: still the rejected credential.
	store("tok1", b3bFar.Add(time.Hour))
	r.spread(id)
	b3bCounts(t, r.srv, 1, 0)

	// The proxy rotated the access token: one retry, then rejected again.
	store("tok-proxy", b3bFar)
	r.e.clk.Advance(touchTTLFloor)
	r.e.touch(id)
	b3bCounts(t, r.srv, 2, 0)
	r.spread(id)
	b3bCounts(t, r.srv, 2, 0)
	if got := r.srv.TokenCalls("tok-proxy"); got != 1 {
		t.Errorf("requests with the proxy-refreshed token = %d, want 1", got)
	}
}

// b3bLogBuf is a goroutine-safe log sink (the poller logs from its fetch goroutine).
type b3bLogBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *b3bLogBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *b3bLogBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// Not parallel: it swaps the process-wide logger.
func Test_budget_fingerprint_not_in_logs_or_wire(t *testing.T) {
	logs := &b3bLogBuf{}
	prev := log.Writer()
	log.SetOutput(logs)
	t.Cleanup(func() { log.SetOutput(prev) })

	r := b3bNew(t)
	const tokRejected, tokRateLimited, tokExpired = "b3b-secret-AAAA", "b3b-secret-BBBB", "b3b-secret-CCCC"
	r.srv.Set(pathAnthropic, func(w http.ResponseWriter, req *http.Request) {
		switch strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ") {
		case tokRejected:
			w.WriteHeader(http.StatusUnauthorized)
		case tokRateLimited:
			w.Header().Set("Retry-After", "600")
			w.WriteHeader(http.StatusTooManyRequests)
		}
	})
	r.srv.Set(pathToken, b3bBody(http.StatusBadRequest, `{"error":"invalid_grant"}`))
	ids := []int64{
		r.e.addOAuth(tokRejected),
		r.e.addOAuth(tokRateLimited),
		r.e.addOAuthExpiring("anthropic", tokExpired, time.Now().Add(-time.Hour)),
	}

	var wire strings.Builder
	dump := func(v any) {
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
		wire.Write(data)
	}
	for _, id := range ids {
		res := r.e.touch(id)
		dump(res)
		if res.Report != nil {
			dump(*res.Report)
		}
		dump(r.e.refresh(id))
		dump(r.e.p.Peek(id))
	}
	r.e.p.Close() // waits for the fetch goroutines' log lines

	var secrets []string
	for _, id := range ids {
		acc, err := r.e.reg.Get(id)
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		secrets = append(secrets, acc.OAuth.AccessToken, acc.OAuth.RefreshToken, fingerprint(acc))
	}
	out := logs.String()
	if !strings.Contains(out, "quota: account") {
		t.Fatalf("no poller log lines captured; the check would be vacuous: %q", out)
	}
	if wire.Len() == 0 {
		t.Fatal("no wire output captured")
	}
	for i, s := range secrets {
		if len(s) < 8 {
			t.Fatalf("secret %d is unexpectedly short", i)
		}
		if strings.Contains(out, s) {
			t.Errorf("log output contains secret #%d (kind %d of access/refresh/fingerprint)", i/3, i%3)
		}
		if strings.Contains(wire.String(), s) {
			t.Errorf("serialized Result/Report contains secret #%d (kind %d of access/refresh/fingerprint)", i/3, i%3)
		}
	}
}

// ---- Group 3: failure state survives a restart (AC52) ----

func Test_budget_backoff_persists_across_restart(t *testing.T) {
	t.Parallel()
	for _, entry := range b3bEntries {
		t.Run(entry.name, func(t *testing.T) {
			t.Parallel()
			r := b3bNew(t)
			r.srv.Set(pathAnthropic, respondStatus(http.StatusTooManyRequests, "Retry-After", "600"))
			id := r.e.addOAuth("tok")
			r.e.touch(id)
			b3bCounts(t, r.srv, 1, 0)

			r.e.restart()
			// Both entry points are silent until 600 s, then exactly one goes out.
			b3bGate(t, r, id, 600*time.Second, entry)
		})
	}
}

func Test_budget_rejected_state_persists_across_restart(t *testing.T) {
	t.Parallel()
	r := b3bNew(t)
	r.srv.Set(pathAnthropic, respondStatus(http.StatusUnauthorized))
	id := r.e.addOAuth("tok1")
	r.e.touch(id)
	b3bCounts(t, r.srv, 1, 0)

	r.e.restart()
	if res := r.e.p.Peek(id); res.RefreshBlocked != BlockRejected {
		t.Errorf("Peek after restart: RefreshBlocked = %q, want %q", res.RefreshBlocked, BlockRejected)
	}
	r.spread(id)
	b3bCounts(t, r.srv, 1, 0)

	r.replace(t, id, "tok2", b3bFar)
	r.e.clk.Advance(touchTTLFloor)
	r.e.touch(id)
	b3bCounts(t, r.srv, 2, 0)
}

func Test_budget_backoff_not_reset_by_restart_with_failures_count(t *testing.T) {
	t.Parallel()
	r := b3bNew(t)
	r.srv.Set(pathAnthropic, respondStatus(http.StatusServiceUnavailable))
	id := r.e.addOAuth("tok")

	// A restart between every failure: the ladder keeps climbing 1, 2, 4 min
	// instead of starting over at 1.
	for i, want := range []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute} {
		if got := b3bFailStep(t, r, id, i); got != want {
			t.Fatalf("failure %d: wait = %v, want %v", i+1, got, want)
		}
		r.e.restart()
		r.e.touch(id)
		r.e.refresh(id)
		b3bCounts(t, r.srv, i+1, 0) // still inside the window after the restart
		if st := r.e.state(id); st.ConsecutiveFailures != i+1 {
			t.Fatalf("after restart ConsecutiveFailures = %d, want %d", st.ConsecutiveFailures, i+1)
		}
		r.e.clk.Advance(time.Second)
	}
}

func Test_budget_failed_attempt_restarts_ttl_after_restart(t *testing.T) {
	t.Parallel()
	r := b3bNew(t)
	r.srv.Set(pathAnthropic, respondStatus(http.StatusServiceUnavailable))
	id := r.e.addOAuth("tok")
	r.e.touch(id) // the failed attempt starts the 5 min TTL
	b3bCounts(t, r.srv, 1, 0)

	r.e.restart()
	// The 1 min backoff is over at +2 min, but a Touch still waits out the
	// TTL counted from the failed attempt.
	r.e.clk.Advance(2 * time.Minute)
	r.e.touch(id)
	r.e.clk.Advance(touchTTLFloor - 2*time.Minute - time.Second)
	r.e.touch(id)
	b3bCounts(t, r.srv, 1, 0)

	r.e.clk.Advance(time.Second)
	r.e.touch(id)
	b3bCounts(t, r.srv, 2, 0)
}
