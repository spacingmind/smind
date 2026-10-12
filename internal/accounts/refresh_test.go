package accounts

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

type fakeRefresher struct {
	called bool
	result OAuthCredential
	err    error
}

func (f *fakeRefresher) Refresh(ctx context.Context, cred OAuthCredential) (OAuthCredential, error) {
	f.called = true
	if f.err != nil {
		return OAuthCredential{}, f.err
	}
	return f.result, nil
}

func TestRegistry_EnsureFresh(t *testing.T) {
	t.Parallel()

	t.Run("not expired, refresher not called", func(t *testing.T) {
		t.Parallel()

		r := newTestRegistry(t)
		created, err := r.AddOAuth("anthropic", "personal", OAuthCredential{
			AccessToken:  "access-old",
			RefreshToken: "refresh-old",
			ExpiresAt:    time.Now().Add(time.Hour),
		})
		if err != nil {
			t.Fatalf("AddOAuth() error = %v", err)
		}

		refresher := &fakeRefresher{}
		got, err := r.EnsureFresh(context.Background(), created.ID, refresher)
		if err != nil {
			t.Fatalf("EnsureFresh() error = %v", err)
		}
		if refresher.called {
			t.Error("EnsureFresh() called refresher, want not called")
		}
		if got.OAuth.AccessToken != "access-old" {
			t.Errorf("AccessToken = %q, want unchanged access-old", got.OAuth.AccessToken)
		}
	})

	t.Run("expired, refresher called and result persisted", func(t *testing.T) {
		t.Parallel()

		r := newTestRegistry(t)
		created, err := r.AddOAuth("anthropic", "personal", OAuthCredential{
			AccessToken:  "access-old",
			RefreshToken: "refresh-old",
			ExpiresAt:    time.Now().Add(-time.Minute),
		})
		if err != nil {
			t.Fatalf("AddOAuth() error = %v", err)
		}

		newExpiry := time.Now().Add(time.Hour).Truncate(time.Second)
		refresher := &fakeRefresher{result: OAuthCredential{
			AccessToken:  "access-new",
			RefreshToken: "refresh-new",
			ExpiresAt:    newExpiry,
		}}

		got, err := r.EnsureFresh(context.Background(), created.ID, refresher)
		if err != nil {
			t.Fatalf("EnsureFresh() error = %v", err)
		}
		if !refresher.called {
			t.Error("EnsureFresh() did not call refresher, want called")
		}
		if got.OAuth.AccessToken != "access-new" || got.OAuth.RefreshToken != "refresh-new" || !got.OAuth.ExpiresAt.Equal(newExpiry) {
			t.Errorf("OAuth = %+v, want access-new/refresh-new/%v", got.OAuth, newExpiry)
		}
		if !got.UpdatedAt.After(created.UpdatedAt) {
			t.Errorf("UpdatedAt = %v, want after %v", got.UpdatedAt, created.UpdatedAt)
		}

		persisted, err := r.Get(created.ID)
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if persisted.OAuth.AccessToken != "access-new" {
			t.Errorf("persisted AccessToken = %q, want access-new", persisted.OAuth.AccessToken)
		}
	})

	t.Run("refresher error, credential unchanged", func(t *testing.T) {
		t.Parallel()

		r := newTestRegistry(t)
		created, err := r.AddOAuth("anthropic", "personal", OAuthCredential{
			AccessToken:  "access-old",
			RefreshToken: "refresh-old",
			ExpiresAt:    time.Now().Add(-time.Minute),
		})
		if err != nil {
			t.Fatalf("AddOAuth() error = %v", err)
		}

		wantErr := errors.New("token endpoint unreachable")
		refresher := &fakeRefresher{err: wantErr}

		_, err = r.EnsureFresh(context.Background(), created.ID, refresher)
		if err == nil {
			t.Fatal("EnsureFresh() error = nil, want error")
		}
		if !errors.Is(err, wantErr) {
			t.Errorf("EnsureFresh() error = %v, want wrapping %v", err, wantErr)
		}

		persisted, err := r.Get(created.ID)
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if persisted.OAuth.AccessToken != "access-old" {
			t.Errorf("persisted AccessToken = %q, want unchanged access-old", persisted.OAuth.AccessToken)
		}
		if !persisted.UpdatedAt.Equal(created.UpdatedAt) {
			t.Errorf("persisted UpdatedAt = %v, want unchanged %v", persisted.UpdatedAt, created.UpdatedAt)
		}
	})

	t.Run("zero ExpiresAt is treated as expired", func(t *testing.T) {
		t.Parallel()

		r := newTestRegistry(t)
		created, err := r.AddOAuth("anthropic", "personal", OAuthCredential{
			AccessToken:  "access-old",
			RefreshToken: "refresh-old",
			// ExpiresAt intentionally left zero, e.g. a never-refreshed row.
		})
		if err != nil {
			t.Fatalf("AddOAuth() error = %v", err)
		}

		refresher := &fakeRefresher{result: OAuthCredential{
			AccessToken:  "access-new",
			RefreshToken: "refresh-new",
			ExpiresAt:    time.Now().Add(time.Hour),
		}}

		got, err := r.EnsureFresh(context.Background(), created.ID, refresher)
		if err != nil {
			t.Fatalf("EnsureFresh() error = %v", err)
		}
		if !refresher.called {
			t.Error("EnsureFresh() did not call refresher for zero ExpiresAt, want called")
		}
		if got.OAuth.AccessToken != "access-new" {
			t.Errorf("AccessToken = %q, want access-new", got.OAuth.AccessToken)
		}
	})

	t.Run("non-oauth account rejected", func(t *testing.T) {
		t.Parallel()

		r := newTestRegistry(t)
		created, err := r.AddAPIKey("openai", "work", "sk-test-123")
		if err != nil {
			t.Fatalf("AddAPIKey() error = %v", err)
		}

		if _, err := r.EnsureFresh(context.Background(), created.ID, &fakeRefresher{}); err == nil {
			t.Fatal("EnsureFresh() error = nil, want error for non-oauth account")
		}
	})

	t.Run("concurrent calls do not race or error", func(t *testing.T) {
		t.Parallel()

		r := newTestRegistry(t)
		created, err := r.AddOAuth("anthropic", "personal", OAuthCredential{
			AccessToken:  "access-old",
			RefreshToken: "refresh-old",
			ExpiresAt:    time.Now().Add(-time.Minute),
		})
		if err != nil {
			t.Fatalf("AddOAuth() error = %v", err)
		}

		const workers = 5
		var wg sync.WaitGroup
		errs := make(chan error, workers)
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				refresher := &fakeRefresher{result: OAuthCredential{
					AccessToken:  fmt.Sprintf("access-%d", i),
					RefreshToken: fmt.Sprintf("refresh-%d", i),
					ExpiresAt:    time.Now().Add(time.Hour),
				}}
				if _, err := r.EnsureFresh(context.Background(), created.ID, refresher); err != nil {
					errs <- err
				}
			}(i)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Errorf("concurrent EnsureFresh() error = %v", err)
		}

		final, err := r.Get(created.ID)
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if final.OAuth == nil {
			t.Fatal("final OAuth = nil, want set by one of the concurrent refreshes")
		}
	})
}

