package accounts

import (
	"encoding/json"
	"fmt"
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

// Registry is a typed accounts registry backed by a *store.Store.
type Registry struct {
	store *store.Store
}

// New returns a Registry backed by s.
func New(s *store.Store) *Registry {
	return &Registry{store: s}
}

// AddAPIKey creates a new api_key account.
func (r *Registry) AddAPIKey(provider, label, key string) (store.Account, error) {
	data, err := json.Marshal(APIKeyCredential{Key: key})
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
