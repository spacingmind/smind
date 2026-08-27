// Package quota polls and caches per-account LLM provider usage, backed by
// internal/store's quota_snapshots table.
package quota

import (
	"context"
	"encoding/json"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

// Usage is a provider-agnostic snapshot of an account's quota usage.
type Usage struct {
	TokensUsed  int64     `json:"tokens_used"`
	TokensLimit int64     `json:"tokens_limit"`
	ResetsAt    time.Time `json:"resets_at"`
}

// Marshal encodes u as JSON, for storage in QuotaSnapshot.UsageData.
func (u Usage) Marshal() (string, error) {
	b, err := json.Marshal(u)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Unmarshal decodes usage JSON as previously produced by Marshal.
func Unmarshal(data string) (Usage, error) {
	var u Usage
	if err := json.Unmarshal([]byte(data), &u); err != nil {
		return Usage{}, err
	}
	return u, nil
}

// Fetcher retrieves current usage for an account from its provider's API.
type Fetcher interface {
	Fetch(ctx context.Context, account store.Account) (Usage, error)
}
