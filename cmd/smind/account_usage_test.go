package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/quota"
)

// usageNow is the frozen clock for golden tests: 2026-10-12 12:00:00 UTC.
var usageNow = time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

// fakeUsageDial returns a dial func whose fake `account.usage` handler
// records the params it was called with and replies with result.
func fakeUsageDial(t *testing.T, result *accountUsageResponse, callErr error) (dial func(context.Context) (wsCaller, error), gotParams *map[string]any) {
	t.Helper()
	gotParams = &map[string]any{}
	return func(ctx context.Context) (wsCaller, error) {
		return func(_ context.Context, method string, params any, out any) error {
			*gotParams = params.(map[string]any)
			if callErr != nil {
				return callErr
			}
			b, err := json.Marshal(result)
			if err != nil {
				t.Fatalf("marshal fake result: %v", err)
			}
			if err := json.Unmarshal(b, out); err != nil {
				t.Fatalf("unmarshal fake result: %v", err)
			}
			return nil
		}, nil
	}, gotParams
}

func TestAccountUsageDefaultIsTouchNotRefresh(t *testing.T) {
	dial, got := fakeUsageDial(t, &accountUsageResponse{Enabled: true, Reports: []accountUsageEntry{}}, nil)
	var out bytes.Buffer
	if code := runAccountUsage(context.Background(), dial, false, false, time.Now(), &out, &out); code != 0 {
		t.Fatalf("code = %d, want 0; out: %s", code, out.String())
	}
	if m := (*got)["mode"]; m != "touch" {
		t.Fatalf("params mode = %v, want \"touch\"", m)
	}
}

func TestAccountUsageRefreshFlag(t *testing.T) {
	dial, got := fakeUsageDial(t, &accountUsageResponse{Enabled: true, Reports: []accountUsageEntry{}}, nil)
	var out bytes.Buffer
	if code := runAccountUsage(context.Background(), dial, true, false, time.Now(), &out, &out); code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if m := (*got)["mode"]; m != "refresh" {
		t.Fatalf("params mode = %v, want \"refresh\"", m)
	}
}

func TestAccountUsageJSON(t *testing.T) {
	fetched := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	dial, _ := fakeUsageDial(t, &accountUsageResponse{
		Enabled: true, TTLSeconds: 300,
		Reports: []accountUsageEntry{{
			AccountID: 1, Key: "anthropic-1", Provider: "anthropic", DisplayName: "Work",
			CredentialType: "oauth", Report: &quota.Report{Version: 2, Status: quota.StatusAvailable, FetchedAt: fetched},
		}},
	}, nil)
	var out bytes.Buffer
	if code := runAccountUsage(context.Background(), dial, false, true, time.Now(), &out, &out); code != 0 {
		t.Fatalf("code = %d, want 0; out: %s", code, out.String())
	}
	var back accountUsageResponse
	if err := json.Unmarshal(out.Bytes(), &back); err != nil {
		t.Fatalf("stdout is not the daemon JSON: %v\n%s", err, out.String())
	}
	if !back.Enabled || len(back.Reports) != 1 || back.Reports[0].Key != "anthropic-1" || back.Reports[0].Report == nil {
		t.Fatalf("json = %s, want enabled response with one report", out.String())
	}
	for _, secret := range []string{"credentialData", "credential_data", "fingerprint", "apiKey", "api_key"} {
		if strings.Contains(out.String(), secret) {
			t.Fatalf("json leaks %q: %s", secret, out.String())
		}
	}
}

func TestAccountUsageDaemonDownExitCode(t *testing.T) {
	dial := func(ctx context.Context) (wsCaller, error) {
		return nil, errors.New("connect to smind daemon at 127.0.0.1:4648 (is `smind serve` running?): refused")
	}
	var stdout, stderr bytes.Buffer
	if code := runAccountUsage(context.Background(), dial, false, false, time.Now(), &stdout, &stderr); code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "smind serve") {
		t.Fatalf("stderr = %q, want the daemon-down hint", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want nothing", stdout.String())
	}
}

