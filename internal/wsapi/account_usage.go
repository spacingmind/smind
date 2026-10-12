package wsapi

import (
	"github.com/spacingmind/smind/internal/quota"
)

// usageCooldown is reserved for ADR-0028 PR-F (observed rate-limit
// cooldowns). The type and the AccountUsage field exist so the wire shape is
// stable; nothing populates it yet.
type usageCooldown struct {
	Until  string `json:"until"`           // RFC3339 UTC
	Scope  string `json:"scope"`           // "account" | "model"
	Model  string `json:"model,omitempty"` // set when Scope is "model"
	Reason string `json:"reason"`
}

// AccountUsage is the wire shape of one account's usage (RPC account.usage
// and the account.usage event). It never carries a credential, an email
// beyond the user's own display label, a fingerprint or a provider body.
type AccountUsage struct {
	AccountID      int64          `json:"accountId"`
	Key            string         `json:"key"`
	Provider       string         `json:"provider"`
	DisplayName    string         `json:"displayName"`
	CredentialType string         `json:"credentialType"`
	Stale          bool           `json:"stale"`
	NextRefreshAt  *string        `json:"nextRefreshAt"`
	RefreshBlocked *string        `json:"refreshBlocked"`
	InFlight       bool           `json:"inFlight"`
	LastError      *string        `json:"lastError"`
	Report         *quota.Report  `json:"report"`
	Cooldown       *usageCooldown `json:"cooldown,omitempty"`
}
