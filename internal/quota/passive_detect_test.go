package quota

import (
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"
)

var passiveNow = time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

// rawHeader builds a header map with the given (possibly non-canonical) keys,
// as a hand-built http.Header would have.
func rawHeader(kv ...string) http.Header {
	h := http.Header{}
	for i := 0; i+1 < len(kv); i += 2 {
		h[kv[i]] = append(h[kv[i]], kv[i+1])
	}
	return h
}

func Test_passive_rejection_truth_table(t *testing.T) {
	t.Parallel()
	states := []string{"", "rejected", "allowed"}
	for _, u := range states {
		for _, f := range states {
			for _, d := range states {
				for _, oi := range states {
					h := http.Header{}
					set := func(name, v string) {
						if v != "" {
							h["anthropic-ratelimit-unified-"+name] = []string{" " + v + " "}
						}
					}
					set("status", u)
					set("5h-status", f)
					set("7d-status", d)
					set("7d_oi-status", oi)

					// Independent expectation.
					wantRej := f == "rejected" || d == "rejected" || u == "rejected"
					wantModel := u == "rejected" && f == "allowed" && d == "allowed" && oi == "rejected"
					if f == "rejected" || d == "rejected" {
						wantModel = false
					}
					want := Rejection{Rejected: wantRej, ModelOnly: wantModel}
					switch {
					case !wantRej:
					case d == "rejected":
						want.Window = WindowWeekly
					case f == "rejected":
						want.Window = WindowFiveHour
					case wantModel:
						want.Window = WindowWeeklyModel
					default:
						want.Window = WindowUnified
					}

					name := "u=" + u + ",5h=" + f + ",7d=" + d + ",oi=" + oi
					t.Run(name, func(t *testing.T) {
						t.Parallel()
						if got := ClassifyRejection(http.StatusTooManyRequests, h); got != want {
							t.Fatalf("429: got %+v want %+v", got, want)
						}
						for _, st := range []int{200, 500} {
							if got := ClassifyRejection(st, h); got != (Rejection{}) {
								t.Fatalf("status %d: got %+v want zero", st, got)
							}
						}
					})
				}
			}
		}
	}
}

func Test_passive_rejection_named_shapes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		h    http.Header
		want Rejection
	}{
		{"oi-only without unified rejected is not a rejection",
			rawHeader("Anthropic-Ratelimit-Unified-5h-Status", "allowed", "Anthropic-Ratelimit-Unified-7d-Status", "allowed", "Anthropic-Ratelimit-Unified-7d_oi-Status", "rejected"),
			Rejection{}},
		{"oi-only with unified rejected is model scoped",
			rawHeader("Anthropic-Ratelimit-Unified-Status", "rejected", "Anthropic-Ratelimit-Unified-5h-Status", "allowed", "Anthropic-Ratelimit-Unified-7d-Status", "allowed", "Anthropic-Ratelimit-Unified-7d_oi-Status", "REJECTED"),
			Rejection{Rejected: true, ModelOnly: true, Window: WindowWeeklyModel}},
		// claude_executor_ratelimit_test.go:30-41
		{"ref 7-day example",
			rawHeader("Anthropic-Ratelimit-Unified-Status", "rejected", "Anthropic-Ratelimit-Unified-5h-Status", "allowed", "Anthropic-Ratelimit-Unified-7d-Status", "rejected"),
			Rejection{Rejected: true, Window: WindowWeekly}},
		{"bare 429 with allowed windows",
			rawHeader("Anthropic-Ratelimit-Unified-5h-Status", "allowed", "Anthropic-Ratelimit-Unified-7d-Status", "allowed"),
			Rejection{}},
		{"nil header", nil, Rejection{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ClassifyRejection(429, tc.h); got != tc.want {
				t.Fatalf("got %+v want %+v", got, tc.want)
			}
		})
	}
}

func epoch(t time.Time) string { return strconv.FormatInt(t.Unix(), 10) }

