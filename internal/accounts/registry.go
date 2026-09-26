package accounts

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

// Account is a typed view of a store.Account, with credential_data decoded
// into the credential matching its CredentialType.
type Account struct {
	ID             int64
	Provider       string
	Label          string
	CredentialType string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	APIKey         *APIKeyCredential
	OAuth          *OAuthCredential
}

// Notifier receives account lifecycle notifications (ADR-0015): update
// after rename/credential swap, remove after delete. Called synchronously
// from the Registry method that made the change, after the underlying
// store write committed; implementations (internal/wsapi's bus adapter)
// must not block. Mirrors internal/profiles.Notifier's shape.
type Notifier interface {
	NotifyAccountUpdated(a store.Account)
	NotifyAccountRemoved(id int64)
}

// Registry is a typed accounts registry backed by a *store.Store.
type Registry struct {
	store *store.Store

	// notifier, if set via SetNotifier, receives account lifecycle
	// notifications so the wsapi server can push them as ADR-0009-shaped
	// subscription events. Mirrors internal/profiles.Registry's pattern.
	notifierMu sync.Mutex
	notifier   Notifier
}

// New returns a Registry backed by s.
func New(s *store.Store) *Registry {
	return &Registry{store: s}
}

// SetNotifier registers n. Nil-safe, matching profiles.Registry.
// SetNotifier's shape.
func (r *Registry) SetNotifier(n Notifier) {
	if r == nil {
		return
	}
	r.notifierMu.Lock()
	r.notifier = n
	r.notifierMu.Unlock()
}

func (r *Registry) getNotifier() Notifier {
	if r == nil {
		return nil
	}
	r.notifierMu.Lock()
	n := r.notifier
	r.notifierMu.Unlock()
	return n
}

// AddAPIKey creates a new api_key account.
func (r *Registry) AddAPIKey(provider, label, key string) (store.Account, error) {
	return r.AddAPIKeyWithBaseURL(provider, label, key, "")
}

// AddAPIKeyWithBaseURL creates a new api_key account whose credential
// carries an upstream base_url override ("" = provider default). The URL
// is validated by ValidateBaseURL (https anywhere, http loopback only).
func (r *Registry) AddAPIKeyWithBaseURL(provider, label, key, baseURL string) (store.Account, error) {
	if err := ValidateBaseURL(baseURL); err != nil {
		return store.Account{}, err
	}
	data, err := json.Marshal(APIKeyCredential{Key: key, BaseURL: baseURL})
	if err != nil {
		return store.Account{}, fmt.Errorf("marshal api key credential: %w", err)
	}
	return r.store.CreateAccount(store.Account{
		Provider:       provider,
		Label:          label,
		CredentialType: CredentialTypeAPIKey,
		CredentialData: string(data),
	})
}

// AddOAuth creates a new oauth account.
func (r *Registry) AddOAuth(provider, label string, cred OAuthCredential) (store.Account, error) {
	data, err := json.Marshal(cred)
	if err != nil {
		return store.Account{}, fmt.Errorf("marshal oauth credential: %w", err)
	}
	return r.store.CreateAccount(store.Account{
		Provider:       provider,
		Label:          label,
		CredentialType: CredentialTypeOAuth,
		CredentialData: string(data),
	})
}

// AddWithCredential creates a new account whose credential is parsed the
// account.add way (see ParseCredential) -- both account.add and
// account.updateCredential go through that one parser, so the two RPCs can
// never drift apart.
func (r *Registry) AddWithCredential(provider, label, credential, baseURL string) (store.Account, error) {
	credentialType, credentialData, err := r.ParseCredential(credential, baseURL)
	if err != nil {
		return store.Account{}, err
	}
	return r.store.CreateAccount(store.Account{
		Provider:       provider,
		Label:          label,
		CredentialType: credentialType,
		CredentialData: credentialData,
	})
}

