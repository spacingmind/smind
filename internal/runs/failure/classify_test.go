package failure

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
			r := Classify(Input{Provider: "glm", Vendor: "zai", Err: glmText}, c.now)
			if r.Kind != KindPlanWindowExhausted || r.Window != "5h" {
				t.Fatalf("got %+v", r)
			}
			wantReset(t, r.ResetAt, c.want)
		})
	}
}

func TestClassify_GLMWrappedErrorStillMatches(t *testing.T) {
	e := glmText + ` (taskrunner: glm prompt: acp: rpc error -32603: Internal error ({"details":"` + glmText + `"}))`
	r := Classify(Input{Provider: "glm", Vendor: "zai", Err: e}, at(2026, 10, 22, 15, 10, 0))
	if r.Kind != KindPlanWindowExhausted || r.Window != "5h" {
		t.Fatalf("got %+v", r)
	}
	wantReset(t, r.ResetAt, at(2026, 10, 22, 16, 35, 18))
}

func TestClassify_UnparseableResetKeepsKindNilReset(t *testing.T) {
	r := Classify(Input{Vendor: "zai", Err: "429 Usage limit reached for 5 hour. Your limit will reset at soon"}, at(2026, 10, 22, 15, 0, 0))
	if r.Kind != KindPlanWindowExhausted || r.ResetAt != nil {
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
	if r.Kind != KindPlanWindowExhausted || r.Window != "5h" {
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
	r := Classify(Input{Vendor: "anthropic", Err: "Claude AI usage limit reached|1790000000"}, at(2026, 10, 22, 10, 0, 0))
	if r.Kind != KindPlanWindowExhausted || r.Window != "" {
		t.Fatalf("got %+v", r)
	}
	wantReset(t, r.ResetAt, time.Unix(1790000000, 0))
}

func TestClassify_ClaudeNewTextHourLimit(t *testing.T) {
	e := "5-hour limit reached ∙ resets 3pm"
	r := Classify(Input{Vendor: "anthropic", Err: e}, at(2026, 10, 22, 10, 0, 0))
	if r.Kind != KindPlanWindowExhausted || r.Window != "5h" {
		t.Fatalf("got %+v", r)
	}
	wantReset(t, r.ResetAt, at(2026, 10, 22, 15, 0, 0))
	r = Classify(Input{Vendor: "anthropic", Err: e}, at(2026, 10, 22, 16, 0, 0))
	wantReset(t, r.ResetAt, at(2026, 10, 23, 15, 0, 0))
}

func TestClassify_CodexUsageLimit(t *testing.T) {
	r := Classify(Input{Provider: "codex-native", Vendor: "openai", Err: "You've hit your usage limit. Upgrade to Pro"}, at(2026, 10, 22, 10, 0, 0))
	if r.Kind != KindPlanWindowExhausted || r.ResetAt != nil {
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
		Vendor: "zai",
		Signal: &Signal{Status: "rejected", ResetsAt: x, Type: "seven_day"},
	}, at(2026, 10, 22, 15, 10, 0))
	if r.Kind != KindPlanWindowExhausted || r.Window != "weekly" {
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
		r := Classify(Input{Provider: "glm", Vendor: "zai", Err: e}, now)
		if r.Kind != KindPlanWindowExhausted || r.Window != "5h" {
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
			r := Classify(Input{Provider: "glm", Vendor: "zai", Err: e}, now)
			if r.Kind != KindPlanWindowExhausted || r.ResetAt == nil || !r.ResetAt.Equal(want) {
				t.Fatalf("got %+v, want reset %v", r, want)
			}
			if d := r.ResetAt.Sub(now); d != 1*time.Minute+52*time.Second {
				t.Fatalf("reset is %v ahead of the run, want 1m52s", d)
			}
		})
	}
	// An undated time is the next occurrence on the Shanghai clock, whatever the daemon zone.
	now := time.Date(2026, 10, 22, 9, 33, 26, 0, time.UTC).In(time.FixedZone("EST", -5*3600))
	r := Classify(Input{Provider: "glm", Vendor: "zai", Err: "429 Usage limit reached for 5 hour. Your limit will reset at 17:35:18"}, now)
	if r.ResetAt == nil || !r.ResetAt.Equal(want) {
		t.Fatalf("undated: got %+v, want reset %v", r, want)
	}
}

