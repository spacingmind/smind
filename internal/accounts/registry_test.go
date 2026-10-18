package accounts

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return New(s)
}

func TestRegistry_AddAPIKey(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)

	created, err := r.AddAPIKey("openai", "work", "sk-test-123")
	if err != nil {
		t.Fatalf("AddAPIKey() error = %v", err)
	}
	if created.ID == 0 {
		t.Fatalf("AddAPIKey() ID = 0, want nonzero")
	}
	if created.CredentialType != CredentialTypeAPIKey {
		t.Errorf("CredentialType = %q, want %q", created.CredentialType, CredentialTypeAPIKey)
	}

	got, err := r.Get(created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.APIKey == nil {
		t.Fatalf("Get().APIKey = nil, want non-nil")
	}
	if got.APIKey.Key != "sk-test-123" {
		t.Errorf("APIKey.Key = %q, want %q", got.APIKey.Key, "sk-test-123")
	}
	if got.OAuth != nil {
		t.Errorf("OAuth = %+v, want nil", got.OAuth)
	}
	if got.Provider != "openai" || got.Label != "work" {
		t.Errorf("Provider/Label = %q/%q, want openai/work", got.Provider, got.Label)
	}
}

func TestRegistry_AddOAuth(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	expiresAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	created, err := r.AddOAuth("anthropic", "personal", OAuthCredential{
		AccessToken:  "access-abc",
		RefreshToken: "refresh-xyz",
		ExpiresAt:    expiresAt,
	})
	if err != nil {
		t.Fatalf("AddOAuth() error = %v", err)
	}
	if created.CredentialType != CredentialTypeOAuth {
		t.Errorf("CredentialType = %q, want %q", created.CredentialType, CredentialTypeOAuth)
	}

	got, err := r.Get(created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.OAuth == nil {
		t.Fatalf("Get().OAuth = nil, want non-nil")
	}
	if got.OAuth.AccessToken != "access-abc" || got.OAuth.RefreshToken != "refresh-xyz" || !got.OAuth.ExpiresAt.Equal(expiresAt) {
		t.Errorf("OAuth = %+v, want access-abc/refresh-xyz/%v", got.OAuth, expiresAt)
	}
	if got.APIKey != nil {
		t.Errorf("APIKey = %+v, want nil", got.APIKey)
	}
}

func TestRegistry_List(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)

	empty, err := r.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("List() = %+v, want empty", empty)
	}

	apiKeyAccount, err := r.AddAPIKey("openai", "work", "sk-test-123")
	if err != nil {
		t.Fatalf("AddAPIKey() error = %v", err)
	}
	oauthAccount, err := r.AddOAuth("anthropic", "personal", OAuthCredential{
		AccessToken:  "access-abc",
		RefreshToken: "refresh-xyz",
		ExpiresAt:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("AddOAuth() error = %v", err)
	}

	list, err := r.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("List() len = %d, want 2", len(list))
	}
	if list[0].ID != apiKeyAccount.ID || list[0].APIKey == nil {
		t.Errorf("List()[0] = %+v, want api key account %d", list[0], apiKeyAccount.ID)
	}
	if list[1].ID != oauthAccount.ID || list[1].OAuth == nil {
		t.Errorf("List()[1] = %+v, want oauth account %d", list[1], oauthAccount.ID)
	}
}

func TestRegistry_GetMissing(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)

	if _, err := r.Get(999); err == nil {
		t.Fatal("Get() error = nil, want error for missing account")
	}
}

func TestRegistry_GetUnknownCredentialType(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)

	// Bypass the registry's typed constructors to simulate a corrupted or
	// pre-migration row with an unrecognized credential_type.
	raw, err := r.store.CreateAccount(store.Account{
		Provider:       "anthropic",
		Label:          "corrupt",
		CredentialType: "bogus",
		CredentialData: "{}",
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}

	if _, err := r.Get(raw.ID); err == nil {
		t.Fatal("Get() error = nil, want error for unknown credential type")
	}
}

