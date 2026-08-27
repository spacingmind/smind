package accounts

import (
	"context"
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

// EnsureFresh returns the account with id, refreshing and persisting its
// OAuth credential first if it is expired or within expiryBuffer of expiry.
func (r *Registry) EnsureFresh(ctx context.Context, id int64, refresher OAuthRefresher) (Account, error) {
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

	refreshed, err := refresher.Refresh(ctx, *acc.OAuth)
	if err != nil {
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
