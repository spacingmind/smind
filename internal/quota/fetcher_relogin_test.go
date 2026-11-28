package quota

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
)

type reloginCreds struct{ calls atomic.Int32 }

func (c *reloginCreds) EnsureFresh(context.Context, int64, accounts.OAuthRefresher) (accounts.Account, error) {
	c.calls.Add(1)
	return accounts.Account{}, fmt.Errorf("account 1: %w", accounts.ErrNeedsRelogin)
}

func Test_fetcher_needs_relogin_makes_no_provider_call(t *testing.T) {
	var usageHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		usageHits.Add(1)
		_, _ = w.Write([]byte(fetcherUsageBody))
	}))
	t.Cleanup(server.Close)
	src := newAnthropicSource(server.Client())
	src.url = server.URL
	refresher := &fetcherRefresher{}
	creds := &reloginCreds{}
	f := newAccountFetcher(creds, map[string]providerSpec{
		"anthropic": {source: src, refresher: refresher},
	}, nil)

	acc := accounts.Account{
		ID: 1, Provider: "anthropic", CredentialType: accounts.CredentialTypeOAuth,
		OAuth: &accounts.OAuthCredential{AccessToken: "a", RefreshToken: "r", ExpiresAt: time.Now().Add(-time.Hour)},
	}
	got, err := f.Fetch(context.Background(), acc)
	if err != nil {
		t.Fatalf("Fetch() error = %v, want nil", err)
	}
	if got.Status != StatusUnavailable || got.Problem == nil || got.Problem.Kind != ProblemExpired {
		t.Errorf("report = %+v, want unavailable/expired", got)
	}
	if n := usageHits.Load(); n != 0 {
		t.Errorf("usage endpoint requests = %d, want 0", n)
	}
	if n := refresher.calls.Load(); n != 0 {
		t.Errorf("refresher calls = %d, want 0", n)
	}
}
