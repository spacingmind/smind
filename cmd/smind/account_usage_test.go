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

// A refused refresh marks every account stale, but "showing the last good
// report" is only true for an account that has one.
func TestAccountUsageStaleHintOnlyForAvailableReports(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	res := &accountUsageResponse{Enabled: true, Reports: []accountUsageEntry{{
		DisplayName: "Claude", Provider: "anthropic", Stale: true, RefreshBlocked: ptrString("rejected"),
		Report: &quota.Report{Status: quota.StatusUnavailable, Problem: &quota.Problem{Kind: quota.ProblemExpired}},
	}}}
	var buf bytes.Buffer
	renderAccountUsage(&buf, res, true, now)
	if strings.Contains(buf.String(), "last good report") {
		t.Fatalf("stale hint printed for an unavailable report:\n%s", buf.String())
	}
}

func TestAccountUsageStripsControlCharacters(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	res := &accountUsageResponse{Enabled: true, Reports: []accountUsageEntry{{
		DisplayName: "Work\x1b[2J", Provider: "anthropic",
		Report: &quota.Report{Status: quota.StatusAvailable, PlanLabel: "pro\nFAKE",
			Windows: []quota.Window{{ID: "w", Label: "Ses\x1b[31msion", UsedPct: ptrFloat(10)}}},
	}}}
	var buf bytes.Buffer
	renderAccountUsage(&buf, res, false, now)
	if strings.ContainsRune(buf.String(), 0x1b) || strings.Contains(buf.String(), "pro\nFAKE") {
		t.Fatalf("control characters reached the output: %q", buf.String())
	}
}

// cooldownRender renders one account row against usageNow with the local
// zone pinned to UTC+2, so "local" clock output is deterministic.
func cooldownRender(t *testing.T, e accountUsageEntry) string {
	t.Helper()
	prev := time.Local
	time.Local = time.FixedZone("test", 2*3600)
	t.Cleanup(func() { time.Local = prev })
	e.Provider = "anthropic"
	e.DisplayName = "Work"
	var out bytes.Buffer
	renderAccountUsage(&out, &accountUsageResponse{Enabled: true, Reports: []accountUsageEntry{e}}, false, usageNow)
	return out.String()
}