// ParseCredential is the single account.add/account.updateCredential
// credential parser (ADR-0015: updateCredential parses "exactly the way
// account.add parses it"): a JSON body with a refresh_token is an OAuth
// credential; anything else is a bare API key, with baseUrl validated by
// ValidateBaseURL and attached to the API-key credential. Returns the
// resulting credential type and marshaled credential_data.
func (r *Registry) ParseCredential(credential, baseURL string) (credentialType, credentialData string, err error) {
	baseURL = strings.TrimSpace(baseURL)
	var oauth OAuthCredential
	if err := json.Unmarshal([]byte(credential), &oauth); err == nil && oauth.RefreshToken != "" {
		data, err := json.Marshal(oauth)
		if err != nil {
			return "", "", fmt.Errorf("marshal oauth credential: %w", err)
		}
		return CredentialTypeOAuth, string(data), nil
	}

	if err := ValidateBaseURL(baseURL); err != nil {
		return "", "", err
	}
	data, err := json.Marshal(APIKeyCredential{Key: strings.TrimSpace(credential), BaseURL: baseURL})
	if err != nil {
		return "", "", fmt.Errorf("marshal api key credential: %w", err)
	}
	return CredentialTypeAPIKey, string(data), nil
}

// Rename relabels the account with the given id, firing NotifyAccountUpdated
// on success. An empty label is rejected here; an unknown id surfaces as the
// store's not-found error.
func (r *Registry) Rename(id int64, label string) (store.Account, error) {
	if strings.TrimSpace(label) == "" {
		return store.Account{}, fmt.Errorf("label is required")
	}
	updated, err := r.store.RenameAccount(id, label)
	if err != nil {
		return store.Account{}, err
	}
	if n := r.getNotifier(); n != nil {
		n.NotifyAccountUpdated(updated)
	}
	return updated, nil
}

// ReplaceCredential swaps the account's credential (and possibly its type)
// with the parsed credential, firing NotifyAccountUpdated on success. It is
// account.updateCredential's registry call: parsing mirrors account.add
// exactly (see ParseCredential).
func (r *Registry) ReplaceCredential(id int64, credential, baseURL string) (store.Account, error) {
	if strings.TrimSpace(credential) == "" {
		return store.Account{}, fmt.Errorf("credential is required")
	}
	credentialType, credentialData, err := r.ParseCredential(credential, baseURL)
	if err != nil {
		return store.Account{}, err
	}
	updated, err := r.store.ReplaceAccountCredential(id, credentialType, credentialData)
	if err != nil {
		return store.Account{}, err
	}
	if n := r.getNotifier(); n != nil {
		n.NotifyAccountUpdated(updated)
	}
	return updated, nil
}

// Delete permanently removes the account and everything referencing it
// (routing decisions including session affinity, quota snapshots, workspace
// links -- see store.DeleteAccount), firing NotifyAccountRemoved on
// success.
func (r *Registry) Delete(id int64) error {
	if err := r.store.DeleteAccount(id); err != nil {
		return err
	}
	if n := r.getNotifier(); n != nil {
		n.NotifyAccountRemoved(id)
	}
	return nil
}

// Get returns the account with the given id, with its credential decoded.
func (r *Registry) Get(id int64) (Account, error) {
	a, err := r.store.GetAccount(id)
	if err != nil {
		return Account{}, err
	}
	return decodeAccount(a)
}

// List returns all accounts, with credentials decoded.
func (r *Registry) List() ([]Account, error) {
	stored, err := r.store.ListAccounts()
	if err != nil {
		return nil, err
	}
	accounts := make([]Account, 0, len(stored))
	for _, a := range stored {
		decoded, err := decodeAccount(a)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, decoded)
	}
	return accounts, nil
}

func decodeAccount(a store.Account) (Account, error) {
	out := Account{
		ID:             a.ID,
		Provider:       a.Provider,
		Label:          a.Label,
		CredentialType: a.CredentialType,
		CreatedAt:      a.CreatedAt,
		UpdatedAt:      a.UpdatedAt,
	}
	switch a.CredentialType {
	case CredentialTypeAPIKey:
		var cred APIKeyCredential
		if err := json.Unmarshal([]byte(a.CredentialData), &cred); err != nil {
			return Account{}, fmt.Errorf("decode api key credential for account %d: %w", a.ID, err)
		}
		out.APIKey = &cred
	case CredentialTypeOAuth:
		var cred OAuthCredential
		if err := json.Unmarshal([]byte(a.CredentialData), &cred); err != nil {
			return Account{}, fmt.Errorf("decode oauth credential for account %d: %w", a.ID, err)
		}
		out.OAuth = &cred
	default:
		return Account{}, fmt.Errorf("account %d: unknown credential type %q", a.ID, a.CredentialType)
	}
	return out, nil
}
