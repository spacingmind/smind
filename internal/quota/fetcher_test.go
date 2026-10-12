package quota

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/store"
)

const fetcherUsageBody = `{"five_hour":{"utilization":10,"resets_at":"2026-10-12T05:00:00Z"},"seven_day":{"utilization":20,"resets_at":"2026-10-15T05:00:00Z"}}`

type fetcherRefresher struct {
	calls atomic.Int32
	cred  accounts.OAuthCredential
	err   error
}

func (f *fetcherRefresher) Refresh(ctx context.Context, _ accounts.OAuthCredential) (accounts.OAuthCredential, error) {
	f.calls.Add(1)
	if f.err != nil {
		return accounts.OAuthCredential{}, f.err
	}
	return f.cred, nil
}

// fetcherHarness is a registry over a temp store, a counting usage server
// and a fetcher whose anthropic source points at it.
type fetcherHarness struct {
	reg       *accounts.Registry
	fetcher   *AccountFetcher
	refresher *fetcherRefresher
	requests  atomic.Int32
	lastAuth  atomic.Value // string
}

func newFetcherHarness(t *testing.T) *fetcherHarness {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { s.Close() })

	h := &fetcherHarness{reg: accounts.New(s), refresher: &fetcherRefresher{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.requests.Add(1)
		h.lastAuth.Store(r.Header.Get("Authorization"))
		w.Write([]byte(fetcherUsageBody))
	}))
	t.Cleanup(server.Close)

	src := newAnthropicSource(server.Client())
	src.url = server.URL
	osrc := newOpenAISource(server.Client())
	osrc.url = server.URL
	h.fetcher = newAccountFetcher(h.reg, map[string]providerSpec{
		"anthropic": {source: src, refresher: h.refresher},
		"openai":    {source: osrc, refresher: h.refresher},
	}, nil)
	return h
}