func TestOAuth2Refresher_PreservesRefreshTokenWhenOmitted(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access-new","token_type":"Bearer","expires_in":3600}`))
	}))
	defer srv.Close()

	refresher := NewOAuth2Refresher(srv.URL, "client-id")
	got, err := refresher.Refresh(context.Background(), OAuthCredential{
		AccessToken:  "access-old",
		RefreshToken: "refresh-old",
		ExpiresAt:    time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if got.AccessToken != "access-new" {
		t.Errorf("AccessToken = %q, want access-new", got.AccessToken)
	}
	if got.RefreshToken != "refresh-old" {
		t.Errorf("RefreshToken = %q, want preserved refresh-old (token endpoint omitted it)", got.RefreshToken)
	}
}

func TestOAuth2Refresher_WithClientSecretSendsSecretInParams(t *testing.T) {
	t.Parallel()

	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access-new","refresh_token":"refresh-new","token_type":"Bearer","expires_in":3600}`))
	}))
	defer srv.Close()

	refresher := NewOAuth2Refresher(srv.URL, "client-id", WithOAuth2ClientSecret("client-secret"))
	got, err := refresher.Refresh(context.Background(), OAuthCredential{RefreshToken: "refresh-old"})
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if got.AccessToken != "access-new" {
		t.Errorf("AccessToken = %q, want access-new", got.AccessToken)
	}

	vals, err := url.ParseQuery(gotBody)
	if err != nil {
		t.Fatalf("url.ParseQuery(%q) error = %v", gotBody, err)
	}
	if vals.Get("client_secret") != "client-secret" {
		t.Errorf("request client_secret = %q, want client-secret", vals.Get("client_secret"))
	}
	if vals.Get("client_id") != "client-id" {
		t.Errorf("request client_id = %q, want client-id", vals.Get("client_id"))
	}
}

