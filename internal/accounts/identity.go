package accounts

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// AccountIdentity is the stable, non-PII identity of a vendor account
// (ADR-0033 D6): hex SHA-256 of "<provider>:<subject>". subject is the
// vendor-side account identifier taken from the OAuth token exchange
// response (see ExchangeResult.Subject). It is hashed so no vendor id or
// e-mail is ever stored or logged. An empty subject yields "" -- "no
// identity" -- never an error: a response without a subject must not fail a
// login.
func AccountIdentity(provider, subject string) string {
	if subject == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(provider + ":" + subject))
	return hex.EncodeToString(sum[:])
}

// openAISubject derives the identity subject from an OpenAI id_token:
// the JWT's sub claim plus the chatgpt_account_id inside the
// "https://api.openai.com/auth" claim ("sub|chatgpt_account_id").
// OpenAI tokens are per workspace, so both parts matter. The payload is
// decoded WITHOUT signature verification: the id_token came straight from
// the token endpoint over TLS, the same trust the access token already
// gets. Returns "" when the token is absent, malformed, or lacks sub.
func openAISubject(idToken string) string {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Sub  string `json:"sub"`
		Auth struct {
			ChatGPTAccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Sub == "" {
		return ""
	}
	return claims.Sub + "|" + claims.Auth.ChatGPTAccountID
}
