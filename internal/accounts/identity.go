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
	// Some issuers pad the base64url segment; the decoder wants none.
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return ""
	}
	// Claims are read one at a time as raw JSON so an unexpected type in any
	// of them means "no identity", never a failed login.
	var claims map[string]json.RawMessage
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	var auth map[string]json.RawMessage
	if err := json.Unmarshal(claims["https://api.openai.com/auth"], &auth); err != nil {
		return ""
	}
	sub, accountID := rawString(claims["sub"]), rawString(auth["chatgpt_account_id"])
	if sub == "" || accountID == "" {
		return ""
	}
	return lengthPrefixed(sub, accountID)
}

// anthropicSubject is the Anthropic identity subject: account.uuid plus
// organization.uuid, length-prefixed (see lengthPrefixed) like the OpenAI
// subject. One person can belong to several organizations (a personal and a
// team one) under a single account.uuid, each with its own plan and
// rate-limit pool, so BOTH parts are required: account.uuid alone would let a
// login to one organization replace another's row. With either part empty
// the result is "" -- no identity.
func anthropicSubject(accountUUID, orgUUID string) string {
	if accountUUID == "" || orgUUID == "" {
		return ""
	}
	return lengthPrefixed(accountUUID, orgUUID)
}

// anthropicExchangeSubject reads account.uuid and organization.uuid from the
// token response's raw account and organization values; anything but an
// object holding a string uuid is "" for that part, and so (see
// anthropicSubject) no identity.
func anthropicExchangeSubject(account, organization json.RawMessage) string {
	return anthropicSubject(rawObjectUUID(account), rawObjectUUID(organization))
}

// rawObjectUUID returns the string uuid field of raw if raw is a JSON object
// holding one, else "".
func rawObjectUUID(raw json.RawMessage) string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	return rawString(obj["uuid"])
}

// rawString returns raw as a string if it is a JSON string, else "".
func rawString(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}