func (h *fetcherHarness) oauthAccount(t *testing.T, provider, label string, expiresAt time.Time) accounts.Account {
	t.Helper()
	created, err := h.reg.AddOAuth(provider, label, accounts.OAuthCredential{
		AccessToken: "access-old", RefreshToken: "refresh-old", ExpiresAt: expiresAt,
	})
	if err != nil {
		t.Fatalf("AddOAuth() error = %v", err)
	}
	acc, err := h.reg.Get(created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	return acc
}

func TestFetcherAPIKeyNoCall(t *testing.T) {
	t.Parallel()
	h := newFetcherHarness(t)

	for _, provider := range []string{"anthropic", "openai"} {
		created, err := h.reg.AddAPIKeyWithBaseURL(provider, "key", "sk-test", "https://llm.example.com")
		if err != nil {
			t.Fatalf("AddAPIKeyWithBaseURL(%s) error = %v", provider, err)
		}
		acc, _ := h.reg.Get(created.ID)
		got, err := h.fetcher.Fetch(context.Background(), acc)
		if err != nil {
			t.Fatalf("%s: Fetch() error = %v", provider, err)
		}
		if got.Status != StatusUnavailable || got.Problem == nil || got.Problem.Kind != ProblemNoQuota {
			t.Errorf("%s: report = %+v, want unavailable/no_quota", provider, got)
		}
	}
	if n := h.requests.Load(); n != 0 {
		t.Errorf("usage server saw %d requests, want 0", n)
	}
}

func TestFetcherUnsupportedProvider(t *testing.T) {
	t.Parallel()
	h := newFetcherHarness(t)
	acc := h.oauthAccount(t, "kimi", "k", time.Now().Add(time.Hour))

	got, err := h.fetcher.Fetch(context.Background(), acc)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if got.Status != StatusUnavailable || got.Problem.Kind != ProblemNoQuota || got.Problem.Detail != "provider not supported" {
		t.Errorf("report = %+v problem = %+v, want unavailable/no_quota provider not supported", got, got.Problem)
	}
	if n := h.requests.Load(); n != 0 {
		t.Errorf("usage server saw %d requests, want 0", n)
	}
}

func TestFetcherValidTokenNoRefresh(t *testing.T) {
	t.Parallel()
	h := newFetcherHarness(t)
	acc := h.oauthAccount(t, "anthropic", "a", time.Now().Add(time.Hour))

	got, err := h.fetcher.Fetch(context.Background(), acc)
	if err != nil || got.Status != StatusAvailable {
		t.Fatalf("Fetch() = %+v, %v, want available", got, err)
	}
	if n := h.refresher.calls.Load(); n != 0 {
		t.Errorf("refresher called %d times, want 0", n)
	}
	if auth, _ := h.lastAuth.Load().(string); auth != "Bearer access-old" {
		t.Errorf("Authorization = %q, want the stored token", auth)
	}
}

func TestFetcherExpiredTokenRefreshesOnce(t *testing.T) {
	t.Parallel()
	h := newFetcherHarness(t)
	h.refresher.cred = accounts.OAuthCredential{AccessToken: "access-new", RefreshToken: "refresh-new", ExpiresAt: time.Now().Add(time.Hour)}
	acc := h.oauthAccount(t, "anthropic", "a", time.Now().Add(-time.Minute))

	got, err := h.fetcher.Fetch(context.Background(), acc)
	if err != nil || got.Status != StatusAvailable {
		t.Fatalf("Fetch() = %+v, %v, want available", got, err)
	}
	if n := h.refresher.calls.Load(); n != 1 {
		t.Errorf("refresher called %d times, want 1", n)
	}
	if auth, _ := h.lastAuth.Load().(string); auth != "Bearer access-new" {
		t.Errorf("Authorization = %q, want the refreshed token", auth)
	}
	persisted, err := h.reg.Get(acc.ID)
	if err != nil || persisted.OAuth.AccessToken != "access-new" {
		t.Errorf("persisted credential = %+v, %v, want access-new", persisted.OAuth, err)
	}
}

func TestFetcherRefreshFailureExpired(t *testing.T) {
	t.Parallel()
	h := newFetcherHarness(t)
	h.refresher.err = &oauth2.RetrieveError{ErrorCode: "invalid_grant", Body: []byte("refresh-secret-xyz")}
	expiredAt := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	acc := h.oauthAccount(t, "openai", "a", expiredAt)

	got, err := h.fetcher.Fetch(context.Background(), acc)
	if err != nil {
		t.Fatalf("Fetch() error = %v, want nil (explainable outcome)", err)
	}
	if got.Status != StatusUnavailable || got.Problem == nil || got.Problem.Kind != ProblemExpired {
		t.Fatalf("report = %+v, want unavailable/expired", got)
	}
	if got.Problem.ExpiresAt == nil || !got.Problem.ExpiresAt.Equal(expiredAt) {
		t.Errorf("ExpiresAt = %v, want %v", got.Problem.ExpiresAt, expiredAt)
	}
	if n := h.requests.Load(); n != 0 {
		t.Errorf("usage server saw %d requests, want 0", n)
	}
	enc, _ := EncodeReport(got)
	if strings.Contains(enc, "refresh-secret-xyz") || strings.Contains(enc, "invalid_grant") {
		t.Errorf("stored report leaks the refresher error: %s", enc)
	}
}

func TestFetcherRefreshFailureTransientIsError(t *testing.T) {
	t.Parallel()
	for name, rerr := range map[string]error{
		"network":             errors.New("anthropic oauth refresh request: dial tcp: refused"),
		"5xx":                 errors.New("anthropic oauth refresh failed with status 503: upstream secret-body"),
		"429":                 errors.New("openai oauth refresh failed with status 429: slow down"),
		"timeout":             context.DeadlineExceeded,
		"oauth2-server_error": &oauth2.RetrieveError{ErrorCode: "server_error", Body: []byte("x")},
	} {
		t.Run("fetcher_refresh_failure_transient_is_error/"+name, func(t *testing.T) {
			t.Parallel()
			h := newFetcherHarness(t)
			h.refresher.err = rerr
			acc := h.oauthAccount(t, "anthropic", "a", time.Now().Add(-time.Hour))

			got, err := h.fetcher.Fetch(context.Background(), acc)
			var fe *FetchError
			if !errors.As(err, &fe) || fe.Error() != "token refresh failed" {
				t.Fatalf("err = %v, want *FetchError token refresh failed", err)
			}
			if got.Status != StatusError || got.Error != "token refresh failed" {
				t.Errorf("report = %+v, want error/token refresh failed", got)
			}
			if n := h.requests.Load(); n != 0 {
				t.Errorf("usage server saw %d requests, want 0", n)
			}
			enc, _ := EncodeReport(got)
			if strings.Contains(enc, "secret-body") || strings.Contains(err.Error(), "secret-body") {
				t.Errorf("leak of refresher text: %s / %v", enc, err)
			}
		})
	}
}

func TestFetcherRefreshTerminalVariants(t *testing.T) {
	t.Parallel()
	for name, rerr := range map[string]error{
		"oauth2_invalid_grant":      &oauth2.RetrieveError{ErrorCode: "invalid_grant"},
		"wrapped_oauth2":            fmt.Errorf("refresh oauth token: %w", &oauth2.RetrieveError{ErrorCode: "invalid_grant"}),
		"text_invalid_grant":        errors.New("status 400: {\"error\":\"invalid_grant\"}"),
		"refresh_token_expired":     errors.New("status 401: refresh_token_expired"),
		"refresh_token_reused":      errors.New("status 401: refresh_token_reused"),
		"refresh_token_invalidated": errors.New("status 401: refresh_token_invalidated"),
	} {
		t.Run("fetcher_refresh_terminal_variants/"+name, func(t *testing.T) {
			t.Parallel()
			h := newFetcherHarness(t)
			h.refresher.err = rerr
			acc := h.oauthAccount(t, "openai", "a", time.Now().Add(-time.Hour))

			got, err := h.fetcher.Fetch(context.Background(), acc)
			if err != nil {
				t.Fatalf("Fetch() error = %v, want nil", err)
			}
			if got.Status != StatusUnavailable || got.Problem == nil || got.Problem.Kind != ProblemExpired {
				t.Errorf("report = %+v, want unavailable/expired", got)
			}
		})
	}
}

func TestFetcherCancelledContextDuringRefresh(t *testing.T) {
	t.Parallel()
	h := newFetcherHarness(t)
	h.refresher.err = context.Canceled
	acc := h.oauthAccount(t, "anthropic", "a", time.Now().Add(-time.Minute))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := h.fetcher.Fetch(ctx, acc)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Fetch() error = %v, want context.Canceled", err)
	}
}

