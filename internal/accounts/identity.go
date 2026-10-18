package accounts

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// AccountIdentity is the stable, non-PII identity of a vendor account
// (ADR-0033 D6): hex SHA-256 of the length-prefixed provider and subject,
// "<len>:<provider>|<len>:<subject>". Length-prefixing makes the hash input
// delimiter-safe: no choice of provider/subject text can make two different
// pairs hash the same input (a plain "provider:subject" join would let
// ("a", "b:c") and ("a:b", "c") collide). subject is the vendor-side account
// identifier taken from the OAuth token exchange response (see
// ExchangeResult.Subject). It is hashed so no vendor id or e-mail is ever
// stored or logged. An empty subject yields "" -- "no identity" -- never an
// error: a response without a subject must not fail a login.
func AccountIdentity(provider, subject string) string {
	if subject == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(lengthPrefixed(provider, subject)))
	return hex.EncodeToString(sum[:])
}

// lengthPrefixed joins fields as "<len>:<field>|<len>:<field>|..." so the
// result parses back to exactly one field list whatever the fields contain.
func lengthPrefixed(fields ...string) string {
	var b strings.Builder
	for i, f := range fields {
		if i > 0 {
			b.WriteByte('|')
		}
		fmt.Fprintf(&b, "%d:%s", len(f), f)
	}
	return b.String()
}

// openAISubject derives the identity subject from an OpenAI id_token:
// the JWT's sub claim plus the chatgpt_account_id inside the
// "https://api.openai.com/auth" claim, length-prefixed (see lengthPrefixed)
// so the two parts cannot blur into each other. OpenAI tokens are per
// workspace, so BOTH parts are required: with either missing the identity
// would be a different, coarser thing than the one a complete login yields
// (and could wrongly merge two workspaces), so the result is "" -- no
// identity. The payload is decoded WITHOUT signature verification: the
// id_token came straight from the token endpoint over TLS, the same trust
// the access token already gets. Returns "" when the token is absent or
// malformed.
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
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Sub == "" || claims.Auth.ChatGPTAccountID == "" {
		return ""
	}
	return lengthPrefixed(claims.Sub, claims.Auth.ChatGPTAccountID)
}
