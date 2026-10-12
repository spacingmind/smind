package quota

import (
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"
)

// rejectWeekly is a 429 declaring a weekly rejection that resets at reset.
func rejectWeekly(reset time.Time) http.Header {
	return hdr(uf+"status", "rejected", uf+"5h-status", "allowed", uf+"7d-status", "rejected", uf+"7d-reset", epoch(reset))
}

func Test_passive_window_persists_restart(t *testing.T) {
	t.Parallel()
	pe := newPassiveEnv(t)
	idB, idC := pe.addOAuth("b"), pe.addOAuth("c")
	now := pe.clk.Now()

	// A: a 5h rejection, then a later weekly one that extends it: the window
	// follows the extending rejection.
	pe.observe("m", http.StatusTooManyRequests, reject5h(now.Add(time.Hour)))
	if c, ok := pe.cooldown("m"); !ok || c.Window != WindowFiveHour {
		t.Fatalf("5h cooldown = %+v/%v, want window %q", c, ok, WindowFiveHour)
	}
	weeklyReset := now.Add(48 * time.Hour)
	pe.observe("m", http.StatusTooManyRequests, rejectWeekly(weeklyReset))
	// B: a fallback cooldown keeps its window, also when it escalates.
	pe.p.Observe(respAt(idB, "m", 429, rejectNoDeadline(), now))
	pe.clk.Advance(time.Minute + time.Second)
	t2 := pe.clk.Now()
	pe.p.Observe(respAt(idB, "m", 429, rejectNoDeadline(), t2))
	// C: model scope, plus a bare 429 back-off that has no window.
	pe.p.Observe(respAt(idC, "claude-opus-5", 429, rejectOIOnly(30*60), now))
	pe.p.Observe(respAt(idC, "claude-haiku", 429, hdr("Retry-After", "20"), t2))
	if c, ok := pe.p.Cooldown(idC, "claude-haiku", t2); !ok || c.Reason != ReasonBackoff || c.Window != "" {
		t.Fatalf("back-off = %+v/%v, want no window", c, ok)
	}
	pe.p.Close()

	pe.restart()
	p2 := pe.newPassive()
	t.Cleanup(p2.Close)
	for _, tc := range []struct {
		id     int64
		model  string
		window string
		reason CooldownReason
	}{
		{pe.id, "m", WindowWeekly, ReasonRejected},
		{idB, "m", WindowFiveHour, ReasonFallback},
		{idC, "claude-opus-5", WindowWeeklyModel, ReasonRejected},
	} {
		got, ok := p2.Cooldown(tc.id, tc.model, t2)
		if !ok || got.Window != tc.window || got.Reason != tc.reason {
			t.Errorf("account %d after restart = %+v/%v, want window %q reason %s", tc.id, got, ok, tc.window, tc.reason)
		}
	}
	if got, _ := p2.Cooldown(pe.id, "m", t2); !got.Until.Equal(graceEnd(weeklyReset)) {
		t.Errorf("extended until = %v, want %v", got.Until, graceEnd(weeklyReset))
	}
}

func Test_passive_active_lists_only_nonexpired_nontransient_cooldowns(t *testing.T) {
	t.Parallel()
	pe := newPassiveEnv(t)
	now := pe.clk.Now()
	if got := pe.p.Active(pe.id, now); len(got) != 0 {
		t.Fatalf("Active() with nothing recorded = %+v", got)
	}

	// Two model scopes with different ends, an account-wide cooldown, a bare-429
	// back-off (transient) and a model cooldown that has already ended.
	pe.p.Observe(respAt(pe.id, "claude-opus-5", 429, rejectOIOnly(10*60), now))
	pe.p.Observe(respAt(pe.id, "claude-sonnet-4", 429, rejectOIOnly(30*60), now))
	pe.p.Observe(respAt(pe.id, "claude-old", 429, rejectOIOnly(60), now))
	pe.p.Observe(respAt(pe.id, "claude-haiku", 429, hdr("Retry-After", "30"), now))
	pe.p.Observe(respAt(pe.id, "m", 429, reject5h(now.Add(time.Hour)), now))
	if _, ok := pe.p.Cooldown(pe.id, "claude-haiku", now); !ok {
		t.Fatal("the bare-429 back-off should apply to routing")
	}
	later := now.Add(2 * time.Minute) // claude-old (1 min + grace) ended

	got := pe.p.Active(pe.id, later)
	if len(got) != 3 {
		t.Fatalf("Active() = %+v, want account-wide + 2 model cooldowns", got)
	}
	if got[0].Scope != CooldownAccount || got[0].Window != WindowFiveHour || !got[0].Until.Equal(graceEnd(now.Add(time.Hour))) {
		t.Errorf("first = %+v, want the account-wide 5h cooldown", got[0])
	}
	if got[1].Model != "claude-sonnet-4" || got[2].Model != "claude-opus-5" {
		t.Errorf("model order = %q, %q, want latest end first", got[1].Model, got[2].Model)
	}
	for _, c := range got {
		if c.Transient() || !c.Until.After(later) {
			t.Errorf("Active() returned %+v", c)
		}
	}
	if got := pe.p.Active(pe.id, now.Add(2*time.Hour)); len(got) != 0 {
		t.Errorf("Active() after every end = %+v", got)
	}

	// An end beyond now+MaxCooldown (a clock jump) is clamped like Cooldown does.
	if c := pe.p.Active(pe.id, now.Add(-30*24*time.Hour)); len(c) == 0 || c[0].Until.After(now.Add(-30*24*time.Hour).Add(MaxCooldown)) {
		t.Errorf("Active() under a clock jump = %+v", c)
	}
}