func TestOAuth2Refresher_WithHTTPClientOverride(t *testing.T) {
	t.Parallel()

	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access-new","token_type":"Bearer","expires_in":3600}`))
	}))
	defer srv.Close()

	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	client := &http.Client{Transport: &rewriteTransport{target: target}}

	// tokenURL points nowhere reachable; only the HTTP client override's
	// transport (which rewrites every request to srv) makes this resolve.
	refresher := NewOAuth2Refresher("http://unreachable.invalid/token", "client-id", WithOAuth2HTTPClient(client))
	if _, err := refresher.Refresh(context.Background(), OAuthCredential{RefreshToken: "refresh-old"}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if !called {
		t.Error("Refresh() did not use the overridden HTTP client, want it to hit the local server")
	}
}

// funcRefresher adapts a function to OAuthRefresher.
type funcRefresher func(context.Context, OAuthCredential) (OAuthCredential, error)

func (f funcRefresher) Refresh(ctx context.Context, c OAuthCredential) (OAuthCredential, error) {
	return f(ctx, c)
}

// countingRefresher counts concurrent refreshes and simulates a slow token
// endpoint so overlapping EnsureFresh calls are forced to interleave.
type countingRefresher struct {
	mu     sync.Mutex
	calls  int64
	delay  time.Duration
	enter  chan struct{} // if non-nil, signal on each entry before delaying
	result OAuthCredential
	err    error
}

func (f *countingRefresher) Refresh(ctx context.Context, cred OAuthCredential) (OAuthCredential, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.enter != nil {
		f.enter <- struct{}{}
	}
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return OAuthCredential{}, f.err
	}
	return f.result, nil
}

func (f *countingRefresher) count() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestRegistry_EnsureFresh_ConcurrentSingleRefresh(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	created, err := r.AddOAuth("anthropic", "personal", OAuthCredential{
		AccessToken:  "access-old",
		RefreshToken: "refresh-old",
		ExpiresAt:    time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("AddOAuth() error = %v", err)
	}

	refresher := &countingRefresher{
		delay: 50 * time.Millisecond,
		result: OAuthCredential{
			AccessToken:  "access-new",
			RefreshToken: "refresh-new",
			ExpiresAt:    time.Now().Add(time.Hour),
		},
	}

	const workers = 8
	var wg sync.WaitGroup
	tokens := make([]string, workers)
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got, err := r.EnsureFresh(context.Background(), created.ID, refresher)
			if err != nil {
				t.Errorf("EnsureFresh() error = %v", err)
				return
			}
			tokens[i] = got.OAuth.AccessToken
		}(i)
	}
	wg.Wait()

	if got := refresher.count(); got != 1 {
		t.Errorf("refresher called %d times, want exactly 1", got)
	}
	for i, tok := range tokens {
		if tok != "access-new" {
			t.Errorf("worker %d AccessToken = %q, want access-new", i, tok)
		}
	}
}

func TestRegistry_EnsureFresh_OtherAccountsIndependent(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	mk := func(label string) store.Account {
		t.Helper()
		a, err := r.AddOAuth("anthropic", label, OAuthCredential{
			AccessToken:  "access-old",
			RefreshToken: "refresh-old",
			ExpiresAt:    time.Now().Add(-time.Minute),
		})
		if err != nil {
			t.Fatalf("AddOAuth(%s) error = %v", label, err)
		}
		return a
	}
	a1, a2 := mk("one"), mk("two")

	// A's refresher blocks until B's has been entered. With a global lock B
	// could never start, so A would sit until its own timeout.
	bEntered := make(chan struct{})
	refA := funcRefresher(func(ctx context.Context, _ OAuthCredential) (OAuthCredential, error) {
		select {
		case <-bEntered:
		case <-time.After(3 * time.Second):
			return OAuthCredential{}, errors.New("account B refresh never started while A was refreshing")
		}
		return OAuthCredential{AccessToken: "access-a", ExpiresAt: time.Now().Add(time.Hour)}, nil
	})
	refB := funcRefresher(func(ctx context.Context, _ OAuthCredential) (OAuthCredential, error) {
		close(bEntered)
		return OAuthCredential{AccessToken: "access-b", ExpiresAt: time.Now().Add(time.Hour)}, nil
	})

	done := make(chan error, 2)
	go func() {
		_, err := r.EnsureFresh(context.Background(), a1.ID, refA)
		done <- err
	}()
	go func() {
		_, err := r.EnsureFresh(context.Background(), a2.ID, refB)
		done <- err
	}()

	timeout := time.After(5 * time.Second)
	for range 2 {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("EnsureFresh() error = %v", err)
			}
		case <-timeout:
			t.Fatal("refreshes for different accounts did not complete in parallel; cross-account blocking suspected")
		}
	}
}

