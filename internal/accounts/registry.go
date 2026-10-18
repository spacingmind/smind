package accounts

import (
	"database/sql"
	"encoding/json"
	"errors"
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
	// Models are path.Match globs of the models this account serves
	// (empty = no list); see MatchesModel.
	Models []string
	// HasIdentity reports whether the account carries a stable vendor
	// identity (ADR-0033 D6). The identity itself is deliberately not
	// exposed on this type.
	HasIdentity bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
	APIKey      *APIKeyCredential
	OAuth       *OAuthCredential
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

	// credReplacedHook, if set via SetCredentialReplacedHook, is invoked
	// after a successful ReplaceCredential (never on the refresh path).
	credHookMu       sync.Mutex
	credReplacedHook func(accountID int64)

	// refreshMu guards refreshLocks. Entries are kept for the Registry's
	// lifetime rather than dropped when unused: account counts are small
	// and pinning avoids lock-reuse races.
	refreshMu    sync.Mutex
	refreshLocks map[int64]chan struct{}
}

// New returns a Registry backed by s.
func New(s *store.Store) *Registry {
	return &Registry{store: s, refreshLocks: make(map[int64]chan struct{})}
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

// SetCredentialReplacedHook registers fn, called with the account id after a
// successful ReplaceCredential (ADR-0028 §7: a credential change clears the
// passive cooldown, so the daemon drops its in-memory cache). It fires after
// the store call (which already deleted the persisted cooldown rows) and
// before NotifyAccountUpdated, so a subscriber reacting to the update event
// never sees a stale cooldown. The refresh path (EnsureFresh) does not call
// it. Nil-safe; a nil fn clears the hook.
func (r *Registry) SetCredentialReplacedHook(fn func(accountID int64)) {
	if r == nil {
		return
	}
	r.credHookMu.Lock()
	r.credReplacedHook = fn
	r.credHookMu.Unlock()
}

func (r *Registry) getCredentialReplacedHook() func(accountID int64) {
	if r == nil {
		return nil
	}
	r.credHookMu.Lock()
	fn := r.credReplacedHook
	r.credHookMu.Unlock()
	return fn
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

// AddOAuth creates a new oauth account with no identity.
func (r *Registry) AddOAuth(provider, label string, cred OAuthCredential) (store.Account, error) {
	return r.AddOAuthWithIdentity(provider, label, cred, "")
}

// AddOAuthWithIdentity creates a new oauth account carrying identity (see
// AccountIdentity; "" = none). Another row of the same provider owning
// identity is store.ErrIdentityInUse.
func (r *Registry) AddOAuthWithIdentity(provider, label string, cred OAuthCredential, identity string) (store.Account, error) {
	data, err := json.Marshal(cred)
	if err != nil {
		return store.Account{}, fmt.Errorf("marshal oauth credential: %w", err)
	}
	return r.store.CreateAccount(store.Account{
		Provider:       provider,
		Label:          label,
		CredentialType: CredentialTypeOAuth,
		CredentialData: string(data),
		Identity:       identity,
	})
}

// ReplaceOAuth swaps the account's credential for the oauth credential cred
// -- the re-login path (ADR-0033 D6): id, label, models and every row
// referencing the account are kept. There are three cases:
//   - sameAccount: the login's identity matched this row's, so it is the same
//     vendor account. The stored identity and the passive quota cooldown stay
//     (that account is still exhausted) and the credential-replaced hook does
//     not fire.
//   - adoptIdentity != "": a legacy row gains the identity in the same
//     transaction (another owner is store.ErrIdentityInUse and nothing
//     changes).
//   - neither: the new credential cannot be tied to the old one (a legacy
//     row, no identity either side).
//
// In the last two the row may now be a different vendor account, so the
// persisted cooldowns are deleted and the hook fires (dropping the in-memory
// cooldown), as for ReplaceCredential. NotifyAccountUpdated always fires. The
// poller's rejected-credential state is keyed on a credential fingerprint, so
// the new credential unblocks refresh by itself.
func (r *Registry) ReplaceOAuth(id int64, cred OAuthCredential, adoptIdentity string, sameAccount bool) (store.Account, error) {
	data, err := json.Marshal(cred)
	if err != nil {
		return store.Account{}, fmt.Errorf("marshal oauth credential: %w", err)
	}
	// Serialize with EnsureFresh: an in-flight refresh of the old credential
	// must not persist over this one when it finishes (and a refresh that
	// starts afterwards re-reads this credential under the same lock).
	unlock := r.lockAccount(id)
	defer unlock()
	var updated store.Account
	switch {
	case adoptIdentity != "":
		updated, err = r.store.ReplaceAccountCredentialAdoptingIdentity(id, CredentialTypeOAuth, string(data), adoptIdentity)
	case sameAccount:
		updated, err = r.store.ReplaceAccountCredentialKeepingIdentity(id, CredentialTypeOAuth, string(data))
	default:
		updated, err = r.store.ReplaceAccountCredential(id, CredentialTypeOAuth, string(data))
	}
	if err != nil {
		return store.Account{}, err
	}
	if !sameAccount {
		if fn := r.getCredentialReplacedHook(); fn != nil {
			fn(id)
		}
	}
	if n := r.getNotifier(); n != nil {
		n.NotifyAccountUpdated(updated)
	}
	return updated, nil
}

// lockAccount takes the account's refresh lock (the one EnsureFresh holds),
// blocking until it is free, and returns the unlock function.
func (r *Registry) lockAccount(id int64) (unlock func()) {
	lock := r.accountRefreshLock(id)
	lock <- struct{}{}
	return func() { <-lock }
}

// stored returns the raw store row (including its identity, which the typed
// Account never carries).
func (r *Registry) stored(id int64) (store.Account, error) {
	return r.store.GetAccount(id)
}

// findByIdentity returns the provider's account owning identity, if any.
func (r *Registry) findByIdentity(provider, identity string) (store.Account, bool, error) {
	a, err := r.store.GetAccountByIdentity(provider, identity)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Account{}, false, nil
	}
	if err != nil {
		return store.Account{}, false, err
	}
	return a, true, nil
}

// AddWithCredential creates a new account whose credential is parsed the
// account.add way (see ParseCredential) -- both account.add and
// account.updateCredential go through that one parser, so the two RPCs can
// never drift apart.
func (r *Registry) AddWithCredential(provider, label, credential, baseURL string) (store.Account, error) {
	return r.AddWithCredentialModels(provider, label, credential, baseURL, nil)
}

// AddWithCredentialModels is AddWithCredential plus a model-glob list (see
// ValidateModels; empty = no list).
func (r *Registry) AddWithCredentialModels(provider, label, credential, baseURL string, models []string) (store.Account, error) {
	credentialType, credentialData, err := r.ParseCredential(credential, baseURL)
	if err != nil {
		return store.Account{}, err
	}
	modelsJSON, err := EncodeModels(models)
	if err != nil {
		return store.Account{}, err
	}
	return r.store.CreateAccount(store.Account{
		Provider:       provider,
		Label:          label,
		CredentialType: credentialType,
		CredentialData: credentialData,
		Models:         modelsJSON,
	})
}

// SetModels replaces the account's model globs (empty clears), firing
// NotifyAccountUpdated on success. Globs are validated by ValidateModels.
func (r *Registry) SetModels(id int64, models []string) (store.Account, error) {
	modelsJSON, err := EncodeModels(models)
	if err != nil {
		return store.Account{}, err
	}
	updated, err := r.store.SetAccountModels(id, modelsJSON)
	if err != nil {
		return store.Account{}, err
	}
	if n := r.getNotifier(); n != nil {
		n.NotifyAccountUpdated(updated)
	}
	return updated, nil
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
// with the parsed credential, firing the credential-replaced hook and then
// NotifyAccountUpdated on success. It is
// account.updateCredential's registry call: parsing mirrors account.add
// exactly (see ParseCredential). The account's identity (ADR-0033 D6)
// described the old credential, so it is cleared; the OAuth re-login path
// (ReplaceOAuth) owns identity instead.
func (r *Registry) ReplaceCredential(id int64, credential, baseURL string) (store.Account, error) {
	if strings.TrimSpace(credential) == "" {
		return store.Account{}, fmt.Errorf("credential is required")
	}
	credentialType, credentialData, err := r.ParseCredential(credential, baseURL)
	if err != nil {
		return store.Account{}, err
	}
	// Same reason as ReplaceOAuth: an in-flight refresh must not overwrite it.
	unlock := r.lockAccount(id)
	defer unlock()
	updated, err := r.store.ReplaceAccountCredential(id, credentialType, credentialData)
	if err != nil {
		return store.Account{}, err
	}
	if fn := r.getCredentialReplacedHook(); fn != nil {
		fn(id)
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
		HasIdentity:    a.Identity != "",
		CreatedAt:      a.CreatedAt,
		UpdatedAt:      a.UpdatedAt,
	}
	if a.Models != "" {
		if err := json.Unmarshal([]byte(a.Models), &out.Models); err != nil {
			return Account{}, fmt.Errorf("decode models for account %d: %w", a.ID, err)
		}
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
