package accounts

import (
	"context"
	"errors"
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
}
