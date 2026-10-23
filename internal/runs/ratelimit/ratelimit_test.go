package ratelimit

import (
	"testing"
	"time"
)

var ict = time.FixedZone("ICT", 7*3600)

func at(y int, mo time.Month, d, h, m, s int) time.Time {
	return time.Date(y, mo, d, h, m, s, 0, ict)
}

func wantReset(t *testing.T, got *time.Time, want time.Time) {
	t.Helper()
	if got == nil {
		t.Fatalf("ResetAt = nil, want %v", want)
	}
	if !got.Equal(want) {
		t.Fatalf("ResetAt = %v, want %v", got, want)
	}
	if got.Location() != time.UTC {
		t.Fatalf("ResetAt location = %v, want UTC", got.Location())
	}
}

const glmText = "429 Usage limit reached for 5 hour. Your limit will reset at 17:35:18"

func TestClassify_GLM429FiveHourFixture(t *testing.T) {
	cases := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		// The text is Asia/Shanghai (UTC+8) wall time: 17:35:18 CST is
		// 16:35:18 in the ICT (+7) zone these tests use.
		{"same day", at(2026, 10, 22, 15, 10, 0), at(2026, 10, 22, 16, 35, 18)},
		{"next day", at(2026, 10, 22, 18, 0, 0), at(2026, 10, 23, 16, 35, 18)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Classify(Input{Provider: "glm", Err: glmText}, c.now)
			if r.Kind != KindRateLimited || r.Window != "5h" {
				t.Fatalf("got %+v", r)
			}
			wantReset(t, r.ResetAt, c.want)
		})
	}
}

func TestClassify_GLMWrappedErrorStillMatches(t *testing.T) {
	e := glmText + ` (taskrunner: glm prompt: acp: rpc error -32603: Internal error ({"details":"` + glmText + `"}))`
	r := Classify(Input{Provider: "glm", Err: e}, at(2026, 10, 22, 15, 10, 0))
	if r.Kind != KindRateLimited || r.Window != "5h" {
		t.Fatalf("got %+v", r)
	}
	wantReset(t, r.ResetAt, at(2026, 10, 22, 16, 35, 18))
}

func TestClassify_UnparseableResetKeepsKindNilReset(t *testing.T) {
	r := Classify(Input{Err: "429 Usage limit reached for 5 hour. Your limit will reset at soon"}, at(2026, 10, 22, 15, 0, 0))
	if r.Kind != KindRateLimited || r.ResetAt != nil {
		t.Fatalf("got %+v", r)
	}
}

func TestClassify_ClaudeSignalRejected(t *testing.T) {
	x := time.Unix(1790000000, 0)
	r := Classify(Input{
		Provider: "claude-native",
		Err:      "claude code prompt: exit status 1",
		Signal:   &Signal{Status: "rejected", ResetsAt: x, Type: "five_hour"},
	}, at(2026, 10, 22, 10, 0, 0))
	if r.Kind != KindRateLimited || r.Window != "5h" {
		t.Fatalf("got %+v", r)
	}
	wantReset(t, r.ResetAt, x)
}

func TestClassify_ClaudeSignalWarningOnlyNeverClassifies(t *testing.T) {
	for _, st := range []string{"allowed_warning", "allowed"} {
		r := Classify(Input{Err: "claude code prompt: boom", Signal: &Signal{Status: st, Type: "five_hour"}}, at(2026, 10, 22, 10, 0, 0))
		if r != (Result{}) {
			t.Fatalf("%s: got %+v", st, r)
		}
	}
}

func TestClassify_ClaudeSignalOverageRejectedIgnored(t *testing.T) {
	r := Classify(Input{Err: "boom", Signal: &Signal{Status: "rejected", Type: "overage"}}, at(2026, 10, 22, 10, 0, 0))
	if r != (Result{}) {
		t.Fatalf("got %+v", r)
	}
}

func TestClassify_ClaudeOldTextEpoch(t *testing.T) {
	r := Classify(Input{Err: "Claude AI usage limit reached|1790000000"}, at(2026, 10, 22, 10, 0, 0))
	if r.Kind != KindRateLimited || r.Window != "" {
		t.Fatalf("got %+v", r)
	}
	wantReset(t, r.ResetAt, time.Unix(1790000000, 0))
}

func TestClassify_ClaudeNewTextHourLimit(t *testing.T) {
	e := "5-hour limit reached ∙ resets 3pm"
	r := Classify(Input{Err: e}, at(2026, 10, 22, 10, 0, 0))
	if r.Kind != KindRateLimited || r.Window != "5h" {
		t.Fatalf("got %+v", r)
	}
	wantReset(t, r.ResetAt, at(2026, 10, 22, 15, 0, 0))
	r = Classify(Input{Err: e}, at(2026, 10, 22, 16, 0, 0))
	wantReset(t, r.ResetAt, at(2026, 10, 23, 15, 0, 0))
}