func Test_passive_reset_latest_deadline_wins(t *testing.T) {
	t.Parallel()
	now := passiveNow
	h := rawHeader(
		"Retry-After", "600",
		"Anthropic-Ratelimit-Unified-5h-Status", "allowed",
		"Anthropic-Ratelimit-Unified-5h-Reset", epoch(now.Add(2*time.Hour)),
		"Anthropic-Ratelimit-Unified-7d-Status", "rejected",
		"Anthropic-Ratelimit-Unified-7d-Reset", epoch(now.Add(72*time.Hour)),
	)
	r := ClassifyRejection(429, h)
	got, ok := RejectionDeadline(h, now, r)
	if !ok || !got.Equal(now.Add(72*time.Hour)) {
		t.Fatalf("got %v %v want +3d", got, ok)
	}

	// 5h-reset of a non-rejected window is ignored, even when later.
	h = rawHeader(
		"Retry-After", "600",
		"Anthropic-Ratelimit-Unified-5h-Status", "allowed",
		"Anthropic-Ratelimit-Unified-5h-Reset", epoch(now.Add(5*time.Hour)),
		"Anthropic-Ratelimit-Unified-7d-Status", "rejected",
		"Anthropic-Ratelimit-Unified-7d-Reset", epoch(now.Add(time.Hour)),
	)
	got, ok = RejectionDeadline(h, now, ClassifyRejection(429, h))
	if !ok || !got.Equal(now.Add(time.Hour)) {
		t.Fatalf("non-rejected window counted: got %v %v", got, ok)
	}

	// oi-only: only Retry-After is a candidate.
	h = rawHeader(
		"Retry-After", "120",
		"Anthropic-Ratelimit-Unified-Status", "rejected",
		"Anthropic-Ratelimit-Unified-5h-Status", "allowed",
		"Anthropic-Ratelimit-Unified-7d-Status", "allowed",
		"Anthropic-Ratelimit-Unified-7d_oi-Status", "rejected",
		"Anthropic-Ratelimit-Unified-7d_oi-Reset", epoch(now.Add(48*time.Hour)),
		"Anthropic-Ratelimit-Unified-Reset", epoch(now.Add(48*time.Hour)),
	)
	r = ClassifyRejection(429, h)
	got, ok = RejectionDeadline(h, now, r)
	if !r.ModelOnly || !ok || !got.Equal(now.Add(2*time.Minute)) {
		t.Fatalf("oi-only: got %v %v %+v", got, ok, r)
	}

	// Past or absent deadlines and not-rejected verdicts.
	h = rawHeader("Anthropic-Ratelimit-Unified-5h-Status", "rejected", "Anthropic-Ratelimit-Unified-5h-Reset", epoch(now.Add(-time.Hour)))
	if _, ok := RejectionDeadline(h, now, ClassifyRejection(429, h)); ok {
		t.Fatal("past reset accepted")
	}
	if _, ok := RejectionDeadline(h, now, Rejection{}); ok {
		t.Fatal("deadline for non-rejection")
	}

	// Unified reset applies when only the unified status is rejected.
	h = rawHeader("Anthropic-Ratelimit-Unified-Status", "rejected", "Anthropic-Ratelimit-Unified-Reset", epoch(now.Add(time.Hour)))
	got, ok = RejectionDeadline(h, now, ClassifyRejection(429, h))
	if !ok || !got.Equal(now.Add(time.Hour)) {
		t.Fatalf("unified reset: got %v %v", got, ok)
	}
}