func TestAccountUsageUnknownFlagExits2(t *testing.T) {
	previous := osStderr
	osStderr = &bytes.Buffer{}
	defer func() { osStderr = previous }()
	if code := cmdAccountUsage([]string{"--bogus"}); code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
}

func TestAccountUsageNoAccounts(t *testing.T) {
	dial, _ := fakeUsageDial(t, &accountUsageResponse{Enabled: true, Reports: []accountUsageEntry{}}, nil)
	var out bytes.Buffer
	if code := runAccountUsage(context.Background(), dial, false, false, usageNow, &out, &out); code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if strings.TrimSpace(out.String()) != "no accounts" {
		t.Fatalf("stdout = %q, want %q", out.String(), "no accounts")
	}
}

func TestAccountUsageDisabled(t *testing.T) {
	dial, _ := fakeUsageDial(t, &accountUsageResponse{Enabled: false, TTLSeconds: 300}, nil)
	var out bytes.Buffer
	if code := runAccountUsage(context.Background(), dial, false, false, usageNow, &out, &out); code != 0 {
		t.Fatalf("code = %d, want 0 (disabled is data, not an error)", code)
	}
	if !strings.Contains(out.String(), "account usage is disabled") {
		t.Fatalf("stdout = %q, want the disabled notice", out.String())
	}
}

func ptrFloat(f float64) *float64    { return &f }
func ptrString(s string) *string     { return &s }
func ptrTime(t time.Time) *time.Time { return &t }

// TestAccountUsageTextOutput is the golden text-render test: window lines
// with aligned labels, pct rounding, the reset countdown, unavailable
// wording for expired/rejected/no_quota, the stale hint, balances, and
// the updated-ago line -- all against the frozen usageNow clock.
func TestAccountUsageTextOutput(t *testing.T) {
	resetAt := usageNow.Add(2*time.Hour + 4*time.Minute)
	fetchedAt := usageNow.Add(-3 * time.Minute)
	used := 41.4
	dial, _ := fakeUsageDial(t, &accountUsageResponse{
		Enabled: true, TTLSeconds: 300,
		Reports: []accountUsageEntry{
			{
				AccountID: 1, Key: "anthropic-1", Provider: "anthropic", DisplayName: "Work",
				CredentialType: "oauth", Stale: true,
				Report: &quota.Report{
					Version: 2, Status: quota.StatusAvailable, PlanLabel: "pro", FetchedAt: fetchedAt,
					Windows: []quota.Window{
						{ID: "five_hour", Label: "Session", UsedPct: &used, RemainingPct: ptrFloat(58.6), ResetsAt: &resetAt},
						{ID: "weekly", Label: "Week", ResetsAt: ptrTime(usageNow.Add(3*24*time.Hour + 5*time.Hour))},
					},
					Balances: []quota.Balance{
						{ID: "credits", Label: "Credits", Remaining: ptrFloat(12.5), Unit: "credits"},
					},
				},
			},
			{
				AccountID: 2, Key: "anthropic-2", Provider: "anthropic", DisplayName: "Old login",
				Report: &quota.Report{Version: 2, Status: quota.StatusUnavailable,
					Problem: &quota.Problem{Kind: quota.ProblemExpired}},
			},
			{
				AccountID: 3, Key: "openai-3", Provider: "openai", DisplayName: "Banned",
				Report: &quota.Report{Version: 2, Status: quota.StatusUnavailable,
					Problem: &quota.Problem{Kind: quota.ProblemRejected, Status: 403}},
			},
			{
				AccountID: 4, Key: "glm-4", Provider: "glm", DisplayName: "Key",
				Report: &quota.Report{Version: 2, Status: quota.StatusUnavailable,
					Problem: &quota.Problem{Kind: quota.ProblemNoQuota}},
			},
		},
	}, nil)
	var out bytes.Buffer
	if code := runAccountUsage(context.Background(), dial, false, false, usageNow, &out, &out); code != 0 {
		t.Fatalf("code = %d, want 0; out: %s", code, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"Work (anthropic · pro)",
		"  Session  41% used · resets in 2h 4m",
		"  Week     — · resets in 3d 5h",
		"  Credits: 12.5 credits",
		"  updated 3m ago",
		"  (stale: showing the last good report)",
		"Old login (anthropic)",
		"  login expired — sign in again",
		"  login rejected by the provider",
		"  no plan windows (API-key account)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q;\ngot:\n%s", want, got)
		}
	}
}