func TestClassify_CodexUsageLimit(t *testing.T) {
	r := Classify(Input{Provider: "codex-native", Err: "You've hit your usage limit. Upgrade to Pro"}, at(2026, 10, 22, 10, 0, 0))
	if r.Kind != KindRateLimited || r.ResetAt != nil {
		t.Fatalf("got %+v", r)
	}
}

func TestClassify_UnknownErrorIsNotClassified(t *testing.T) {
	for _, e := range []string{"acp: rpc error -32603: Internal error", "claude code prompt: exit status 1", ""} {
		if r := Classify(Input{Err: e}, at(2026, 10, 22, 10, 0, 0)); r != (Result{}) {
			t.Fatalf("%q: got %+v", e, r)
		}
	}
}

func TestClassify_429OnlyInUnrelatedTextIsNotClassified(t *testing.T) {
	if r := Classify(Input{Err: "tool failed: grep found 4290 matches"}, at(2026, 10, 22, 10, 0, 0)); r != (Result{}) {
		t.Fatalf("got %+v", r)
	}
}

func TestClassify_SignalResetWinsOverText(t *testing.T) {
	x := at(2026, 10, 22, 12, 0, 0)
	r := Classify(Input{
		Err:    glmText,
		Signal: &Signal{Status: "rejected", ResetsAt: x, Type: "seven_day"},
	}, at(2026, 10, 22, 15, 10, 0))
	if r.Kind != KindRateLimited || r.Window != "weekly" {
		t.Fatalf("got %+v", r)
	}
	wantReset(t, r.ResetAt, x)
}

func TestClassify_GenericRateLimitError(t *testing.T) {
	r := Classify(Input{Err: `rate_limit_error: Number of requests has exceeded your rate limit`}, at(2026, 10, 22, 10, 0, 0))
	if r.Kind != KindRateLimited || r.Window != "" || r.ResetAt != nil {
		t.Fatalf("got %+v", r)
	}
}

func TestClassify_GLMResetWithDate(t *testing.T) {
	const e = "429 Usage limit reached for 5 hour. Your limit will reset at 2026-10-22 17:35:18"
	for _, now := range []time.Time{at(2026, 10, 22, 15, 10, 0), at(2026, 10, 25, 9, 0, 0)} {
		r := Classify(Input{Provider: "glm", Err: e}, now)
		if r.Kind != KindRateLimited || r.Window != "5h" {
			t.Fatalf("got %+v", r)
		}
		wantReset(t, r.ResetAt, at(2026, 10, 22, 16, 35, 18))
	}
}

// TestClassify_GLMResetIsShanghaiTime pins the zone: Z.ai's reset text is
// UTC+8 wall time whatever zone the daemon runs in. Real data: a run at
// 16:33:26 +07 on 2026-10-22 got "reset at 2026-10-22 17:35:18"; read in the
// daemon's own zone that is 1h 1m ahead, read as UTC+8 it is 1m 52s ahead.
func TestClassify_GLMResetIsShanghaiTime(t *testing.T) {
	const e = "429 Usage limit reached for 5 hour. Your limit will reset at 2026-10-22 17:35:18"
	want := time.Date(2026, 10, 22, 9, 35, 18, 0, time.UTC)
	for name, loc := range map[string]*time.Location{
		"daemon at +07": time.FixedZone("ICT", 7*3600),
		"daemon at UTC": time.UTC,
		"daemon at -05": time.FixedZone("EST", -5*3600),
		"daemon at +08": time.FixedZone("CST", 8*3600),
	} {
		t.Run(name, func(t *testing.T) {
			now := time.Date(2026, 10, 22, 9, 33, 26, 0, time.UTC).In(loc)
			r := Classify(Input{Provider: "glm", Err: e}, now)
			if r.Kind != KindRateLimited || r.ResetAt == nil || !r.ResetAt.Equal(want) {
				t.Fatalf("got %+v, want reset %v", r, want)
			}
			if d := r.ResetAt.Sub(now); d != 1*time.Minute+52*time.Second {
				t.Fatalf("reset is %v ahead of the run, want 1m52s", d)
			}
		})
	}
	// An undated time is the next occurrence on the Shanghai clock, whatever the daemon zone.
	now := time.Date(2026, 10, 22, 9, 33, 26, 0, time.UTC).In(time.FixedZone("EST", -5*3600))
	r := Classify(Input{Provider: "glm", Err: "429 Usage limit reached for 5 hour. Your limit will reset at 17:35:18"}, now)
	if r.ResetAt == nil || !r.ResetAt.Equal(want) {
		t.Fatalf("undated: got %+v, want reset %v", r, want)
	}
}