func TestClassify_ClaudeSessionLimitText(t *testing.T) {
	const e = "You've hit your session limit · resets 1pm (Asia/Saigon)"
	// 12:39 +07: the reset is 13:00 the same day, whatever zone the daemon runs in.
	want := time.Date(2026, 10, 23, 13, 0, 0, 0, time.FixedZone("ICT", 7*3600))
	for name, loc := range map[string]*time.Location{
		"daemon at +07": time.FixedZone("ICT", 7*3600),
		"daemon at UTC": time.UTC,
		"daemon at -05": time.FixedZone("EST", -5*3600),
	} {
		t.Run(name, func(t *testing.T) {
			now := time.Date(2026, 10, 23, 12, 39, 0, 0, time.FixedZone("ICT", 7*3600)).In(loc)
			r := Classify(Input{Provider: "claude-native", Vendor: "anthropic", Err: e}, now)
			if r.Kind != KindPlanWindowExhausted || r.Window != "5h" {
				t.Fatalf("got %+v", r)
			}
			wantReset(t, r.ResetAt, want)
		})
	}
	// After the reset time has passed the next occurrence is tomorrow.
	late := time.Date(2026, 10, 23, 14, 46, 0, 0, time.FixedZone("ICT", 7*3600))
	r := Classify(Input{Provider: "claude-native", Vendor: "anthropic", Err: e}, late)
	wantReset(t, r.ResetAt, want.AddDate(0, 0, 1))
}

func TestClassify_ClaudeLimitTextVariants(t *testing.T) {
	now := time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		in, window string
		reset      bool
	}{
		{"You've hit your weekly limit · resets 9am (UTC)", "weekly", true},
		{"You've hit your Opus limit", "weekly-opus", false},
		{"You've hit your limit · resets 3pm", "", true},
		{"You've hit your session limit · resets 1pm (Not/AZone)", "5h", true}, // unknown zone falls back to now's
	}
	for _, c := range cases {
		r := Classify(Input{Provider: "claude-native", Vendor: "anthropic", Err: c.in}, now)
		if r.Kind != KindPlanWindowExhausted || r.Window != c.window || (r.ResetAt != nil) != c.reset {
			t.Errorf("%q -> %+v, want window %q reset %v", c.in, r, c.window, c.reset)
		}
	}
}

func TestIsLimitRefusal(t *testing.T) {
	yes := []string{"You've hit your session limit · resets 1pm (Asia/Saigon)", "  you've hit your weekly limit  "}
	no := []string{"", "All done. I fixed it.", "I told the user: You've hit your session limit, then wrote a long essay " + string(make([]byte, 0)) + stringsRepeat("x", 400)}
	for _, s := range yes {
		if !IsLimitRefusal(s) {
			t.Errorf("IsLimitRefusal(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if IsLimitRefusal(s) {
			t.Errorf("IsLimitRefusal(%.40q) = true, want false", s)
		}
	}
}

func stringsRepeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}

// TestClassify_ResetInferred pins which resets are marked: only those read in
// an assumed zone. The GLM text is the real Z.ai 429 of the 2026-10-22 dogfood
// run; Claude's stated zone, an epoch and a backend signal are never marked.
func TestClassify_ResetInferred(t *testing.T) {
	now := at(2026, 10, 22, 12, 39, 0)
	cases := []struct {
		name string
		in   Input
		want bool
	}{
		{"glm dated", Input{Provider: "glm", Vendor: "zai", Err: "429 Usage limit reached for 5 hour. Your limit will reset at 2026-10-22 17:35:18"}, true},
		{"glm clock only", Input{Provider: "glm", Vendor: "zai", Err: glmText}, true},
		{"glm unparseable reset", Input{Provider: "glm", Vendor: "zai", Err: "429 Usage limit reached for 5 hour. Your limit will reset at soon"}, false},
		{"claude stated zone", Input{Provider: "claude-native", Vendor: "anthropic", Err: "You've hit your session limit · resets 1pm (Asia/Saigon)"}, false},
		{"claude zone-less", Input{Provider: "claude-native", Vendor: "anthropic", Err: "You've hit your session limit · resets 1pm"}, true},
		{"claude unknown zone", Input{Provider: "claude-native", Vendor: "anthropic", Err: "You've hit your session limit · resets 1pm (Mars/Olympus)"}, true},
		{"claude 5-hour zone-less", Input{Provider: "claude-native", Vendor: "anthropic", Err: "5-hour limit reached - resets 3pm"}, true},
		{"claude epoch", Input{Provider: "claude-native", Vendor: "anthropic", Err: "Claude AI usage limit reached|1790000000"}, false},
		{"claude signal", Input{Provider: "claude-native", Vendor: "anthropic", Err: "exit status 1", Signal: &Signal{Status: "rejected", ResetsAt: time.Unix(1790000000, 0), Type: "five_hour"}}, false},
		{"signal without time falls back to inferred text", Input{Provider: "claude-native", Vendor: "anthropic", Err: "5-hour limit reached - resets 3pm", Signal: &Signal{Status: "rejected", Type: "five_hour"}}, true},
		{"signal time wins over zone-less text", Input{Provider: "claude-native", Vendor: "anthropic", Err: "5-hour limit reached - resets 3pm", Signal: &Signal{Status: "rejected", ResetsAt: time.Unix(1790000000, 0), Type: "five_hour"}}, false},
		{"no reset at all", Input{Provider: "codex-native", Vendor: "openai", Err: "You've hit your usage limit"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Classify(c.in, now)
			if r.Kind != KindPlanWindowExhausted {
				t.Fatalf("got %+v, want rate limited", r)
			}
			if r.ResetInferred != c.want {
				t.Fatalf("ResetInferred = %v, want %v (%+v)", r.ResetInferred, c.want, r)
			}
			if r.ResetInferred && r.ResetAt == nil {
				t.Fatalf("inferred without a reset: %+v", r)
			}
		})
	}
}