func TestAccountUsageCLIShowsCooldownUntilAndWindow(t *testing.T) {
	// usageNow 12:00 UTC = 14:00 local; 13:30 UTC = 15:30 local, same day.
	got := cooldownRender(t, accountUsageEntry{Cooldown: &accountUsageCooldown{
		Until: usageNow.Add(90 * time.Minute), Scope: "account", Reason: "rejected", Window: "five_hour",
	}})
	if !strings.Contains(got, "Work (anthropic)\n  cooling down until 15:30 (5-hour window rejected)\n") {
		t.Fatalf("got:\n%s", got)
	}
	got = cooldownRender(t, accountUsageEntry{Cooldown: &accountUsageCooldown{
		Until: usageNow.Add(48 * time.Hour), Scope: "account", Reason: "rejected", Window: "weekly",
	}})
	if !strings.Contains(got, "cooling down until Oct 14 14:00 (weekly window rejected)") {
		t.Fatalf("got:\n%s", got)
	}
	got = cooldownRender(t, accountUsageEntry{Cooldown: &accountUsageCooldown{
		Until: usageNow.Add(time.Hour), Scope: "account", Reason: "rejected", Window: "unified",
	}})
	if !strings.Contains(got, "(rate limit rejected)") {
		t.Fatalf("got:\n%s", got)
	}
	// Crossing local midnight: 23:00 UTC is 01:00 local the next day.
	got = cooldownRender(t, accountUsageEntry{Cooldown: &accountUsageCooldown{
		Until: usageNow.Add(11 * time.Hour), Scope: "account", Reason: "rejected",
	}})
	if !strings.Contains(got, "until Oct 13 01:00 (rate limit rejected)") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestAccountUsageCLICooldownModelScopeAndFallbackWording(t *testing.T) {
	until := usageNow.Add(time.Hour)
	got := cooldownRender(t, accountUsageEntry{Cooldown: &accountUsageCooldown{
		Until: until, Scope: "model", Model: "claude-opus-4", Reason: "rejected", Window: "weekly_model",
	}})
	if !strings.Contains(got, "(weekly limit for claude-opus-4 rejected)") || strings.Contains(got, "for claude-opus-4 for") {
		t.Fatalf("got:\n%s", got)
	}
	got = cooldownRender(t, accountUsageEntry{Cooldown: &accountUsageCooldown{
		Until: until, Scope: "model", Model: "claude-opus-4", Reason: "rejected", Window: "five_hour",
	}})
	if !strings.Contains(got, "(5-hour window for claude-opus-4 rejected)") {
		t.Fatalf("got:\n%s", got)
	}
	got = cooldownRender(t, accountUsageEntry{Cooldown: &accountUsageCooldown{
		Until: until, Scope: "account", Reason: "fallback",
	}})
	if !strings.Contains(got, "(rate limit rejected, reset time unknown)") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestAccountUsageCLIExpiredCooldownPrintsNothing(t *testing.T) {
	got := cooldownRender(t, accountUsageEntry{Cooldown: &accountUsageCooldown{
		Until: usageNow.Add(-time.Minute), Scope: "account", Reason: "rejected", Window: "five_hour",
	}})
	if strings.Contains(got, "cooling down") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestAccountUsageCLICooldownTextIsSanitised(t *testing.T) {
	got := cooldownRender(t, accountUsageEntry{Cooldown: &accountUsageCooldown{
		Until: usageNow.Add(time.Hour), Scope: "model", Model: "opus\x1b[31m\nEVIL", Reason: "rejected", Window: "weekly_model",
	}})
	if strings.ContainsAny(got, "\x1b") || strings.Contains(got, "\nEVIL") || !strings.Contains(got, "opus[31mEVIL") {
		t.Fatalf("got %q", got)
	}
}

func TestAccountUsageCLIObservedWindowMarkedFromRecentTraffic(t *testing.T) {
	got := cooldownRender(t, accountUsageEntry{Report: &quota.Report{
		Version: 2, Status: quota.StatusAvailable,
		Windows: []quota.Window{
			{ID: "five_hour", Label: "Session", UsedPct: ptrFloat(80), Source: "observed"},
			{ID: "weekly", Label: "Week", Source: "observed", Status: "rejected"},
			{ID: "weekly_x", Label: "Other", Source: "fetched", Status: "allowed_warning"},
		},
	}})
	for _, want := range []string{
		"Session  80% used · from recent traffic",
		"Week     rejected · from recent traffic",
		"Other    allowed_warning\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q;\ngot:\n%s", want, got)
		}
	}
}

func TestAccountUsageCLIJSONPassthroughIncludesCooldown(t *testing.T) {
	raw := `{"enabled":true,"ttlSeconds":300,"reports":[{"accountId":1,"key":"k","cooldown":{"until":"2026-10-12T13:00:00Z","scope":"model","model":"m","reason":"fallback","future":1}}]}`
	dial := func(context.Context) (wsCaller, error) {
		return func(_ context.Context, _ string, _ any, out any) error {
			return json.Unmarshal([]byte(raw), out)
		}, nil
	}
	var out bytes.Buffer
	if code := runAccountUsage(context.Background(), dial, false, true, usageNow, &out, &out); code != 0 {
		t.Fatalf("code = %d", code)
	}
	for _, want := range []string{`"cooldown"`, `"until": "2026-10-12T13:00:00Z"`, `"future": 1`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %s in %s", want, out.String())
		}
	}
}

func TestAccountUsageCLIMalformedCooldownUntilDoesNotFailCommand(t *testing.T) {
	raw := `{"enabled":true,"ttlSeconds":300,"reports":[` +
		`{"accountId":1,"key":"a","provider":"anthropic","displayName":"Bad","cooldown":{"until":"not a time","scope":"account","reason":"rejected","window":"five_hour"}},` +
		`{"accountId":2,"key":"b","provider":"anthropic","displayName":"NumUntil","cooldown":{"until":12345,"scope":"account","reason":"rejected"}},` +
		`{"accountId":3,"key":"c","provider":"anthropic","displayName":"Good","cooldown":{"until":"2026-10-12T13:30:00Z","scope":"account","reason":"rejected","window":"weekly"}}]}`
	dial := func(context.Context) (wsCaller, error) {
		return func(_ context.Context, _ string, _ any, out any) error {
			return json.Unmarshal([]byte(raw), out)
		}, nil
	}
	var out, errOut bytes.Buffer
	if code := runAccountUsage(context.Background(), dial, false, false, usageNow, &out, &errOut); code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, errOut.String())
	}
	got := out.String()
	for _, name := range []string{"Bad (anthropic)", "NumUntil (anthropic)", "Good (anthropic)"} {
		if !strings.Contains(got, name) {
			t.Errorf("row %q missing from:\n%s", name, got)
		}
	}
	if n := strings.Count(got, "cooling down until"); n != 1 || !strings.Contains(got, "(weekly window rejected)") {
		t.Errorf("want only the valid cooldown line, got %d in:\n%s", n, got)
	}
}
