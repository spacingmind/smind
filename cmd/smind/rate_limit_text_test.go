package main

import (
	"testing"
	"time"
)

func TestRateLimitedText(t *testing.T) {
	zone := time.FixedZone("T", 7*3600)
	now := time.Date(2026, 10, 22, 15, 32, 0, 0, zone)
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	tests := []struct {
		name   string
		window string
		reset  *time.Time
		infer  bool
		want   string
	}{
		{"same day with window", "5h", at(2*time.Hour + 3*time.Minute), false, "rate limited (5h window), resets 17:35 (in 2h 3m)"},
		{"minutes only", "5h", at(45 * time.Minute), false, "rate limited (5h window), resets 16:17 (in 45m)"},
		{"no window", "", at(45 * time.Minute), false, "rate limited, resets 16:17 (in 45m)"},
		{"next day within 24h", "5h", at(10 * time.Hour), false, "rate limited (5h window), resets Fri 01:32 (in 10h 0m)"},
		{"within a week", "weekly", at(3 * 24 * time.Hour), false, "rate limited (weekly window), resets Sun 15:32 (in 72h 0m)"},
		{"beyond a week", "weekly", at(10 * 24 * time.Hour), false, "rate limited (weekly window), resets 2026-11-01 15:32 (in 240h 0m)"},
		{"inferred same day", "5h", at(2*time.Hour + 3*time.Minute), true, "rate limited (5h window), resets ≈17:35 (in ≈2h 3m)"},
		{"inferred other day", "5h", at(10 * time.Hour), true, "rate limited (5h window), resets ≈Fri 01:32 (in ≈10h 0m)"},
		{"inferred unknown reset has no marker", "5h", nil, true, "rate limited (5h window), reset time unknown"},
		{"unknown reset", "5h", nil, false, "rate limited (5h window), reset time unknown"},
		{"passed", "5h", at(-time.Minute), false, "rate limited (5h window), reset time has passed"},
		{"equal to now", "", at(0), false, "rate limited, reset time has passed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rateLimitedText(tt.window, tt.reset, tt.infer, now); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTaskRuns_StatusTextForRateLimited(t *testing.T) {
	zone := time.FixedZone("T", 7*3600)
	now := time.Date(2026, 10, 22, 15, 32, 0, 0, zone)
	reset := now.Add(2*time.Hour + 3*time.Minute)
	limited := runSummaryRow{Status: "error", Err: "429 Usage limit reached", FailureKind: "rate_limited", FailureWindow: "5h", FailureResetAt: &reset}
	if got, want := runStatusCell(limited, now), "error: rate limited (5h window), resets 17:35 (in 2h 3m)"; got != want {
		t.Errorf("rate limited cell = %q, want %q", got, want)
	}
	inferredRow := limited
	inferredRow.FailureResetInferred = true
	if got, want := runStatusCell(inferredRow, now), "error: rate limited (5h window), resets ≈17:35 (in ≈2h 3m)"; got != want {
		t.Errorf("inferred rate limited cell = %q, want %q", got, want)
	}
	if got := runStatusCell(runSummaryRow{Status: "error", Err: "boom", FailureKind: "other"}, now); got != "error: boom" {
		t.Errorf("other failure cell = %q, want the plain cause", got)
	}
	if got := runStatusCell(runSummaryRow{Status: "done", Disposition: "needs-continue"}, now); got != "done (needs-continue)" {
		t.Errorf("done cell = %q", got)
	}
}
