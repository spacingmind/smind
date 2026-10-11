package quota

import (
	"fmt"
	"strings"

	"github.com/spacingmind/smind/internal/accounts"
)

const maxAccountKeyLen = 128

// AccountKey is the stable identity of an account in usage reports and UI
// pins: "<provider>-<id>". It is derived only from the provider and the row
// id, never from the label (which may be an email), a token or a JWT claim,
// so it survives renames, credential swaps and token refresh.
func AccountKey(a accounts.Account) string {
	var b strings.Builder
	for _, r := range a.Provider {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	suffix := fmt.Sprintf("-%d", a.ID)
	prov := b.String()
	if max := maxAccountKeyLen - len(suffix); len(prov) > max {
		prov = prov[:max]
	}
	if prov == "" {
		prov = "account"
	}
	return prov + suffix
}

// providerDefaultNames maps a provider id to the name shown when the account
// has no user-set label.
var providerDefaultNames = map[string]string{
	"anthropic": "Claude",
	"openai":    "Codex",
}

// DisplayName is the one rule every surface (web, CLI, MCP) uses to name an
// account: the trimmed user-set label. An empty label, or one equal to a
// provider default (anthropic, openai, claude, codex, default; any case),
// falls back to the provider's default name ("Claude", "Codex"; any other
// provider is its id, title-cased). The label is used as written even when it
// is an email: it is the user's own text. Callers must not log it or put it
// in an account key.
func DisplayName(a accounts.Account) string {
	label := strings.TrimSpace(a.Label)
	switch strings.ToLower(label) {
	case "", "anthropic", "openai", "claude", "codex", "default":
		return providerDefaultName(a.Provider)
	}
	return label
}

func providerDefaultName(provider string) string {
	if n, ok := providerDefaultNames[provider]; ok {
		return n
	}
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return "Account"
	}
	return strings.ToUpper(provider[:1]) + provider[1:]
}
