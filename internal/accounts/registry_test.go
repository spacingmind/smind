package accounts

import (
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