// TestClassify_ResetZone pins the zone the daemon reports for the client's
// "Details" note, per provider case.
func TestClassify_ResetZone(t *testing.T) {
	now := at(2026, 10, 22, 12, 39, 0)
	cases := []struct {
		name string
		in   Input
		want string
	}{
		{"glm is UTC+8", Input{Provider: "glm", Vendor: "zai", Err: glmText}, "UTC+8"},
		{"glm dated is UTC+8", Input{Provider: "glm", Vendor: "zai", Err: "429 Usage limit reached for 5 hour. Your limit will reset at 2026-10-22 17:35:18"}, "UTC+8"},
		{"claude printed IANA zone", Input{Provider: "claude-native", Vendor: "anthropic", Err: "You've hit your session limit · resets 1pm (Asia/Saigon)"}, "Asia/Saigon"},
		{"claude zone-less is daemon-local", Input{Provider: "claude-native", Vendor: "anthropic", Err: "5-hour limit reached - resets 3pm"}, "daemon-local:ICT"},
		{"claude unknown zone is daemon-local", Input{Provider: "claude-native", Vendor: "anthropic", Err: "You've hit your session limit · resets 1pm (Mars/Olympus)"}, "daemon-local:ICT"},
		{"no reset parsed", Input{Provider: "codex-native", Vendor: "openai", Err: "You've hit your usage limit"}, ""},
		{"glm unparseable reset", Input{Provider: "glm", Vendor: "zai", Err: "429 Usage limit reached for 5 hour. Your limit will reset at soon"}, ""},
		{"absolute epoch", Input{Provider: "claude-native", Vendor: "anthropic", Err: "Claude AI usage limit reached|1790000000"}, ""},
		{"backend signal", Input{Provider: "claude-native", Vendor: "anthropic", Err: "exit status 1", Signal: &Signal{Status: "rejected", ResetsAt: time.Unix(1790000000, 0), Type: "five_hour"}}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Classify(c.in, now).ResetZone; got != c.want {
				t.Fatalf("ResetZone = %q, want %q", got, c.want)
			}
		})
	}
}

func TestClassify_ResetZoneLocalOffsetName(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 39, 0, 0, time.Local)
	got := Classify(Input{Provider: "claude-native", Vendor: "anthropic", Err: "5-hour limit reached - resets 3pm"}, now).ResetZone
	if want := DaemonLocalPrefix + "UTC" + now.Format("-07:00"); got != want {
		t.Fatalf("ResetZone = %q, want %q", got, want)
	}
}

// TestRateLimit_VendorProfile_GLMOnInstance: a GLM-worded 429 resolves by
// the vendor profile, not the provider id -- on the zai profile it carries
// the 5h window and the UTC+8 reset; on anthropic only the generic pattern
// fires, with no window or reset.
func TestRateLimit_VendorProfile_GLMOnInstance(t *testing.T) {
	r := Classify(Input{Vendor: "zai", Err: glmText}, at(2026, 10, 22, 15, 10, 0))
	if r.Kind != KindPlanWindowExhausted || r.Window != "5h" {
		t.Fatalf("zai: got %+v", r)
	}
	if r.ResetZone != "UTC+8" {
		t.Fatalf("zai: ResetZone = %q, want UTC+8", r.ResetZone)
	}
	if r.ResetAt == nil {
		t.Fatal("zai: ResetAt = nil, want a reset")
	}

	r = Classify(Input{Vendor: "anthropic", Err: glmText}, at(2026, 10, 22, 15, 10, 0))
	if r.Kind != KindPlanWindowExhausted {
		t.Fatalf("anthropic: got %+v, want the generic pattern to fire", r)
	}
	if r.Window != "" || r.ResetAt != nil {
		t.Fatalf("anthropic: got %+v, want no window and no reset", r)
	}
}

