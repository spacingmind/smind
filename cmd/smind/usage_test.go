package main

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

func strPtr(v string) *string { return &v }
func boolPtr(v bool) *bool    { return &v }

// TestRunUsagePrintsSummaryRows drives `smind usage` end to end against a
// real wsapi.Handler-backed daemon: request-log rows seeded directly (as
// internal/server's async writer would) must surface as one aggregated
// row per account with summed tokens.
func TestRunUsagePrintsSummaryRows(t *testing.T) {
	_, s := newTestProfileDaemon(t)

	acct, err := s.CreateAccount(store.Account{
		Provider: "anthropic", Label: "a1", CredentialType: "api_key",
		CredentialData: `{"key":"sk-test"}`,
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}

	in, out := int64(100), int64(20)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for i, startedAt := range []time.Time{base, base.Add(time.Minute)} {
		if _, err := s.CreateRequestLog(store.RequestLog{
			StartedAt: startedAt, Provider: "anthropic", AccountID: &acct.ID, SessionKey: "s1",
			Model: strPtr("claude-3"), Stream: boolPtr(false), Status: 200, Outcome: store.RequestLogOutcomeOK,
			DurationMs: 5, InputTokens: &in, OutputTokens: &out,
		}); err != nil {
			t.Fatalf("CreateRequestLog(%d) error = %v", i, err)
		}
	}

	var code int
	stdout := captureStdout(t, func() { code = run([]string{"usage", "--by", "account"}) })
	if code != 0 {
		t.Fatalf("run(usage) = %d, want 0; stdout: %s", code, stdout)
	}
	key := strconv.FormatInt(acct.ID, 10)
	if !strings.Contains(stdout, key) {
		t.Fatalf("stdout = %q, want a row keyed by account %s", stdout, key)
	}
	if !strings.Contains(stdout, "200") || !strings.Contains(stdout, "40") {
		t.Fatalf("stdout = %q, want summed input=200 output=40", stdout)
	}
}

// TestRunUsageEmpty prints a friendly note rather than an empty table.
func TestRunUsageEmpty(t *testing.T) {
	newTestProfileDaemon(t)

	var code int
	stdout := captureStdout(t, func() { code = run([]string{"usage"}) })
	if code != 0 {
		t.Fatalf("run(usage) with no rows = %d, want 0; stdout: %s", code, stdout)
	}
	if !strings.Contains(stdout, "no usage recorded") {
		t.Fatalf("stdout = %q, want the empty-usage notice", stdout)
	}
}

// TestRunUsageRejectsBadGroupBy guards the --by flag's allowed values.
func TestRunUsageRejectsBadGroupBy(t *testing.T) {
	newTestProfileDaemon(t)

	var code int
	stderr := captureStderr(t, func() { code = run([]string{"usage", "--by", "bogus"}) })
	if code == 0 {
		t.Fatalf("run(usage --by bogus) = 0, want 2; stderr: %s", stderr)
	}
}