func Test_passive_reset_formats(t *testing.T) {
	t.Parallel()
	now := passiveNow
	target := now.Add(90 * time.Minute)
	tests := []struct {
		name string
		h    http.Header
		want time.Time
	}{
		{"epoch", rawHeader("Anthropic-Ratelimit-Unified-5h-Reset", epoch(target)), target},
		{"fractional epoch", rawHeader("Anthropic-Ratelimit-Unified-5h-Reset", epoch(target)+".5"), target.Add(500 * time.Millisecond)},
		{"rfc3339", rawHeader("Anthropic-Ratelimit-Unified-5h-Reset", target.Format(time.RFC3339)), target},
		{"http-date", rawHeader("Anthropic-Ratelimit-Unified-5h-Reset", target.Format(http.TimeFormat)), target},
		{"retry-after delta", rawHeader("Retry-After", "45"), now.Add(45 * time.Second)},
		{"retry-after fractional", rawHeader("Retry-After", "1.5"), now.Add(1500 * time.Millisecond)},
		{"retry-after date", rawHeader("Retry-After", target.Format(http.TimeFormat)), target},
		{"retry-after rfc3339", rawHeader("Retry-After", target.Format(time.RFC3339)), target},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := tc.h
			h["Anthropic-Ratelimit-Unified-5h-Status"] = []string{"rejected"}
			got, ok := RejectionDeadline(h, now, ClassifyRejection(429, h))
			if !ok || got.Sub(tc.want).Abs() > time.Millisecond {
				t.Fatalf("got %v %v want %v", got, ok, tc.want)
			}
		})
	}
	for _, bad := range []string{"", "NaN", "Inf", "-5", "0", "soon"} {
		h := rawHeader("Anthropic-Ratelimit-Unified-5h-Status", "rejected", "Anthropic-Ratelimit-Unified-5h-Reset", bad, "Retry-After", bad)
		if _, ok := RejectionDeadline(h, now, ClassifyRejection(429, h)); ok {
			t.Fatalf("garbage %q accepted", bad)
		}
	}
}

func Test_passive_grace_is_1_to_30_seconds_and_never_negative(t *testing.T) {
	t.Parallel()
	seen := map[time.Duration]bool{}
	for i := 0; i < 1000; i++ {
		i := int64(i)
		d := graceFrom(func(max int64) (int64, error) { return i % max, nil })
		if d < time.Second || d > 30*time.Second || d%time.Second != 0 {
			t.Fatalf("grace %v out of range", d)
		}
		seen[d] = true
	}
	if !seen[time.Second] || !seen[30*time.Second] {
		t.Fatal("bounds 1s and 30s not both hit")
	}
	if d := graceFrom(func(int64) (int64, error) { return 0, errors.New("rng") }); d != time.Second {
		t.Fatalf("failing source: %v", d)
	}
	for i := 0; i < 200; i++ {
		if d := DefaultGrace(); d < time.Second || d > 30*time.Second {
			t.Fatalf("DefaultGrace %v", d)
		}
	}
	dl := passiveNow.Add(time.Hour)
	if got := CooldownEnd(dl, passiveNow, -time.Hour); !got.Equal(dl) {
		t.Fatalf("negative grace subtracted: %v", got)
	}
	if got := CooldownEnd(dl, passiveNow, 5*time.Second); !got.Equal(dl.Add(5 * time.Second)) {
		t.Fatalf("grace not added: %v", got)
	}
}

func Test_passive_cooldown_clamped_to_8_days(t *testing.T) {
	t.Parallel()
	now := passiveNow
	if got := CooldownEnd(now.Add(30*24*time.Hour), now, 30*time.Second); !got.Equal(now.Add(MaxCooldown)) {
		t.Fatalf("got %v", got)
	}
	if got := CooldownEnd(now.Add(MaxCooldown-10*time.Second), now, 30*time.Second); !got.Equal(now.Add(MaxCooldown)) {
		t.Fatalf("grace overflow not clamped: %v", got)
	}
	// A garbage far-future header still yields a deadline; the clamp is CooldownEnd's.
	h := rawHeader("Anthropic-Ratelimit-Unified-5h-Status", "rejected", "Anthropic-Ratelimit-Unified-5h-Reset", "9e300", "Retry-After", "9e300")
	dl, ok := RejectionDeadline(h, now, ClassifyRejection(429, h))
	if !ok {
		t.Fatal("huge reset dropped")
	}
	if got := CooldownEnd(dl, now, time.Second); !got.Equal(now.Add(MaxCooldown)) {
		t.Fatalf("huge reset not clamped: %v", got)
	}
}