// TestAccountUsageRefreshFlagBlockedPrintsReasonAndNextTime: with
// --refresh and refreshBlocked set, the text output names the reason and
// when a forced refresh will be allowed again (local clock + countdown);
// "rejected" instead says the login must be replaced.
func TestAccountUsageRefreshFlagBlockedPrintsReasonAndNextTime(t *testing.T) {
	next := usageNow.Add(2 * time.Minute).Format(time.RFC3339)
	nextStr := next
	for _, tc := range []struct {
		name        string
		blocked     string
		nextRefresh *string
		want        string
		notWant     string
	}{
		{"floor", "floor", &nextStr, "refresh not available (floor) — next at", ""},
		{"budget", "budget", &nextStr, "refresh not available (budget) — next at", ""},
		{"backoff", "backoff", &nextStr, "/ in 2m", ""},
		{"rejected", "rejected", nil, "refresh not available (rejected) — until the login is replaced", "next at"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dial, _ := fakeUsageDial(t, &accountUsageResponse{
				Enabled: true,
				Reports: []accountUsageEntry{{
					AccountID: 1, Key: "anthropic-1", Provider: "anthropic", DisplayName: "Work",
					RefreshBlocked: &tc.blocked, NextRefreshAt: tc.nextRefresh,
					Report: &quota.Report{Version: 2, Status: quota.StatusAvailable, FetchedAt: usageNow},
				}},
			}, nil)
			var out bytes.Buffer
			if code := runAccountUsage(context.Background(), dial, true, false, usageNow, &out, &out); code != 0 {
				t.Fatalf("code = %d, want 0", code)
			}
			got := out.String()
			if !strings.Contains(got, tc.want) {
				t.Errorf("output missing %q;\ngot:\n%s", tc.want, got)
			}
			if tc.notWant != "" && strings.Contains(got, tc.notWant) {
				t.Errorf("output must not contain %q;\ngot:\n%s", tc.notWant, got)
			}
		})
	}
}

// TestAccountUsageInFlightAndErrorStatus covers the remaining wording:
// inFlight => "still refreshing…", status error => couldn't fetch usage.
func TestAccountUsageInFlightAndErrorStatus(t *testing.T) {
	errStr := "timeout"
	dial, _ := fakeUsageDial(t, &accountUsageResponse{
		Enabled: true,
		Reports: []accountUsageEntry{
			{AccountID: 1, Key: "anthropic-1", Provider: "anthropic", DisplayName: "Work", InFlight: true,
				Report: &quota.Report{Version: 2, Status: quota.StatusAvailable, FetchedAt: usageNow}},
			{AccountID: 2, Key: "anthropic-2", Provider: "anthropic", DisplayName: "Flaky",
				LastError: &errStr,
				Report:    &quota.Report{Version: 2, Status: quota.StatusError, FetchedAt: usageNow}},
		},
	}, nil)
	var out bytes.Buffer
	if code := runAccountUsage(context.Background(), dial, false, false, usageNow, &out, &out); code != 0 {
		t.Fatalf("code = %d, want 0 (unavailable/error accounts are data)", code)
	}
	got := out.String()
	if !strings.Contains(got, "still refreshing…") {
		t.Errorf("output missing inFlight line;\ngot:\n%s", got)
	}
	if !strings.Contains(got, "couldn't fetch usage (timeout)") {
		t.Errorf("output missing error-status line;\ngot:\n%s", got)
	}
}

// TestFormatCountdown pins the countdown formats from the brief.
func TestFormatCountdown(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{45 * time.Second, "45s"},
		{44 * time.Second, "44s"},
		{2*time.Hour + 4*time.Minute, "2h 4m"},
		{3*24*time.Hour + 5*time.Hour, "3d 5h"},
		{2 * time.Minute, "2m"},
		{150 * time.Millisecond, "<1m"},
		{5 * time.Hour, "5h"},
	} {
		if got := formatCountdown(tc.d); got != tc.want {
			t.Errorf("formatCountdown(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}