func TestRegistry_EnsureFresh_ValidTokenNoRefreshRepeated(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	created, err := r.AddOAuth("anthropic", "personal", OAuthCredential{
		AccessToken:  "access-fresh",
		RefreshToken: "refresh-old",
		ExpiresAt:    time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("AddOAuth() error = %v", err)
	}

	refresher := &countingRefresher{}
	for range 3 {
		got, err := r.EnsureFresh(context.Background(), created.ID, refresher)
		if err != nil {
			t.Fatalf("EnsureFresh() error = %v", err)
		}
		if got.OAuth.AccessToken != "access-fresh" {
			t.Errorf("AccessToken = %q, want access-fresh", got.OAuth.AccessToken)
		}
	}
	if got := refresher.count(); got != 0 {
		t.Errorf("refresher called %d times, want 0 for unexpired credential", got)
	}
}

func TestRegistry_EnsureFresh_RefresherErrorReleasesLock(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	created, err := r.AddOAuth("anthropic", "personal", OAuthCredential{
		AccessToken:  "access-old",
		RefreshToken: "refresh-old",
		ExpiresAt:    time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("AddOAuth() error = %v", err)
	}

	failing := &countingRefresher{err: errors.New("token endpoint down")}
	if _, err := r.EnsureFresh(context.Background(), created.ID, failing); err == nil {
		t.Fatal("EnsureFresh() error = nil, want failure")
	}

	succeeding := &countingRefresher{result: OAuthCredential{
		AccessToken:  "access-new",
		RefreshToken: "refresh-new",
		ExpiresAt:    time.Now().Add(time.Hour),
	}}
	done := make(chan Account, 1)
	go func() {
		acc, err := r.EnsureFresh(context.Background(), created.ID, succeeding)
		if err != nil {
			t.Errorf("EnsureFresh() after failure error = %v", err)
		}
		done <- acc
	}()
	select {
	case acc := <-done:
		if acc.OAuth == nil || acc.OAuth.AccessToken != "access-new" {
			t.Fatalf("AccessToken after retry = %+v, want access-new", acc.OAuth)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("lock not released after refresher error; retry deadlocked")
	}
}

// TestRegistry_CredentialReplacedHookFiresOnlyOnReplace is plan scenario
// registry_credential_replaced_hook_fires_only_on_replace.
func TestRegistry_CredentialReplacedHookFiresOnlyOnReplace(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	created, err := r.AddOAuth("anthropic", "personal", OAuthCredential{
		AccessToken:  "access-old",
		RefreshToken: "refresh-old",
		ExpiresAt:    time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("AddOAuth() error = %v", err)
	}
	var fired []int64
	r.SetCredentialReplacedHook(func(id int64) { fired = append(fired, id) })

	refresher := &fakeRefresher{result: OAuthCredential{
		AccessToken: "access-new", RefreshToken: "refresh-new", ExpiresAt: time.Now().Add(time.Hour),
	}}
	if _, err := r.EnsureFresh(context.Background(), created.ID, refresher); err != nil {
		t.Fatalf("EnsureFresh() error = %v", err)
	}
	if !refresher.called || len(fired) != 0 {
		t.Fatalf("after refresh: refreshed = %v, hook calls = %v, want refreshed and no hook", refresher.called, fired)
	}

	if _, err := r.ReplaceCredential(created.ID, "", ""); err == nil {
		t.Fatal("ReplaceCredential(empty) error = nil")
	}
	if len(fired) != 0 {
		t.Fatalf("hook fired on failed replace: %v", fired)
	}

	if _, err := r.ReplaceCredential(created.ID, "sk-new", ""); err != nil {
		t.Fatalf("ReplaceCredential() error = %v", err)
	}
	if len(fired) != 1 || fired[0] != created.ID {
		t.Fatalf("hook calls after replace = %v, want [%d]", fired, created.ID)
	}

	r.SetCredentialReplacedHook(nil) // nil hook and nil registry are safe
	if _, err := r.ReplaceCredential(created.ID, "sk-newer", ""); err != nil {
		t.Fatalf("ReplaceCredential() with nil hook error = %v", err)
	}
	var nilReg *Registry
	nilReg.SetCredentialReplacedHook(func(int64) {})
	if len(fired) != 1 {
		t.Fatalf("hook calls = %v, want still 1", fired)
	}
}