func Test_passive_fallback_duration_levels(t *testing.T) {
	t.Parallel()
	tests := []struct {
		level int
		want  time.Duration
	}{
		{-5, time.Minute}, {0, time.Minute}, {1, time.Minute}, {2, 2 * time.Minute}, {3, 4 * time.Minute},
		{4, 8 * time.Minute}, {5, 16 * time.Minute}, {6, 30 * time.Minute}, {7, 30 * time.Minute},
		{64, 30 * time.Minute}, {1 << 30, 30 * time.Minute}, {int(^uint(0) >> 1), 30 * time.Minute},
	}
	for _, tc := range tests {
		if got := FallbackDuration(tc.level); got != tc.want {
			t.Errorf("level %d: got %v want %v", tc.level, got, tc.want)
		}
	}
}

func Test_passive_bare_429_retry_after_backoff_capped_60s(t *testing.T) {
	t.Parallel()
	now := passiveNow
	tests := []struct {
		name   string
		h      http.Header
		want   time.Duration
		wantOK bool
	}{
		{"30", rawHeader("retry-after", "30"), 30 * time.Second, true},
		{"3600 capped", rawHeader("Retry-After", "3600"), time.Minute, true},
		{"date", rawHeader("Retry-After", now.Add(20*time.Second).Format(http.TimeFormat)), 20 * time.Second, true},
		{"absent", http.Header{}, 0, false},
		{"garbage", rawHeader("Retry-After", "soon"), 0, false},
		{"zero", rawHeader("Retry-After", "0"), 0, false},
		{"negative", rawHeader("Retry-After", "-3"), 0, false},
		{"past date", rawHeader("Retry-After", now.Add(-time.Hour).Format(http.TimeFormat)), 0, false},
		// claude_executor_ratelimit_test.go:517-530: 5h/7d allowed, a bare 429.
		{"ref bare 429 fixture has no retry-after", rawHeader("Anthropic-Ratelimit-Unified-5h-Status", "allowed", "Anthropic-Ratelimit-Unified-7d-Status", "allowed"), 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := RetryAfterBackoff(tc.h, now)
			if ok != tc.wantOK || got != tc.want {
				t.Fatalf("got %v %v want %v %v", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func Test_passive_rejection_window_classification(t *testing.T) {
	t.Parallel()
	const p = "Anthropic-Ratelimit-Unified-"
	tests := []struct {
		name string
		h    http.Header
		want string
	}{
		{"5h rejected", rawHeader(p+"Status", "rejected", p+"5h-Status", "rejected", p+"7d-Status", "allowed"), WindowFiveHour},
		{"7d rejected", rawHeader(p+"Status", "rejected", p+"5h-Status", "allowed", p+"7d-Status", "rejected"), WindowWeekly},
		{"both 5h and 7d rejected: weekly", rawHeader(p+"Status", "rejected", p+"5h-Status", "rejected", p+"7d-Status", "rejected"), WindowWeekly},
		{"7d_oi only", rawHeader(p+"Status", "rejected", p+"5h-Status", "allowed", p+"7d-Status", "allowed", p+"7d_oi-Status", "rejected"), WindowWeeklyModel},
		{"7d_oi rejected next to a rejected 5h", rawHeader(p+"Status", "rejected", p+"5h-Status", "rejected", p+"7d_oi-Status", "rejected"), WindowFiveHour},
		{"only the unified status says rejected", rawHeader(p+"Status", "rejected"), WindowUnified},
		{"unified rejected with allowed_warning windows", rawHeader(p+"Status", "rejected", p+"5h-Status", "allowed_warning", p+"7d-Status", "allowed"), WindowUnified},
		{"7d_oi rejected with a warning 5h is not model only", rawHeader(p+"Status", "rejected", p+"5h-Status", "allowed_warning", p+"7d_oi-Status", "rejected"), WindowUnified},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ClassifyRejection(429, tc.h)
			if !got.Rejected || got.Window != tc.want {
				t.Fatalf("got %+v, want window %q", got, tc.want)
			}
		})
	}
	if got := ClassifyRejection(429, rawHeader(p+"5h-Status", "allowed")); got.Window != "" {
		t.Fatalf("a non-rejection carries window %q", got.Window)
	}
}