func Test_passive_active_disabled_and_makes_no_store_write(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	p, clk := newFakePassive(t, fs, WithPassiveEnabled(false))
	if got := p.Active(1, clk.Now()); got != nil || fs.total() != 0 {
		t.Fatalf("disabled Active() = %+v, store calls %d", got, fs.total())
	}

	p2, clk2 := newFakePassive(t, fs)
	p2.Observe(respAt(1, "m", 429, reject5h(clk2.Now().Add(time.Hour)), clk2.Now()))
	flush(t, p2)
	saves := fs.count("SaveQuotaCooldown")
	for i := 0; i < 5; i++ {
		p2.Active(1, clk2.Now())
	}
	flush(t, p2)
	if fs.count("SaveQuotaCooldown") != saves {
		t.Errorf("Active() wrote to the store")
	}
}

func Test_passive_set_onchange(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	p, clk := newFakePassive(t, fs)
	now := clk.Now()
	p.Observe(respAt(1, "m", 429, reject5h(now.Add(time.Hour)), now)) // no callback yet: fine

	var mu sync.Mutex
	var got []int64
	p.SetOnChange(func(id int64) { mu.Lock(); got = append(got, id); mu.Unlock() })
	p.Observe(respAt(2, "m", 429, reject5h(now.Add(time.Hour)), now))
	p.ClearCooldown(2)
	mu.Lock()
	if len(got) != 2 || got[0] != 2 || got[1] != 2 {
		t.Errorf("callbacks = %v, want [2 2]", got)
	}
	mu.Unlock()

	p.SetOnChange(nil)
	p.Observe(respAt(3, "m", 429, reject5h(now.Add(time.Hour)), now)) // must not panic
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Errorf("callback fired after SetOnChange(nil): %v", got)
	}
}

func Test_passive_onchange_throttles_observation_changes_but_not_cooldowns(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	var mu sync.Mutex
	var calls int
	p, clk := newFakePassive(t, fs)
	p.SetOnChange(func(int64) { mu.Lock(); calls++; mu.Unlock() })
	count := func() int { mu.Lock(); defer mu.Unlock(); return calls }
	util := func(u float64, status string) http.Header {
		return hdr(uf+"5h-status", status, uf+"5h-utilization", strconv.FormatFloat(u, 'f', 2, 64))
	}
	at := clk.Now()
	send := func(h http.Header, status int) { p.Observe(respAt(1, "m", status, h, at)) }

	send(util(0.10, "allowed"), 200)
	if count() != 1 {
		t.Fatalf("first observation: %d calls, want 1", count())
	}
	// A stream of utilization changes inside 5 s collapses to nothing more.
	for i := 1; i <= 50; i++ {
		at = at.Add(50 * time.Millisecond)
		send(util(0.10+float64(i)/100, "allowed"), 200)
	}
	if count() != 1 {
		t.Fatalf("stream of changes inside 5 s: %d calls, want 1", count())
	}
	// 5 s after the last notification, the next change gets through.
	at = at.Add(passiveNotifyInterval)
	send(util(0.90, "allowed"), 200)
	if count() != 2 {
		t.Fatalf("after the interval: %d calls, want 2", count())
	}
	// A verdict flip fires immediately, throttle or not.
	at = at.Add(time.Second)
	send(util(0.92, "allowed_warning"), 200)
	if count() != 3 {
		t.Fatalf("verdict flip: %d calls, want 3", count())
	}
	// An unchanged repeat never fires.
	at = at.Add(time.Minute)
	send(util(0.92, "allowed_warning"), 200)
	if count() != 3 {
		t.Fatalf("identical repeat: %d calls, want 3", count())
	}
	// Cooldown start / extension / clear are never throttled.
	before := count()
	rej := func(retryAfter string) http.Header {
		return hdr(uf+"5h-status", "rejected", uf+"5h-utilization", "1.0", "Retry-After", retryAfter)
	}
	at = at.Add(time.Millisecond)
	send(rej("3600"), 429) // starts a cooldown (and flips the verdict)
	at = at.Add(time.Millisecond)
	send(rej("7200"), 429) // extends it; the observation is unchanged
	p.ClearCooldown(1)
	if count() != before+3 {
		t.Fatalf("cooldown start+extend+clear: %d calls, want %d", count(), before+3)
	}
}