func TestRegistry_AddAPIKeyWithBaseURL(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)

	created, err := r.AddAPIKeyWithBaseURL("anthropic", "local", "sk-ant-x", "http://127.0.0.1:8080")
	if err != nil {
		t.Fatalf("AddAPIKeyWithBaseURL() error = %v", err)
	}

	got, err := r.Get(created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.APIKey == nil {
		t.Fatal("APIKey credential not decoded")
	}
	if got.APIKey.Key != "sk-ant-x" {
		t.Errorf("Key = %q", got.APIKey.Key)
	}
	if got.APIKey.BaseURL != "http://127.0.0.1:8080" {
		t.Errorf("BaseURL = %q, want round-trip", got.APIKey.BaseURL)
	}

	// empty base_url = provider default, and AddAPIKey keeps working
	plain, err := r.AddAPIKey("anthropic", "plain", "sk-ant-y")
	if err != nil {
		t.Fatalf("AddAPIKey() error = %v", err)
	}
	got, err = r.Get(plain.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.APIKey.BaseURL != "" {
		t.Errorf("plain AddAPIKey BaseURL = %q, want empty", got.APIKey.BaseURL)
	}
}

func TestRegistry_AddAPIKeyWithBaseURL_Validation(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)

	ok := []string{
		"",                        // provider default
		"https://api.example.com", // https anywhere
		"http://127.0.0.1:8080",   // loopback with port
		"http://localhost:3000",   // localhost
		"http://[::1]:9000",       // ipv6 loopback
	}
	for _, u := range ok {
		if _, err := r.AddAPIKeyWithBaseURL("anthropic", "t", "k", u); err != nil {
			t.Errorf("AddAPIKeyWithBaseURL(%q) error = %v, want accepted", u, err)
		}
	}

	bad := map[string]string{
		"http://example.com":      "non-loopback http",
		"http://192.168.1.5:8080": "LAN http",
		"ftp://127.0.0.1":         "non-http scheme",
		"https://":                "hostless",
	}
	for u := range bad {
		if _, err := r.AddAPIKeyWithBaseURL("anthropic", "t", "k", u); err == nil {
			t.Errorf("AddAPIKeyWithBaseURL(%q) succeeded, want rejected (%s)", u, bad[u])
		}
	}
}

func TestRegistry_Rename(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	created, err := r.AddAPIKey("anthropic", "old", "sk-test")
	if err != nil {
		t.Fatalf("AddAPIKey() error = %v", err)
	}

	updated, err := r.Rename(created.ID, "work-claude")
	if err != nil {
		t.Fatalf("Rename() error = %v", err)
	}
	if updated.Label != "work-claude" {
		t.Fatalf("Rename() Label = %q, want work-claude", updated.Label)
	}
}

func TestRegistry_RenameEmptyLabelRejected(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	created, err := r.AddAPIKey("anthropic", "old", "sk-test")
	if err != nil {
		t.Fatalf("AddAPIKey() error = %v", err)
	}
	for _, label := range []string{"", "   "} {
		if _, err := r.Rename(created.ID, label); err == nil {
			t.Fatalf("Rename(%q) error = nil, want rejected", label)
		}
	}
}

func TestRegistry_RenameMissing(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	if _, err := r.Rename(999, "x"); err == nil {
		t.Fatal("Rename(999) error = nil, want a not-found error")
	}
}