func TestFetcherTransientErrorIsFetchError(t *testing.T) {
	t.Parallel()
	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	reg := accounts.New(s)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)
	src := newAnthropicSource(server.Client())
	src.url = server.URL
	f := newAccountFetcher(reg, map[string]providerSpec{"anthropic": {source: src}}, nil)

	created, err := reg.AddOAuth("anthropic", "a", accounts.OAuthCredential{AccessToken: "t", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	acc, _ := reg.Get(created.ID)

	rep, err := f.Fetch(context.Background(), acc)
	var fe *FetchError
	if !errors.As(err, &fe) || fe.Status != 429 || fe.RetryAfter != 120*time.Second {
		t.Fatalf("err = %v, want *FetchError 429 with RetryAfter 120s", err)
	}
	if rep.Status != StatusError || rep.Error != "rate limited" {
		t.Errorf("report = %+v, want error/rate limited", rep)
	}
}

// Not parallel: it captures the process-wide logger.
func TestDisplayNameNeverInKeyOrLogs(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	h := newFetcherHarness(t)
	const label = "someone@example.com"
	acc := h.oauthAccount(t, "anthropic", label, time.Now().Add(time.Hour))

	rep, err := h.fetcher.Fetch(context.Background(), acc)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	enc, _ := EncodeReport(rep)
	if DisplayName(acc) != label {
		t.Fatalf("DisplayName() = %q, want the label", DisplayName(acc))
	}
	for name, text := range map[string]string{"key": AccountKey(acc), "log": buf.String(), "report": enc} {
		if strings.Contains(text, label) || strings.Contains(text, "example.com") {
			t.Errorf("%s contains the label: %q", name, text)
		}
	}
}

func TestFetcherZeroExpiryIsUsedWithoutRefresh(t *testing.T) {
	t.Parallel()
	h := newFetcherHarness(t)
	acc := h.oauthAccount(t, "anthropic", "a", time.Time{})

	got, err := h.fetcher.Fetch(context.Background(), acc)
	if err != nil || got.Status != StatusAvailable {
		t.Fatalf("Fetch() = %+v, %v, want available", got, err)
	}
	if n := h.refresher.calls.Load(); n != 0 {
		t.Errorf("refresher called %d times for an unknown expiry, want 0", n)
	}
}

func TestFetcherRefreshFailureInsideBufferUsesCurrentToken(t *testing.T) {
	t.Parallel()
	h := newFetcherHarness(t)
	h.refresher.err = errors.New("boom")
	acc := h.oauthAccount(t, "anthropic", "a", time.Now().Add(10*time.Second))

	got, err := h.fetcher.Fetch(context.Background(), acc)
	if err != nil || got.Status != StatusAvailable {
		t.Fatalf("Fetch() = %+v, %v, want available using the still-valid token", got, err)
	}
	if auth, _ := h.lastAuth.Load().(string); auth != "Bearer access-old" {
		t.Errorf("Authorization = %q, want the current token", auth)
	}
}
