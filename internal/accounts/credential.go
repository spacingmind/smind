// Package accounts is a typed layer over internal/store's account rows,
// marshaling structured credentials to and from Account.CredentialData.
package accounts

import (
	"encoding/json"
	"fmt"
	"time"
)

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

// epochMillisThreshold distinguishes epoch seconds from epoch milliseconds
// in a numeric expires_at: a seconds value for any remotely plausible date
// (past or future) stays well under this, while a millis value comfortably
// exceeds it. 1e12 seconds is the year 33658; 1e12 millis is 2001-09-09, so
// any real-world timestamp lands unambiguously on one side of it.
const epochMillisThreshold = 1e12

// UnmarshalJSON accepts expires_at as an RFC3339 string (this package's own
// wire format, see MarshalJSON's default behavior) or as a JSON number,
// which upstream OAuth providers such as Claude Code emit as a Unix epoch
// timestamp -- in milliseconds or, less commonly, in seconds. Without this,
// json.Unmarshal fails on numeric expires_at (time.Time only decodes from a
// JSON string), which previously caused wsapi's account.add handler to fall
// back to treating an OAuth credential as a plain API key.
func (c *OAuthCredential) UnmarshalJSON(data []byte) error {
	var raw struct {
		AccessToken  string          `json:"access_token"`
		RefreshToken string          `json:"refresh_token"`
		ExpiresAt    json.RawMessage `json:"expires_at"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	c.AccessToken = raw.AccessToken
	c.RefreshToken = raw.RefreshToken
	c.ExpiresAt = time.Time{}

	if len(raw.ExpiresAt) == 0 || string(raw.ExpiresAt) == "null" {
		return nil
	}

	var asString string
	if err := json.Unmarshal(raw.ExpiresAt, &asString); err == nil {
		t, err := time.Parse(time.RFC3339, asString)
		if err != nil {
			return fmt.Errorf("parse expires_at %q as RFC3339: %w", asString, err)
		}
		c.ExpiresAt = t
		return nil
	}

	var asNumber float64
	if err := json.Unmarshal(raw.ExpiresAt, &asNumber); err == nil {
		if asNumber > epochMillisThreshold {
			c.ExpiresAt = time.UnixMilli(int64(asNumber)).UTC()
		} else {
			c.ExpiresAt = time.Unix(int64(asNumber), 0).UTC()
		}
		return nil
	}

	return fmt.Errorf("expires_at must be an RFC3339 string or a Unix epoch number, got %s", raw.ExpiresAt)
}