func TestRegistry_ReplaceCredential(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	created, err := r.AddAPIKey("anthropic", "work", "sk-old")
	if err != nil {
		t.Fatalf("AddAPIKey() error = %v", err)
	}

	// api_key -> oauth swap changes both credential_data and credential_type.
	updated, err := r.ReplaceCredential(created.ID,
		`{"access_token":"a","refresh_token":"r","expires_at":"2030-01-01T00:00:00Z"}`, "")
	if err != nil {
		t.Fatalf("ReplaceCredential(oauth) error = %v", err)
	}
	if updated.CredentialType != CredentialTypeOAuth {
		t.Fatalf("ReplaceCredential(oauth) CredentialType = %q, want oauth", updated.CredentialType)
	}
	got, err := r.Get(created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.OAuth == nil || got.OAuth.RefreshToken != "r" {
		t.Fatalf("Get() OAuth = %+v, want the swapped refresh credential", got.OAuth)
	}

	// oauth -> api_key swap, with a base_url exactly like account.add's.
	updated, err = r.ReplaceCredential(created.ID, "sk-new", "http://127.0.0.1:8080")
	if err != nil {
		t.Fatalf("ReplaceCredential(api_key) error = %v", err)
	}
	if updated.CredentialType != CredentialTypeAPIKey {
		t.Fatalf("ReplaceCredential(api_key) CredentialType = %q, want api_key", updated.CredentialType)
	}
	got, err = r.Get(created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.APIKey == nil || got.APIKey.Key != "sk-new" || got.APIKey.BaseURL != "http://127.0.0.1:8080" {
		t.Fatalf("Get() APIKey = %+v, want sk-new with the loopback base_url", got.APIKey)
	}
}

func TestRegistry_ReplaceCredentialEmptyRejected(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	created, err := r.AddAPIKey("anthropic", "work", "sk-old")
	if err != nil {
		t.Fatalf("AddAPIKey() error = %v", err)
	}
	if _, err := r.ReplaceCredential(created.ID, "  ", ""); err == nil {
		t.Fatal("ReplaceCredential(empty) error = nil, want rejected")
	}
}

func TestRegistry_ReplaceCredentialMissing(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	if _, err := r.ReplaceCredential(999, "sk-x", ""); err == nil {
		t.Fatal("ReplaceCredential(999) error = nil, want a not-found error")
	}
}

func TestRegistry_Delete(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	created, err := r.AddAPIKey("anthropic", "work", "sk-test")
	if err != nil {
		t.Fatalf("AddAPIKey() error = %v", err)
	}
	// Give the cascade something to clear: an affinity row pinned to this
	// account (ADR-0015 condition b).
	if _, err := r.store.CreateRoutingDecision(store.RoutingDecision{
		SessionKey: "sess-a", AccountID: created.ID, Policy: "affinity",
		DecidedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour),
	}); err != nil {
		t.Fatalf("CreateRoutingDecision() error = %v", err)
	}

	if err := r.Delete(created.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if _, err := r.Get(created.ID); err == nil {
		t.Fatal("Get() after delete error = nil, want a not-found error")
	}
	decisions, err := r.store.ListRoutingDecisions()
	if err != nil {
		t.Fatalf("ListRoutingDecisions() error = %v", err)
	}
	if len(decisions) != 0 {
		t.Fatalf("ListRoutingDecisions() after delete = %+v, want no affinity row still pointing at the removed account", decisions)
	}
}

func TestRegistry_DeleteMissing(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	if err := r.Delete(999); err == nil {
		t.Fatal("Delete(999) error = nil, want a not-found error")
	}
}

func TestRegistry_NotifierLifecycle(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	created, err := r.AddAPIKey("anthropic", "work", "sk-test")
	if err != nil {
		t.Fatalf("AddAPIKey() error = %v", err)
	}

	rec := &recordingNotifier{}
	r.SetNotifier(rec)

	if _, err := r.Rename(created.ID, "renamed"); err != nil {
		t.Fatalf("Rename() error = %v", err)
	}
	if _, err := r.ReplaceCredential(created.ID, "sk-new", ""); err != nil {
		t.Fatalf("ReplaceCredential() error = %v", err)
	}
	if err := r.Delete(created.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if len(rec.updated) != 2 || rec.updated[0].Label != "renamed" || rec.updated[1].CredentialType != CredentialTypeAPIKey {
		t.Fatalf("updated notifications = %+v, want renamed then swapped", rec.updated)
	}
	if len(rec.removed) != 1 || rec.removed[0] != created.ID {
		t.Fatalf("removed notifications = %+v, want [%d]", rec.removed, created.ID)
	}
}

type recordingNotifier struct {
	updated []store.Account
	removed []int64
}

func (n *recordingNotifier) NotifyAccountUpdated(a store.Account) { n.updated = append(n.updated, a) }
func (n *recordingNotifier) NotifyAccountRemoved(id int64)        { n.removed = append(n.removed, id) }

// A credential replaced by paste (account.updateCredential) no longer
// describes the vendor account its identity came from, so the identity is
// cleared; a later OAuth login of that vendor account must not silently
// convert the pasted-credential row back.
func TestRegistry_ReplaceCredentialIdentity(t *testing.T) {
	t.Parallel()

	t.Run("updateCredential clears identity", func(t *testing.T) {
		t.Parallel()

		coord, registry, login := newReloginCoordinator(t)
		first, err := relogin(t, coord, login, "work", LoginOptions{}, "subject-1")
		if err != nil || !first.Account.HasIdentity {
			t.Fatalf("Login() = %+v, %v, want an account with identity", first, err)
		}
		id := first.Account.ID

		updated, err := registry.ReplaceCredential(id, "sk-ant-pasted", "")
		if err != nil {
			t.Fatalf("ReplaceCredential() error = %v", err)
		}
		if updated.Identity != "" {
			t.Fatal("ReplaceCredential() left the identity of the replaced credential")
		}
		if got, _ := registry.Get(id); got.HasIdentity || got.CredentialType != CredentialTypeAPIKey || got.APIKey == nil || got.APIKey.Key != "sk-ant-pasted" {
			t.Fatalf("account after paste = %+v, want an api_key row without identity", got)
		}

		// The same vendor account logging in again creates a new row; the
		// pasted-credential row is untouched.
		second, err := relogin(t, coord, login, "again", LoginOptions{}, "subject-1")
		if err != nil || !second.Created || second.Account.ID == id || !second.Account.HasIdentity {
			t.Fatalf("OAuth login after paste = %+v, %v, want a new row with identity", second, err)
		}
		if got, _ := registry.Get(id); got.CredentialType != CredentialTypeAPIKey || got.APIKey == nil || got.APIKey.Key != "sk-ant-pasted" {
			t.Fatalf("pasted-credential row changed by an OAuth login: %+v", got)
		}

		// A row-level Re-login of the pasted row now adopts-or-conflicts like
		// any legacy row: here the new row owns the identity.
		_, err = relogin(t, coord, login, "x", LoginOptions{AccountID: id}, "subject-1")
		var le *LoginError
		if !errors.As(err, &le) || le.Code != LoginCodeIdentityInUse {
			t.Fatalf("Re-login of the pasted row error = %v, want identity_in_use", err)
		}
	})

	t.Run("oauth re-login of the same account keeps the identity", func(t *testing.T) {
		t.Parallel()

		coord, registry, login := newReloginCoordinator(t)
		first, err := relogin(t, coord, login, "work", LoginOptions{}, "subject-1")
		if err != nil {
			t.Fatalf("Login() error = %v", err)
		}
		if _, err := relogin(t, coord, login, "work", LoginOptions{AccountID: first.Account.ID}, "subject-1"); err != nil {
			t.Fatalf("Re-login error = %v", err)
		}
		if got, _ := registry.Get(first.Account.ID); !got.HasIdentity {
			t.Fatal("OAuth re-login of the same account dropped its identity")
		}
	})
}

// blockingRefresher parks inside Refresh until released, so a test can start
// a re-login while a refresh is in flight.
type blockingRefresher struct {
	entered chan struct{}
	release chan struct{}
}

func (b *blockingRefresher) Refresh(_ context.Context, cred OAuthCredential) (OAuthCredential, error) {
	close(b.entered)
	<-b.release
	return OAuthCredential{AccessToken: "refreshed-old", RefreshToken: cred.RefreshToken, ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func TestRegistry_ReloginDuringRefresh(t *testing.T) {
	t.Parallel()

	t.Run("relogin during an in-flight refresh keeps the new credential", func(t *testing.T) {
		t.Parallel()

		registry := newTestRegistry(t)
		acct, err := registry.AddOAuth("anthropic", "work", OAuthCredential{AccessToken: "expired", RefreshToken: "old-r", ExpiresAt: time.Now().Add(-time.Hour)})
		if err != nil {
			t.Fatalf("AddOAuth() error = %v", err)
		}

		refresher := &blockingRefresher{entered: make(chan struct{}), release: make(chan struct{})}
		refreshDone := make(chan error, 1)
		go func() {
			_, err := registry.EnsureFresh(context.Background(), acct.ID, refresher)
			refreshDone <- err
		}()
		<-refresher.entered // the refresh now holds the account's lock

		fresh := OAuthCredential{AccessToken: "from-relogin", RefreshToken: "new-r", ExpiresAt: time.Now().Add(time.Hour)}
		reloginDone := make(chan error, 1)
		go func() {
			_, err := registry.ReplaceOAuth(acct.ID, fresh, "", true)
			reloginDone <- err
		}()

		// The re-login must wait for the refresh rather than race it.
		select {
		case err := <-reloginDone:
			t.Fatalf("ReplaceOAuth returned while a refresh was in flight (err = %v)", err)
		case <-time.After(200 * time.Millisecond):
		}

		close(refresher.release)
		if err := <-refreshDone; err != nil {
			t.Fatalf("EnsureFresh() error = %v", err)
		}
		if err := <-reloginDone; err != nil {
			t.Fatalf("ReplaceOAuth() error = %v", err)
		}

		got, err := registry.Get(acct.ID)
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if got.OAuth == nil || got.OAuth.AccessToken != "from-relogin" || got.OAuth.RefreshToken != "new-r" {
			t.Fatalf("credential after re-login = %+v, want the new login's tokens (an in-flight refresh must not overwrite it)", got.OAuth)
		}
	})
}
