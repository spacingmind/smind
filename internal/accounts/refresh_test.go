package accounts

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
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
