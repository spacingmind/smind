package quota

import (
	"net/http"
	"time"

	"testing"
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