func TestRateLimit_GenericUnknownVendor(t *testing.T) {
	if r := Classify(Input{Vendor: "generic", Err: `API Error: 429 {"error":{"type":"rate_limit_error","message":"slow down"}}`}, at(2026, 10, 22, 10, 0, 0)); r.Kind != KindRateLimited {
		t.Fatalf("generic 429: got %+v", r)
	}
	const claudeSession = "You've hit your session limit · resets 1pm (Asia/Saigon)"
	if r := Classify(Input{Vendor: "generic", Err: claudeSession}, at(2026, 10, 23, 12, 39, 0)); r.Kind != "" {
		t.Fatalf("generic claude text: got %+v, want not classified", r)
	}
	if r := Classify(Input{Vendor: "", Err: claudeSession}, at(2026, 10, 23, 12, 39, 0)); r.Kind != "" {
		t.Fatalf("empty vendor: got %+v, want not classified", r)
	}
}

func TestRateLimit_ClaudeTextOnlyForAnthropic(t *testing.T) {
	const e = "You've hit your session limit · resets 1pm (Asia/Saigon)"
	if r := Classify(Input{Vendor: "anthropic", Err: e}, at(2026, 10, 23, 12, 39, 0)); r.Kind != KindPlanWindowExhausted || r.Window != "5h" {
		t.Fatalf("anthropic: got %+v, want rate_limited/5h", r)
	}
	if r := Classify(Input{Vendor: "zai", Err: e}, at(2026, 10, 23, 12, 39, 0)); r.Kind != "" {
		t.Fatalf("zai: got %+v, want not classified", r)
	}
}

func TestClassify_PlanWindowBeatsAuthAndModelText(t *testing.T) {
	err := `Not logged in · Please run /login. There's an issue with the selected model (x). You've hit your weekly limit · resets 1pm (Asia/Saigon)`
	r := Classify(Input{Vendor: "anthropic", Err: err}, at(2026, 11, 1, 3, 0, 0))
	if r.Kind != KindPlanWindowExhausted || r.Window != "weekly" {
		t.Fatalf("got %+v", r)
	}
}

func TestClassify_BareRateLimitIsResidualNotTransient(t *testing.T) {
	r := Classify(Input{Err: `API Error: 429 {"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`}, at(2026, 11, 1, 3, 0, 0))
	if r.Kind != KindRateLimited || r.RetryAfter != 0 {
		t.Fatalf("got %+v", r)
	}
}

func TestClassify_TransientNeedsRetryAfterOrOverloadBody(t *testing.T) {
	now := at(2026, 11, 1, 3, 0, 0)
	rl := `API Error: 429 rate_limit_error`
	if r := Classify(Input{Err: rl, RetryAfter: 12 * time.Second}, now); r.Kind != KindTransient429 || r.RetryAfter != 12*time.Second {
		t.Fatalf("retry-after: got %+v", r)
	}
	if r := Classify(Input{Err: rl + ", try again in 2 minutes"}, now); r.Kind != KindTransient429 || r.RetryAfter != 2*time.Minute {
		t.Fatalf("try again text: got %+v", r)
	}
	if r := Classify(Input{Err: `API Error: 529 {"error":{"type":"overloaded_error"}}`}, now); r.Kind != KindTransient429 {
		t.Fatalf("overload body: got %+v", r)
	}
}

func TestClassify_ModelErrorMentioningLimitStaysModel(t *testing.T) {
	r := Classify(Input{Err: `The 'gpt-4o' model is not supported when using Codex with a ChatGPT account. See the rate limit docs.`}, at(2026, 11, 1, 3, 0, 0))
	if r.Kind != KindModelNotInPlan {
		t.Fatalf("got %+v", r)
	}
}

func TestClassify_AuthErrorMentioning429StaysAuth(t *testing.T) {
	r := Classify(Input{Err: `API Error: 401 {"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}} (earlier: 429 too many requests)`}, at(2026, 11, 1, 3, 0, 0))
	if r.Kind != KindAuthExpired {
		t.Fatalf("got %+v", r)
	}
}

func TestClassify_StructuredCodeBeatsText(t *testing.T) {
	now := at(2026, 11, 1, 3, 0, 0)
	for code, want := range map[string]string{CodeUsageLimit: KindPlanWindowExhausted, CodeUnauthorized: KindAuthExpired, CodeOverloaded: KindTransient429, CodeRateLimit: KindTransient429} {
		if r := Classify(Input{Code: code, Err: "something unrelated"}, now); r.Kind != want {
			t.Fatalf("code %s: got %+v, want %s", code, r, want)
		}
	}
	if r := Classify(Input{Err: "I fixed the login page and the rate limiter"}, now); r.Kind != "" {
		t.Fatalf("plain text classified: %+v", r)
	}
}

// An expired login can surface as "issue with the selected model" (claude-code
// CHANGELOG), so auth text wins over model text.
func TestClassify_AuthBeatsModelText(t *testing.T) {
	r := Classify(Input{Err: `Not logged in · Please run /login (There's an issue with the selected model (x). It may not exist or you may not have access to it.)`}, at(2026, 11, 1, 3, 0, 0))
	if r.Kind != KindAuthExpired {
		t.Fatalf("got %+v", r)
	}
}
