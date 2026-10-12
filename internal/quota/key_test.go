package quota

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
)

func TestAccountKeyStable(t *testing.T) {
	t.Parallel()

	base := accounts.Account{
		ID:        3,
		Provider:  "anthropic",
		Label:     "me@example.com",
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	renamed := base
	renamed.Label = "Work"
	renamed.UpdatedAt = base.CreatedAt.Add(time.Hour)
	apiKey := renamed
	apiKey.CredentialType = "api_key"
	apiKey.APIKey = &accounts.APIKeyCredential{Key: "sk-test-token-abc"}
	oauth := renamed
	oauth.CredentialType = "oauth"
	oauth.OAuth = &accounts.OAuthCredential{AccessToken: "test-token-abc", RefreshToken: "test-refresh-abc"}

	want := AccountKey(base)
	for _, tt := range []struct {
		name string
		a    accounts.Account
	}{
		{"base", base},
		{"renamed", renamed},
		{"api key", apiKey},
		{"oauth swap", oauth},
	} {
		if got := AccountKey(tt.a); got != want {
			t.Errorf("AccountKey(%s) = %q, want %q", tt.name, got, want)
		}
	}

	if got := AccountKey(accounts.Account{ID: 3, Provider: "anthropic"}); got != "anthropic-3" {
		t.Errorf("anthropic-3: got %q", got)
	}
	if got := AccountKey(accounts.Account{ID: 12, Provider: "openai"}); got != "openai-12" {
		t.Errorf("openai-12: got %q", got)
	}
}

func TestAccountKeyFormat(t *testing.T) {
	t.Parallel()

	re := regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)
	providers := []string{
		"anthropic",
		"openai",
		"pro vider",
		"prôvider",
		"pro@vider",
		"pro/vider",
		"",
		string(make([]byte, 0)),
	}
	// 300-char hostile provider name.
	long := make([]byte, 300)
	for i := range long {
		long[i] = 'x'
	}
	providers = append(providers, string(long), string(make([]rune, 300)))

	for _, p := range providers {
		got := AccountKey(accounts.Account{ID: 7, Provider: p})
		if !re.MatchString(got) {
			t.Errorf("AccountKey(provider=%d chars) = %q, want match %v", len(p), got, re)
		}
	}
}

func TestAccountKeyNeverContainsLabel(t *testing.T) {
	t.Parallel()

	email := "me@example.com"
	key := AccountKey(accounts.Account{ID: 5, Provider: "anthropic", Label: email})
	if strings.Contains(key, email) {
		t.Errorf("key %q contains label %q", key, email)
	}

	// A provider that is itself email-like must be sanitized too.
	key = AccountKey(accounts.Account{ID: 5, Provider: email})
	if strings.Contains(key, email) || strings.Contains(key, "@") {
		t.Errorf("key %q leaks email-like provider", key)
	}
}

func TestDisplayNameUsesLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		label string
		want  string
	}{
		{"plain", "Work Max", "Work Max"},
		{"trimmed", "  Work Max  ", "Work Max"},
		{"email kept", "me@example.com", "me@example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := accounts.Account{Provider: "anthropic", Label: tt.label}
			if got := DisplayName(a); got != tt.want {
				t.Errorf("DisplayName(%q) = %q, want %q", tt.label, got, tt.want)
			}
		})
	}
}

func TestDisplayNameEmptyLabelFallsBackToProviderDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		provider string
		label    string
		want     string
	}{
		{"anthropic", "", "Claude"},
		{"anthropic", "   ", "Claude"},
		{"openai", "", "Codex"},
		{"openai", "   ", "Codex"},
	}
	for _, tt := range tests {
		a := accounts.Account{Provider: tt.provider, Label: tt.label}
		if got := DisplayName(a); got != tt.want {
			t.Errorf("DisplayName(%q, %q) = %q, want %q", tt.provider, tt.label, got, tt.want)
		}
	}
}

func TestDisplayNameProviderDefaultLabelFallsBack(t *testing.T) {
	t.Parallel()

	tests := []struct {
		label string
		want  string
	}{
		{"anthropic", "Claude"},
		{"Claude", "Claude"},
		{"OPENAI", "Claude"},
		{"codex", "Claude"},
		{"default", "Claude"},
		{" Default ", "Claude"},
		{"Claude work", "Claude work"},
		{"claude-2", "claude-2"},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			t.Parallel()
			a := accounts.Account{Provider: "anthropic", Label: tt.label}
			if got := DisplayName(a); got != tt.want {
				t.Errorf("DisplayName(label=%q) = %q, want %q", tt.label, got, tt.want)
			}
		})
	}
}

func TestDisplayNameUnknownProviderFallsBackToProviderIDTitlecased(t *testing.T) {
	t.Parallel()

	if got := DisplayName(accounts.Account{Provider: "kimi"}); got != "Kimi" {
		t.Errorf("DisplayName(kimi) = %q, want %q", got, "Kimi")
	}
	if got := DisplayName(accounts.Account{}); got != "Account" {
		t.Errorf("DisplayName(empty) = %q, want %q", got, "Account")
	}
}
