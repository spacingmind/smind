package accounts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"golang.org/x/oauth2"
)

// expiryBuffer is how far ahead of ExpiresAt a credential is treated as due
// for refresh, to avoid using a token that expires mid-request.
const expiryBuffer = 30 * time.Second

// OAuthRefresher exchanges a refresh token for a new access token.
type OAuthRefresher interface {
	Refresh(ctx context.Context, cred OAuthCredential) (OAuthCredential, error)
}

// OAuth2Refresher is a generic OAuthRefresher using the standard OAuth2
// refresh-token grant, parameterized by token endpoint and client ID.
type OAuth2Refresher struct {
	config     oauth2.Config
	httpClient *http.Client
}

// OAuth2RefresherOption configures an OAuth2Refresher.
type OAuth2RefresherOption func(*OAuth2Refresher)

// WithOAuth2ClientSecret sets a client secret to send alongside the client
// ID. Providers that require one (e.g. Google's OAuth2 token endpoint)
// expect it as a form parameter rather than an Authorization header, so this
// also pins the auth style to AuthStyleInParams instead of the default
// auto-detection, which would otherwise cost a wasted probe request against
// providers that reject header-style auth outright.
func WithOAuth2ClientSecret(secret string) OAuth2RefresherOption {
	return func(r *OAuth2Refresher) {
		r.config.ClientSecret = secret
		r.config.Endpoint.AuthStyle = oauth2.AuthStyleInParams
	}
}

// WithOAuth2HTTPClient overrides the HTTP client used for refresh requests.
// Intended for tests to redirect requests at a local server.
func WithOAuth2HTTPClient(client *http.Client) OAuth2RefresherOption {
	return func(r *OAuth2Refresher) {
		r.httpClient = client
	}
}

// NewOAuth2Refresher returns an OAuth2Refresher that exchanges refresh
// tokens against tokenURL using clientID.
func NewOAuth2Refresher(tokenURL, clientID string, opts ...OAuth2RefresherOption) *OAuth2Refresher {
	r := &OAuth2Refresher{
		config: oauth2.Config{
			ClientID: clientID,
			Endpoint: oauth2.Endpoint{TokenURL: tokenURL},
		},
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Refresh exchanges cred.RefreshToken for a new access token.
func (r *OAuth2Refresher) Refresh(ctx context.Context, cred OAuthCredential) (OAuthCredential, error) {
	if r.httpClient != nil {
		ctx = context.WithValue(ctx, oauth2.HTTPClient, r.httpClient)
	}
	tok, err := r.config.TokenSource(ctx, &oauth2.Token{RefreshToken: cred.RefreshToken}).Token()
	if err != nil {
		return OAuthCredential{}, fmt.Errorf("refresh oauth token: %w", err)
	}
	refreshToken := tok.RefreshToken
	if refreshToken == "" {
		refreshToken = cred.RefreshToken
	}
	return OAuthCredential{
		AccessToken:  tok.AccessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    tok.Expiry,
	}, nil
}

// accountRefreshLock returns the account's lock: a one-slot channel rather
// than a sync.Mutex so a waiter can give up when its context ends (a proxied
// request must not wait past its own cancellation behind a slow refresh).
func (r *Registry) accountRefreshLock(id int64) chan struct{} {
	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()
	if r.refreshLocks == nil {
		r.refreshLocks = make(map[int64]chan struct{})
	}
	ch, ok := r.refreshLocks[id]
	if !ok {
		ch = make(chan struct{}, 1)
		r.refreshLocks[id] = ch
	}
	return ch
}

// EnsureFresh returns the account with id, refreshing and persisting its
// OAuth credential first if it is expired or within expiryBuffer of expiry.
// Concurrent calls for the same account are serialized so the refresher
// runs at most once; the losers re-read the already-fresh credential.
func (r *Registry) EnsureFresh(ctx context.Context, id int64, refresher OAuthRefresher) (Account, error) {
	lock := r.accountRefreshLock(id)
	select {
	case lock <- struct{}{}:
	case <-ctx.Done():
		return Account{}, ctx.Err()
	}
	defer func() { <-lock }()

	acc, err := r.Get(id)
	if err != nil {
		return Account{}, err
	}
	if acc.OAuth == nil {
		return Account{}, fmt.Errorf("account %d: not an oauth account", id)
	}
	if time.Now().Add(expiryBuffer).Before(acc.OAuth.ExpiresAt) {
		return acc, nil
	}

	key := refreshKey(acc.OAuth.RefreshToken)
	if r.refreshIsDead(id, key) {
		return Account{}, fmt.Errorf("account %d: %w", id, ErrNeedsRelogin)
	}

	refreshed, err := refresher.Refresh(ctx, *acc.OAuth)
	if err != nil {
		if IsTerminalRefreshError(err) {
			r.markRefreshDead(id, key)
			return Account{}, fmt.Errorf("refresh oauth credential for account %d: %w: %w", id, ErrNeedsRelogin, err)
		}
		return Account{}, fmt.Errorf("refresh oauth credential for account %d: %w", id, err)
	}

	data, err := json.Marshal(refreshed)
	if err != nil {
		return Account{}, fmt.Errorf("marshal refreshed credential: %w", err)
	}
	updated, err := r.store.UpdateAccountCredential(id, string(data))
	if err != nil {
		return Account{}, fmt.Errorf("persist refreshed credential for account %d: %w", id, err)
	}
	return decodeAccount(updated)
}

// refreshKey identifies a refresh token without keeping it: the first 16 hex
// characters of its SHA-256. Never log the token itself.
func refreshKey(refreshToken string) string {
	sum := sha256.Sum256([]byte(refreshToken))
	return hex.EncodeToString(sum[:])[:16]
}

func (r *Registry) markRefreshDead(id int64, key string) {
	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()
	if r.deadRefresh == nil {
		r.deadRefresh = make(map[int64]string)
	}
	r.deadRefresh[id] = key
}

// refreshIsDead reports whether key is the remembered dead refresh token of
// account id. A stale entry (the credential was replaced) is pruned.
func (r *Registry) refreshIsDead(id int64, key string) bool {
	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()
	dead, ok := r.deadRefresh[id]
	if !ok {
		return false
	}
	if dead != key {
		delete(r.deadRefresh, id)
		return false
	}
	return true
}

// clearDead forgets a remembered dead refresh token of account id.
func (r *Registry) clearDead(id int64) {
	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()
	delete(r.deadRefresh, id)
}

// AccountNeedsRelogin is NeedsRelogin for an account already in hand (no
// store read).
func (r *Registry) AccountNeedsRelogin(acc Account) bool {
	if acc.OAuth == nil {
		return false
	}
	return r.refreshIsDead(acc.ID, refreshKey(acc.OAuth.RefreshToken))
}

// NeedsRelogin reports whether the account's current refresh token is the one
// the provider refused for good. False for an unknown or api_key account.
func (r *Registry) NeedsRelogin(id int64) bool {
	acc, err := r.Get(id)
	if err != nil || acc.OAuth == nil {
		return false
	}
	return r.refreshIsDead(id, refreshKey(acc.OAuth.RefreshToken))
}
