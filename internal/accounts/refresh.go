package accounts

import (
	"context"
	"encoding/json"
	"fmt"
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
	config oauth2.Config
}

// NewOAuth2Refresher returns an OAuth2Refresher that exchanges refresh
// tokens against tokenURL using clientID.
func NewOAuth2Refresher(tokenURL, clientID string) *OAuth2Refresher {
	return &OAuth2Refresher{
		config: oauth2.Config{
			ClientID: clientID,
			Endpoint: oauth2.Endpoint{TokenURL: tokenURL},
		},
	}
}

// Refresh exchanges cred.RefreshToken for a new access token.
func (r *OAuth2Refresher) Refresh(ctx context.Context, cred OAuthCredential) (OAuthCredential, error) {
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
