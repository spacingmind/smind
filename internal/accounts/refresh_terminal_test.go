package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/spacingmind/smind/internal/store"
)

type deadTokenRefresher struct {
	calls  atomic.Int64
	result OAuthCredential
	err    error
}

func (f *deadTokenRefresher) Refresh(ctx context.Context, cred OAuthCredential) (OAuthCredential, error) {
	f.calls.Add(1)
	if f.err != nil {
		return OAuthCredential{}, f.err
	}
	return f.result, nil
}

func expiredAccount(t *testing.T, r *Registry, refresh string) store.Account {
	t.Helper()
	acc, err := r.AddOAuth("anthropic", "p-"+refresh, OAuthCredential{
		AccessToken: "access-old", RefreshToken: refresh, ExpiresAt: time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("AddOAuth: %v", err)
	}
	return acc
}

func Test_ensurefresh_terminal_error_is_remembered_and_calls_the_refresher_once(t *testing.T) {
	r := newTestRegistry(t)
	acc := expiredAccount(t, r, "refresh-dead")
	f := &deadTokenRefresher{err: &oauth2.RetrieveError{ErrorCode: "invalid_grant"}}
	for i := 0; i < 5; i++ {
		_, err := r.EnsureFresh(context.Background(), acc.ID, f)
		if !errors.Is(err, ErrNeedsRelogin) {
			t.Fatalf("call %d: err = %v, want ErrNeedsRelogin", i, err)
		}
	}
	if got := f.calls.Load(); got != 1 {
		t.Errorf("refresher calls = %d, want 1", got)
	}
	if !r.NeedsRelogin(acc.ID) {
		t.Error("NeedsRelogin = false, want true")
	}
	if r.NeedsRelogin(acc.ID + 99) {
		t.Error("NeedsRelogin(unknown) = true")
	}
}

func Test_ensurefresh_transient_error_is_not_remembered(t *testing.T) {
	r := newTestRegistry(t)
	acc := expiredAccount(t, r, "refresh-ok")
	f := &deadTokenRefresher{err: errors.New("dial tcp: i/o timeout")}
	for i := 0; i < 2; i++ {
		_, err := r.EnsureFresh(context.Background(), acc.ID, f)
		if err == nil || errors.Is(err, ErrNeedsRelogin) {
			t.Fatalf("call %d: err = %v, want a plain error", i, err)
		}
	}
	if got := f.calls.Load(); got != 2 {
		t.Errorf("refresher calls = %d, want 2", got)
	}
	if r.NeedsRelogin(acc.ID) {
		t.Error("NeedsRelogin = true, want false")
	}
}

func Test_ensurefresh_new_credential_clears_the_mark(t *testing.T) {
	r := newTestRegistry(t)
	acc := expiredAccount(t, r, "refresh-dead")
	f := &deadTokenRefresher{err: &oauth2.RetrieveError{ErrorCode: "invalid_grant"}}
	if _, err := r.EnsureFresh(context.Background(), acc.ID, f); !errors.Is(err, ErrNeedsRelogin) {
		t.Fatalf("first: %v", err)
	}
	data, _ := json.Marshal(OAuthCredential{
		AccessToken: "access-2", RefreshToken: "refresh-new", ExpiresAt: time.Now().Add(-time.Minute),
	})
	if _, err := r.ReplaceCredential(acc.ID, string(data), ""); err != nil {
		t.Fatalf("ReplaceCredential: %v", err)
	}
	if r.NeedsRelogin(acc.ID) {
		t.Fatal("NeedsRelogin = true after the credential was replaced")
	}
	f.err = nil
	f.result = OAuthCredential{AccessToken: "access-fresh", RefreshToken: "refresh-new", ExpiresAt: time.Now().Add(time.Hour)}
	got, err := r.EnsureFresh(context.Background(), acc.ID, f)
	if err != nil {
		t.Fatalf("EnsureFresh: %v", err)
	}
	if got.OAuth.AccessToken != "access-fresh" {
		t.Errorf("AccessToken = %q", got.OAuth.AccessToken)
	}
	if n := f.calls.Load(); n != 2 {
		t.Errorf("refresher calls = %d, want 2", n)
	}
}
