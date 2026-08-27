// Package accounts is a typed layer over internal/store's account rows,
// marshaling structured credentials to and from Account.CredentialData.
package accounts

import "time"

// Credential type discriminators, stored as store.Account.CredentialType.
const (
	CredentialTypeAPIKey = "api_key"
	CredentialTypeOAuth  = "oauth"
)

// APIKeyCredential is a static bearer credential.
type APIKeyCredential struct {
	Key string `json:"key"`
}

// OAuthCredential is a refreshable OAuth2 credential.
type OAuthCredential struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}
