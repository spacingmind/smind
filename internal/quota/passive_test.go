package quota

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/store"
)

func Test_passive_rejected_5h_cools_down_until_reset(t *testing.T) {
	t.Parallel()
	pe := newPassiveEnv(t)
	reset := pe.clk.Now().Add(2 * time.Hour)
	pe.observe("claude-sonnet-4", http.StatusTooManyRequests, reject5h(reset))

	want := graceEnd(reset)
	for _, model := range []string{"claude-sonnet-4", "claude-opus-4", ""} {
		got, ok := pe.cooldown(model)
		wantCooldown(t, got, ok, want, CooldownAccount, "", ReasonRejected)
		if got.Level != 0 || got.Transient() {
			t.Errorf("model %q: Level/Transient = %d/%v, want 0/false", model, got.Level, got.Transient())
		}
	}
	// The Poller answers from the same source.
	if c, ok := pe.env.p.Cooldown(pe.id, "claude-opus-4", pe.clk.Now()); !ok || !c.Until.Equal(want) {
		t.Errorf("Poller.Cooldown = %+v/%v, want until %v", c, ok, want)
	}

	if _, ok := pe.p.Cooldown(pe.id, "x", want.Add(-time.Nanosecond)); !ok {
		t.Errorf("Cooldown just before the end = none, want active")
	}
	if _, ok := pe.p.Cooldown(pe.id, "x", want); ok {
		t.Errorf("Cooldown at reset+grace = active, want eligible again")
	}
	if n := pe.f.Calls(); n != 0 {
		t.Errorf("usage fetches = %d, want 0", n)
	}
	if pe.changeCount() != 1 {
		t.Errorf("onChange calls = %d, want 1", pe.changeCount())
	}
}

func Test_passive_7d_oi_only_scopes_model(t *testing.T) {
	t.Parallel()
	pe := newPassiveEnv(t)
	now := pe.clk.Now()
	pe.observe("claude-opus-5", http.StatusTooManyRequests, rejectOIOnly(3600))

	want := now.Add(time.Hour + testGrace)
	got, ok := pe.cooldown(" Claude-Opus-5 ") // case and spaces do not matter
	wantCooldown(t, got, ok, want, CooldownModel, "claude-opus-5", ReasonRejected)
	for _, other := range []string{"claude-sonnet-4", ""} {
		wantNoCooldown(t, pe.p, pe.id, other, now)
	}

	// A credential-wide rejection afterwards cools every model.
	reset := now.Add(2 * time.Hour)
	pe.observe("claude-sonnet-4", http.StatusTooManyRequests, reject5h(reset))
	for _, model := range []string{"claude-sonnet-4", "claude-opus-5", ""} {
		got, ok := pe.cooldown(model)
		wantCooldown(t, got, ok, graceEnd(reset), CooldownAccount, "", ReasonRejected)
	}
}

func Test_passive_bare_429_not_exhausted(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		h    http.Header
	}{
		{"no headers", nil},
		{"5h and 7d allowed", hdr(uf+"5h-status", "allowed", uf+"7d-status", "allowed", uf+"status", "allowed")},
		{"unrelated headers", hdr("X-Request-Id", "abc")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pe := newPassiveEnv(t)
			now := pe.clk.Now()
			pe.observe("claude-opus-4", http.StatusTooManyRequests, tt.h)
			for _, model := range []string{"claude-opus-4", "claude-sonnet-4", ""} {
				wantNoCooldown(t, pe.p, pe.id, model, now)
			}
		})
	}

	t.Run("Retry-After is a short model back-off", func(t *testing.T) {
		t.Parallel()
		pe := newPassiveEnv(t)
		now := pe.clk.Now()
		pe.observe("claude-opus-4", http.StatusTooManyRequests, hdr("Retry-After", "30"))
		got, ok := pe.cooldown("claude-opus-4")
		wantCooldown(t, got, ok, now.Add(30*time.Second), CooldownModel, "claude-opus-4", ReasonBackoff)
		if !got.Transient() {
			t.Errorf("Transient() = false, want true")
		}
		wantNoCooldown(t, pe.p, pe.id, "claude-sonnet-4", now)
		wantNoCooldown(t, pe.p, pe.id, "", now)
		if _, ok := pe.p.Cooldown(pe.id, "claude-opus-4", now.Add(30*time.Second)); ok {
			t.Errorf("back-off still active at its end")
		}
		// It never persists and never calls onChange.
		pe.p.Close()
		rows, err := pe.st.ListQuotaCooldowns(pe.id)
		if err != nil || len(rows) != 0 {
			t.Errorf("persisted rows = %v, %v; want none", rows, err)
		}
		if pe.changeCount() != 0 {
			t.Errorf("onChange calls = %d, want 0 for a back-off", pe.changeCount())
		}
	})

	t.Run("Retry-After 3600 is still capped at 60s", func(t *testing.T) {
		t.Parallel()
		pe := newPassiveEnv(t)
		now := pe.clk.Now()
		pe.observe("claude-opus-4", http.StatusTooManyRequests, hdr("Retry-After", "3600"))
		got, ok := pe.cooldown("claude-opus-4")
		wantCooldown(t, got, ok, now.Add(time.Minute), CooldownModel, "claude-opus-4", ReasonBackoff)
	})

	t.Run("back-off only extends", func(t *testing.T) {
		t.Parallel()
		pe := newPassiveEnv(t)
		now := pe.clk.Now()
		pe.observe("m", http.StatusTooManyRequests, hdr("Retry-After", "30"))
		pe.observe("m", http.StatusTooManyRequests, hdr("Retry-After", "10"))
		got, ok := pe.cooldown("m")
		wantCooldown(t, got, ok, now.Add(30*time.Second), CooldownModel, "m", ReasonBackoff)
		pe.observe("m", http.StatusTooManyRequests, hdr("Retry-After", "45"))
		got, ok = pe.cooldown("m")
		wantCooldown(t, got, ok, now.Add(45*time.Second), CooldownModel, "m", ReasonBackoff)
	})

	t.Run("Retry-After alone on a non-429 does nothing", func(t *testing.T) {
		t.Parallel()
		pe := newPassiveEnv(t)
		pe.observe("m", http.StatusServiceUnavailable, hdr("Retry-After", "30"))
		wantNoCooldown(t, pe.p, pe.id, "m", pe.clk.Now())
	})
}

func Test_passive_success_headers_never_affect_cooldown(t *testing.T) {
	t.Parallel()
	loud := hdr(uf+"status", "rejected", uf+"5h-status", "rejected", uf+"5h-reset", epoch(newFakeClock().Now().Add(time.Hour)),
		uf+"5h-utilization", "1.0", uf+"7d-status", "allowed_warning", uf+"7d-utilization", "0.97", "Retry-After", "600")
	for _, status := range []int{200, 201, 500, 503, 401, 400} {
		pe := newPassiveEnv(t)
		pe.observe("claude-opus-4", status, loud)
		for _, model := range []string{"claude-opus-4", ""} {
			wantNoCooldown(t, pe.p, pe.id, model, pe.clk.Now())
		}
		obs, ok := pe.p.Observed(pe.id)
		if !ok {
			t.Fatalf("status %d: no observation stored for display", status)
		}
		w, found := windowByID(obs, "five_hour")
		if !found || w.Status != "rejected" || w.UsedPct == nil || *w.UsedPct != 100 {
			t.Errorf("status %d: observed five_hour = %+v, want rejected at 100%%", status, w)
		}
	}
}

func Test_passive_cooldown_persists_restart(t *testing.T) {
	t.Parallel()
	pe := newPassiveEnv(t)
	idB, idC := pe.addOAuth("b"), pe.addOAuth("c")
	now := pe.clk.Now()

	// A: credential-wide. B: fallback, escalated to level 2. C: model-scoped.
	reset := now.Add(2 * time.Hour)
	pe.observe("claude-sonnet-4", http.StatusTooManyRequests, reject5h(reset))
	pe.p.Observe(respAt(idC, "claude-opus-5", 429, rejectOIOnly(30*60), now))
	pe.p.Observe(respAt(idB, "m", 429, rejectNoDeadline(), now))
	pe.clk.Advance(time.Minute + time.Second)
	t2 := pe.clk.Now()
	pe.p.Observe(respAt(idB, "m", 429, rejectNoDeadline(), t2))
	wantB := t2.Add(2 * time.Minute)
	wantC := now.Add(30*time.Minute + testGrace)

	pe.p.Close()
	rows, err := pe.st.ListQuotaCooldowns(idB)
	if err != nil || len(rows) != 1 || rows[0].Level != 2 || rows[0].Reason != string(ReasonFallback) {
		t.Fatalf("stored fallback rows = %+v, %v; want level 2 fallback", rows, err)
	}

	pe.restart() // new store handle, new Poller
	p2 := pe.newPassive()
	t.Cleanup(p2.Close)

	got, ok := p2.Cooldown(pe.id, "claude-sonnet-4", t2)
	wantCooldown(t, got, ok, graceEnd(reset), CooldownAccount, "", ReasonRejected)
	got, ok = p2.Cooldown(idC, "claude-opus-5", t2)
	wantCooldown(t, got, ok, wantC, CooldownModel, "claude-opus-5", ReasonRejected)
	wantNoCooldown(t, p2, idC, "claude-sonnet-4", t2) // the model scope survived as a model scope
	got, ok = p2.Cooldown(idB, "anything", t2)
	wantCooldown(t, got, ok, wantB, CooldownAccount, "", ReasonFallback)
	if got.Level != 2 {
		t.Errorf("fallback Level after restart = %d, want 2", got.Level)
	}
	if c, ok := pe.env.p.Cooldown(pe.id, "x", t2); !ok || !c.Until.Equal(graceEnd(reset)) {
		t.Errorf("Poller.Cooldown after restart = %+v/%v", c, ok)
	}

	// Expired entries are ignored after a restart.
	wantNoCooldown(t, p2, idB, "anything", wantB)
	wantNoCooldown(t, p2, idC, "claude-opus-5", wantC)
	wantNoCooldown(t, p2, pe.id, "m", graceEnd(reset))

	// The level continues escalating from the persisted one: 2 -> 3 (4 min).
	pe.clk.Set(wantB.Add(time.Second))
	t3 := pe.clk.Now()
	p2.Observe(respAt(idB, "m", 429, rejectNoDeadline(), t3))
	got, ok = p2.Cooldown(idB, "m", t3)
	wantCooldown(t, got, ok, t3.Add(4*time.Minute), CooldownAccount, "", ReasonFallback)
	if got.Level != 3 {
		t.Errorf("Level = %d, want 3 (continued from the persisted 2)", got.Level)
	}
}

func Test_passive_zero_http_calls(t *testing.T) {
	srv := newUsageServer(t)
	src := &lateSource{}
	e := newEnv(t, func(reg *accounts.Registry, clk *fakeClock) ReportFetcher { return a3aFetcher(srv, reg, clk) }, WithCooldowns(src))
	id := e.addOAuth("tok")
	p := NewPassive(e.st, WithPassiveClock(e.clk.Now), WithPassiveGrace(fixedGrace))
	t.Cleanup(p.Close)
	src.p.Store(p)

	before, _, err := e.st.GetQuotaPollState(id)
	if err != nil {
		t.Fatalf("GetQuotaPollState: %v", err)
	}

	long := strings.Repeat("x", 600)
	many := http.Header{}
	for i := 0; i < 100; i++ {
		many.Set("anthropic-ratelimit-unified-extra-"+string(rune('a'+i%26))+string(rune('a'+i/26)), "1")
	}
	shapes := []struct {
		status int
		h      http.Header
	}{
		{429, reject5h(e.clk.Now().Add(time.Hour))},
		{429, rejectOIOnly(600)},
		{429, rejectNoDeadline()},
		{429, hdr("Retry-After", "30")},
		{429, nil},
		{200, hdr(uf+"5h-status", "allowed_warning", uf+"5h-utilization", "0.9")},
		{200, nil},
		{500, hdr(uf+"status", "rejected")},
		{429, hdr(uf+"5h-status", "rejected", uf+"5h-reset", long, "Retry-After", "bad\r\nvalue")},
		{200, hdr(uf+"5h-utilization", "NaN", uf+"7d-utilization", "-1e999", uf+"7d-reset", "0")},
		{429, many},
		{0, http.Header{"": {"x"}, uf + "status": nil}},
	}
	for i := 0; i < 1000; i++ {
		s := shapes[i%len(shapes)]
		prov := "anthropic"
		if i%97 == 0 {
			prov = "openai"
		}
		p.Observe(Response{AccountID: id, Provider: prov, Model: "claude-opus-4", Status: s.status, Header: s.h, At: e.clk.Now()})
		e.p.Cooldown(id, "claude-opus-4", e.clk.Now()) // through the Poller
		p.Observed(id)
		if i%50 == 0 {
			e.clk.Advance(time.Minute)
		}
	}
	flush(t, p)

	a3aWantCalls(t, srv, 0, "after 1000 observed responses")
	after, _, err := e.st.GetQuotaPollState(id)
	if err != nil {
		t.Fatalf("GetQuotaPollState: %v", err)
	}
	if !pollStateEqual(before, after) {
		t.Errorf("quota_poll_state changed: %+v -> %+v", before, after)
	}
}

func pollStateEqual(a, b store.QuotaPollState) bool { return reflect.DeepEqual(a, b) }

func Test_passive_reset_in_the_past_or_garbage_falls_back_to_exponential_1_to_30_min(t *testing.T) {
	t.Parallel()
	now := newFakeClock().Now()
	variants := map[string]http.Header{
		"reset in the past": hdr(uf+"5h-status", "rejected", uf+"5h-reset", epoch(now.Add(-time.Hour))),
		"garbage reset":     rejectNoDeadline(),
		"no reset at all":   hdr(uf+"5h-status", "rejected"),
		"retry-after zero":  hdr(uf+"7d-status", "rejected", "Retry-After", "0"),
	}
	wantMin := []time.Duration{1, 2, 4, 8, 16, 30, 30, 30}
	for name, h := range variants {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			pe := newPassiveEnv(t)
			for i, want := range wantMin {
				at := pe.clk.Now()
				pe.observe("m", 429, h)
				got, ok := pe.cooldown("m")
				wantCooldown(t, got, ok, at.Add(want*time.Minute), CooldownAccount, "", ReasonFallback)
				if got.Level != min(i+1, 6) {
					t.Errorf("step %d: Level = %d, want %d", i, got.Level, min(i+1, 6))
				}
				pe.clk.Set(got.Until.Add(time.Second))
			}
		})
	}
}

func Test_passive_fallback_level_resets_after_a_non_rejected_response(t *testing.T) {
	t.Parallel()
	for _, reset := range []struct {
		name   string
		status int
		h      http.Header
	}{
		{"200", 200, nil},
		{"bare 429", 429, hdr("Retry-After", "5")},
		{"500", 500, nil},
	} {
		t.Run(reset.name, func(t *testing.T) {
			t.Parallel()
			pe := newPassiveEnv(t)
			for want := 1; want <= 2; want++ {
				pe.observe("m", 429, rejectNoDeadline())
				got, _ := pe.cooldown("m")
				if got.Level != want {
					t.Fatalf("Level = %d, want %d", got.Level, want)
				}
				pe.clk.Set(got.Until.Add(time.Second))
			}

			// A non-rejected response while a cooldown is active resets the
			// level but neither clears nor shortens the cooldown.
			pe.observe("m", 429, rejectNoDeadline())
			active, _ := pe.cooldown("m")
			pe.observe("m", reset.status, reset.h)
			got, ok := pe.cooldown("m")
			wantCooldown(t, got, ok, active.Until, CooldownAccount, "", ReasonFallback)
			if got.Level != 0 {
				t.Errorf("Level after a non-rejected response = %d, want 0", got.Level)
			}

			// The next fallback rejection starts again at level 1.
			pe.clk.Set(active.Until.Add(time.Second))
			pe.observe("m", 429, rejectNoDeadline())
			got, _ = pe.cooldown("m")
			if got.Level != 1 {
				t.Errorf("Level after the reset = %d, want 1", got.Level)
			}
		})
	}
}

func Test_passive_fallback_level_reset_is_persisted_and_expired_row_removed(t *testing.T) {
	t.Parallel()
	pe := newPassiveEnv(t)
	pe.observe("m", 429, rejectNoDeadline())
	got, _ := pe.cooldown("m")
	pe.clk.Set(got.Until.Add(time.Second))
	pe.observe("m", 200, nil) // expired + level reset: the row goes away
	pe.p.Close()
	rows, err := pe.st.ListQuotaCooldowns(pe.id)
	if err != nil || len(rows) != 0 {
		t.Fatalf("rows = %+v, %v; want none after an expired row was reset", rows, err)
	}
}

func Test_passive_inflight_second_rejection_does_not_escalate(t *testing.T) {
	t.Parallel()
	pe := newPassiveEnv(t)
	now := pe.clk.Now()
	pe.observe("m", 429, rejectNoDeadline())
	for i := 0; i < 5; i++ {
		pe.clk.Advance(time.Second)
		pe.observe("m", 429, rejectNoDeadline())
	}
	got, ok := pe.cooldown("m")
	wantCooldown(t, got, ok, now.Add(time.Minute), CooldownAccount, "", ReasonFallback)
	if got.Level != 1 {
		t.Errorf("Level = %d, want 1 (no escalation while active)", got.Level)
	}
	pe.p.Close()
	rows, _ := pe.st.ListQuotaCooldowns(pe.id)
	if len(rows) != 1 || rows[0].Level != 1 {
		t.Errorf("stored rows = %+v, want one at level 1", rows)
	}
}

func Test_passive_never_shortens_active_cooldown(t *testing.T) {
	t.Parallel()
	pe := newPassiveEnv(t)
	now := pe.clk.Now()
	long := now.Add(2 * time.Hour)
	pe.observe("m", 429, reject5h(long))
	pe.observe("m", 429, reject5h(now.Add(30*time.Minute)))
	got, ok := pe.cooldown("m")
	wantCooldown(t, got, ok, graceEnd(long), CooldownAccount, "", ReasonRejected)

	pe.observe("m", 429, rejectNoDeadline()) // fallback never replaces a running cooldown
	got, _ = pe.cooldown("m")
	if !got.Until.Equal(graceEnd(long)) || got.Reason != ReasonRejected {
		t.Errorf("after a fallback rejection: %+v, want unchanged", got)
	}

	longer := now.Add(3 * time.Hour)
	pe.observe("m", 429, reject5h(longer))
	got, ok = pe.cooldown("m")
	wantCooldown(t, got, ok, graceEnd(longer), CooldownAccount, "", ReasonRejected)

}

func Test_passive_never_shortens_model_cooldown(t *testing.T) {
	t.Parallel()
	pe := newPassiveEnv(t)
	now := pe.clk.Now()
	pe.observe("opus", 429, rejectOIOnly(7200))
	pe.observe("opus", 429, rejectOIOnly(60))
	got, ok := pe.cooldown("opus")
	wantCooldown(t, got, ok, now.Add(2*time.Hour+testGrace), CooldownModel, "opus", ReasonRejected)
	pe.p.Close()
	rows, _ := pe.st.ListQuotaCooldowns(pe.id)
	if len(rows) != 1 || !rows[0].Until.Equal(now.Add(2*time.Hour+testGrace)) {
		t.Errorf("stored rows = %+v, want one until now+2h+grace", rows)
	}
}

func Test_passive_credential_change_clears_cooldown(t *testing.T) {
	t.Parallel()
	t.Run("clears memory and stored rows", func(t *testing.T) {
		t.Parallel()
		pe := newPassiveEnv(t)
		now := pe.clk.Now()
		pe.observe("m", 429, reject5h(now.Add(time.Hour)))
		pe.observe("opus", 429, rejectOIOnly(600))
		pe.observe("m", 429, hdr("Retry-After", "30"))
		flush(t, pe.p)
		before := pe.changeCount()

		pe.p.ClearCooldown(pe.id)
		for _, model := range []string{"m", "opus", ""} {
			wantNoCooldown(t, pe.p, pe.id, model, now)
		}
		rows, err := pe.st.ListQuotaCooldowns(pe.id)
		if err != nil || len(rows) != 0 {
			t.Errorf("stored rows = %+v, %v; want none", rows, err)
		}
		if pe.changeCount() != before+1 {
			t.Errorf("onChange calls = %d, want %d", pe.changeCount(), before+1)
		}

		// A later rejection works again.
		pe.observe("m", 429, reject5h(now.Add(time.Hour)))
		if _, ok := pe.cooldown("m"); !ok {
			t.Errorf("no cooldown after a rejection that follows a clear")
		}
		pe.p.Close()
		if rows, _ := pe.st.ListQuotaCooldowns(pe.id); len(rows) != 1 {
			t.Errorf("stored rows after new rejection = %d, want 1", len(rows))
		}
	})

	t.Run("an in-flight and a queued write cannot resurrect it", func(t *testing.T) {
		t.Parallel()
		fs := newFakeStore()
		fs.saveGate, fs.saveEnter = make(chan struct{}), make(chan struct{}, 8)
		p, clk := newFakePassive(t, fs)
		now := clk.Now()
		p.Observe(respAt(7, "opus", 429, rejectOIOnly(600), now)) // in flight, parked in the store
		a3aWait(t, fs.saveEnter, 1, "the first write to park")
		p.Observe(respAt(7, "m", 429, reject5h(now.Add(time.Hour)), now)) // queued behind it

		p.ClearCooldown(7)
		close(fs.saveGate)
		flush(t, p)
		if n := fs.rowCount(); n != 0 {
			t.Fatalf("store rows after clear = %d, want 0 (undone / dropped)", n)
		}
		wantNoCooldown(t, p, 7, "m", now)
		wantNoCooldown(t, p, 7, "opus", now)

		// And a fresh rejection is persisted normally.
		p.Observe(respAt(7, "m", 429, reject5h(now.Add(time.Hour)), now))
		flush(t, p)
		if _, ok := fs.row(7, ""); !ok {
			t.Errorf("a rejection after the clear was not persisted")
		}
	})
}

func Test_passive_newer_fetched_report_does_not_clear_cooldown(t *testing.T) {
	t.Parallel()
	pe := newPassiveEnv(t)
	reset := pe.clk.Now().Add(3 * time.Hour)
	pe.observe("m", 429, reject5h(reset))

	res := pe.touch(pe.id) // a fetched report, 10% used
	if res.Report == nil || res.Report.Status != StatusAvailable {
		t.Fatalf("Touch() = %+v, want an available report", res)
	}
	pe.clk.Advance(6 * time.Minute)
	res = pe.touch(pe.id)
	if pe.f.Calls() < 2 || res.Report == nil {
		t.Fatalf("expected a second, newer fetched report")
	}
	got, ok := pe.env.p.Cooldown(pe.id, "m", pe.clk.Now())
	wantCooldown(t, got, ok, graceEnd(reset), CooldownAccount, "", ReasonRejected)
}

func Test_passive_overage_enabled_does_not_suppress_cooldown(t *testing.T) {
	t.Parallel()
	pe := newPassiveEnv(t)
	on := true
	pe.f.fn = func(_ context.Context, _ accounts.Account) (Report, error) {
		r := availableTestReport()
		r.OverageEnabled = &on
		return r, nil
	}
	reset := pe.clk.Now().Add(time.Hour)
	pe.observe("m", 429, reject5h(reset))
	res := pe.touch(pe.id)
	if res.Report == nil || res.Report.OverageEnabled == nil || !*res.Report.OverageEnabled {
		t.Fatalf("fetched report = %+v, want overage enabled", res.Report)
	}
	got, ok := pe.env.p.Cooldown(pe.id, "m", pe.clk.Now())
	wantCooldown(t, got, ok, graceEnd(reset), CooldownAccount, "", ReasonRejected)
}

func Test_passive_model_oi_only_with_unknown_model_not_recorded(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	p, clk := newFakePassive(t, fs)
	now := clk.Now()
	for _, model := range []string{"", "   ", strings.Repeat("m", 129)} {
		p.Observe(respAt(3, model, 429, rejectOIOnly(600), now))
	}
	for _, model := range []string{"", "claude-opus-5", "anything"} {
		wantNoCooldown(t, p, 3, model, now)
	}
	if n := p.unnamedModelOnly.Load(); n != 3 {
		t.Errorf("unnamed model-only rejections counted = %d, want 3", n)
	}
	p.Close()
	if n := fs.count("SaveQuotaCooldown"); n != 0 {
		t.Errorf("cooldown writes = %d, want 0", n)
	}
}

// guardStore fails the test on any call.
type guardStore struct {
	fakeStore
	t *testing.T
}

func (g *guardStore) SaveQuotaCooldown(store.QuotaCooldown) error {
	g.t.Error("store call: SaveQuotaCooldown")
	return nil
}
func (g *guardStore) ListQuotaCooldowns(int64) ([]store.QuotaCooldown, error) {
	g.t.Error("store call: ListQuotaCooldowns")
	return nil, nil
}
func (g *guardStore) DeleteQuotaCooldowns(int64) error {
	g.t.Error("store call: DeleteQuotaCooldowns")
	return nil
}
func (g *guardStore) DeleteQuotaCooldown(int64, string) error {
	g.t.Error("store call: DeleteQuotaCooldown")
	return nil
}
func (g *guardStore) ReplaceQuotaSnapshot(store.QuotaSnapshot) (store.QuotaSnapshot, error) {
	g.t.Error("store call: ReplaceQuotaSnapshot")
	return store.QuotaSnapshot{}, nil
}
func (g *guardStore) GetLatestQuotaSnapshotKind(int64, string) (store.QuotaSnapshot, bool, error) {
	g.t.Error("store call: GetLatestQuotaSnapshotKind")
	return store.QuotaSnapshot{}, false, nil
}

func Test_passive_disabled_is_noop(t *testing.T) {
	t.Parallel()
	gs := &guardStore{fakeStore: *newFakeStore(), t: t}
	p := NewPassive(gs, WithPassiveEnabled(false), WithPassiveOnChange(func(int64) { t.Error("onChange fired") }))
	now := time.Now()
	p.Observe(respAt(1, "m", 429, reject5h(now.Add(time.Hour)), now))
	p.Observe(respAt(1, "m", 200, hdr(uf+"5h-utilization", "0.5"), now))
	if c, ok := p.Cooldown(1, "m", now); ok {
		t.Errorf("Cooldown() = %+v, want none while disabled", c)
	}
	if _, ok := p.Observed(1); ok {
		t.Errorf("Observed() = ok while disabled")
	}
	p.ClearCooldown(1)
	p.Close()
	if p.started {
		t.Errorf("writer goroutine started while disabled")
	}
}

func Test_observer_disabled_by_config_and_by_enabled_false(t *testing.T) {
	t.Parallel()
	// accountUsage.passive: false -> the wiring builds a disabled observer.
	t.Run("passive disabled", func(t *testing.T) {
		t.Parallel()
		pe := newPassiveEnv(t, WithPassiveEnabled(false))
		pe.observe("m", 429, reject5h(pe.clk.Now().Add(time.Hour)))
		wantNoCooldown(t, pe.p, pe.id, "m", pe.clk.Now())
		if _, ok := pe.env.p.Cooldown(pe.id, "m", pe.clk.Now()); ok {
			t.Errorf("Poller.Cooldown answered from a disabled observer")
		}
		pe.p.Close()
		if rows, _ := pe.st.ListQuotaCooldowns(pe.id); len(rows) != 0 {
			t.Errorf("rows persisted by a disabled observer: %+v", rows)
		}
	})
	// accountUsage.enabled: false -> the Poller answers nothing even when the
	// source has a cooldown.
	t.Run("poller disabled", func(t *testing.T) {
		t.Parallel()
		fs := newFakeStore()
		p, clk := newFakePassive(t, fs)
		p.Observe(respAt(1, "m", 429, reject5h(clk.Now().Add(time.Hour)), clk.Now()))
		poller := New(nil, nil, &fakeReportFetcher{}, WithEnabled(false), WithCooldowns(p))
		if c, ok := poller.Cooldown(1, "m", clk.Now()); ok {
			t.Errorf("disabled Poller.Cooldown = %+v, want none", c)
		}
		on := New(nil, nil, &fakeReportFetcher{}, WithCooldowns(p))
		if _, ok := on.Cooldown(1, "m", clk.Now()); !ok {
			t.Errorf("enabled Poller.Cooldown = none, want the cooldown")
		}
	})
}

func Test_passive_other_providers_ignored(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	p := NewPassive(fs)
	now := time.Now()
	for _, prov := range []string{"openai", "", "Anthropic"} {
		p.Observe(Response{AccountID: 1, Provider: prov, Model: "m", Status: 429, Header: reject5h(now.Add(time.Hour)), At: now})
	}
	wantNoCooldown(t, p, 1, "m", now)
	p.Close()
	if p.started || fs.count("SaveQuotaCooldown")+fs.count("ReplaceQuotaSnapshot") != 0 {
		t.Errorf("another provider's response started a writer or a write")
	}
}

func Test_passive_clock_jump_clamps_to_8_days(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	p, clk := newFakePassive(t, fs)
	now := clk.Now()

	// A persisted row far in the future (clock went backwards since).
	fs.rows[cooldownKey{1, ""}] = store.QuotaCooldown{AccountID: 1, Until: now.Add(30 * 24 * time.Hour), Reason: string(ReasonRejected), ObservedAt: now}
	got, ok := p.Cooldown(1, "m", now)
	wantCooldown(t, got, ok, now.Add(MaxCooldown), CooldownAccount, "", ReasonRejected)

	// A cooldown set now, read after the clock jumped back by 20 days.
	p.Observe(respAt(2, "m", 429, reject5h(now.Add(7*24*time.Hour)), now))
	back := now.Add(-20 * 24 * time.Hour)
	got, ok = p.Cooldown(2, "m", back)
	wantCooldown(t, got, ok, back.Add(MaxCooldown), CooldownAccount, "", ReasonRejected)

	// A garbage reset far in the future is clamped when recorded.
	p.Observe(respAt(3, "m", 429, hdr(uf+"5h-status", "rejected", uf+"5h-reset", "999999999999"), now))
	got, ok = p.Cooldown(3, "m", now)
	wantCooldown(t, got, ok, now.Add(MaxCooldown), CooldownAccount, "", ReasonRejected)
}

func Test_passive_cooldown_read_error_not_cached(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	now := newFakeClock().Now()
	fs.rows[cooldownKey{1, ""}] = store.QuotaCooldown{AccountID: 1, Until: now.Add(time.Hour), Reason: string(ReasonRejected), ObservedAt: now}
	fs.setFail(false, true)
	p, _ := newFakePassive(t, fs)

	if c, ok := p.Cooldown(1, "m", now); ok {
		t.Fatalf("Cooldown() with a failing store = %+v, want none from memory", c)
	}
	fs.setFail(false, false)
	if _, ok := p.Cooldown(1, "m", now); !ok {
		t.Fatalf("Cooldown() after the store recovered = none, want the persisted row (read error must not be cached)")
	}
	reads := fs.count("ListQuotaCooldowns")
	p.Cooldown(1, "m", now)
	p.Cooldown(1, "other", now)
	if n := fs.count("ListQuotaCooldowns"); n != reads {
		t.Errorf("reads grew from %d to %d after a successful load, want cached", reads, n)
	}
}

func Test_passive_write_error_is_logged_and_retried_on_next_write(t *testing.T) {
	fs := newFakeStore()
	fs.errText = "boom SENTINEL-ERR-TEXT"
	fs.setFail(true, false)
	logs, restore := captureLog()
	defer restore()

	p, clk := newFakePassive(t, fs)
	now := clk.Now()
	p.Observe(respAt(4, "m", 429, reject5h(now.Add(time.Hour)), now))
	flush(t, p)
	if _, ok := p.Cooldown(4, "m", now); !ok {
		t.Fatalf("a failed write dropped the cooldown from memory")
	}
	if _, ok := fs.row(4, ""); ok {
		t.Fatalf("row stored despite a failing store")
	}

	fs.setFail(false, false)
	p.Observe(respAt(4, "other", 429, rejectOIOnly(600), now)) // the next write retries the failed row too
	flush(t, p)
	if _, ok := fs.row(4, ""); !ok {
		t.Errorf("failed cooldown row was not retried with the next write")
	}
	out := logs.String()
	if !strings.Contains(out, "account 4 passive cooldown write failed") {
		t.Errorf("no fixed-string log line for the failed write; log = %q", out)
	}
	if strings.Contains(out, "SENTINEL-ERR-TEXT") || strings.Contains(out, "boom") {
		t.Errorf("store error text leaked into the log: %q", out)
	}
}

func Test_passive_onchange_outside_locks_and_only_for_visible_changes(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	var p *Passive
	var mu sync.Mutex
	var calls []int64
	p, clk := newFakePassive(t, fs, WithPassiveOnChange(func(id int64) {
		// Calling back into Passive deadlocks if any lock were still held.
		p.Cooldown(id, "m", clk0())
		p.Observed(id)
		mu.Lock()
		calls = append(calls, id)
		mu.Unlock()
	}))
	now := clk.Now()
	count := func() int { mu.Lock(); defer mu.Unlock(); return len(calls) }

	// Retry-After carries the deadline, so the observation (which ignores it)
	// stays identical and only the cooldown decides whether onChange fires.
	rej := func(retryAfter string) http.Header {
		return hdr(uf+"5h-status", "rejected", "Retry-After", retryAfter)
	}
	p.Observe(respAt(1, "m", 429, rej("3600"), now))
	if count() != 1 {
		t.Fatalf("onChange after a new cooldown = %d, want 1", count())
	}
	p.Observe(respAt(1, "m", 429, rej("1800"), now)) // shorter: ignored
	if count() != 1 {
		t.Errorf("onChange fired for a rejection that changed nothing")
	}
	p.Observe(respAt(1, "m", 429, rej("7200"), now)) // extends
	if count() != 2 {
		t.Errorf("onChange after an extension = %d, want 2", count())
	}
	p.Observe(respAt(1, "m", 429, hdr("Retry-After", "10"), now)) // back-off: silent
	p.Observe(respAt(1, "m", 200, nil, now))
	if count() != 2 {
		t.Errorf("onChange fired for a back-off or a plain response")
	}
	h := hdr(uf+"5h-status", "allowed", uf+"5h-utilization", "0.5")
	p.Observe(respAt(1, "m", 200, h, now))
	p.Observe(respAt(1, "m", 200, h, now.Add(time.Second))) // identical values
	if count() != 3 {
		t.Errorf("onChange for observation changes = %d, want 3 (one for the first, none for the repeat)", count())
	}
	p.ClearCooldown(1)
	if count() != 4 {
		t.Errorf("onChange after ClearCooldown = %d, want 4", count())
	}
}

func clk0() time.Time { return time.Now() }

func Test_passive_close_is_idempotent_and_drains(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	p, clk := newFakePassive(t, fs)
	now := clk.Now()
	for i := int64(1); i <= 20; i++ {
		p.Observe(respAt(i, "m", 429, reject5h(now.Add(time.Hour)), now))
	}
	p.Close()
	p.Close()
	if n := fs.rowCount(); n != 20 {
		t.Errorf("rows after Close = %d, want 20 (drained)", n)
	}
	// After Close, writes are skipped but memory still answers and nothing blocks.
	p.Observe(respAt(99, "m", 429, reject5h(now.Add(time.Hour)), now))
	if _, ok := p.Cooldown(99, "m", now); !ok {
		t.Errorf("memory not updated after Close")
	}
	if _, ok := fs.row(99, ""); ok {
		t.Errorf("write accepted after Close")
	}
	NewPassive(fs).Close() // never started: Close returns at once
}

func Test_passive_header_not_retained(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	p, clk := newFakePassive(t, fs)
	now := clk.Now()
	h := reject5h(now.Add(time.Hour))
	p.Observe(respAt(1, "m", 429, h, now))
	for k := range h {
		delete(h, k)
	}
	h.Set(uf+"5h-status", "allowed")
	if _, ok := p.Cooldown(1, "m", now); !ok {
		t.Errorf("cooldown lost after the caller reused the header map")
	}
}

func Test_passive_concurrent_observe_and_cooldown_race(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	var changes sync.WaitGroup
	_ = &changes
	p, clk := newFakePassive(t, fs, WithPassiveOnChange(func(int64) {}))
	now := clk.Now()
	shapes := []struct {
		status int
		h      http.Header
	}{
		{429, reject5h(now.Add(time.Hour))},
		{429, rejectOIOnly(300)},
		{429, rejectNoDeadline()},
		{429, hdr("Retry-After", "20")},
		{200, hdr(uf+"5h-status", "allowed", uf+"5h-utilization", "0.4")},
		{200, nil},
	}
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 300; i++ {
				id := int64(1 + (g+i)%3)
				s := shapes[(g+i)%len(shapes)]
				at := now.Add(time.Duration(i) * time.Second)
				switch g % 4 {
				case 0, 1:
					p.Observe(respAt(id, "m"+string(rune('a'+i%3)), s.status, s.h, at))
				case 2:
					p.Cooldown(id, "ma", at)
					p.Observed(id)
				case 3:
					if i%50 == 0 {
						p.ClearCooldown(id)
					} else {
						p.Cooldown(id, "mb", at)
					}
				}
			}
		}()
	}
	wg.Wait()
	p.Close()
}

func Test_passive_never_shortens_persisted_cooldown_after_restart(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	p, clk := newFakePassive(t, fs)
	now := clk.Now()
	long := now.Add(72 * time.Hour)
	fs.rows[cooldownKey{9, ""}] = store.QuotaCooldown{AccountID: 9, Until: long, Reason: string(ReasonRejected), ObservedAt: now}

	// A fresh Passive on the same store: nothing loaded yet, and the first thing
	// it sees is a 429 that declares a 10 s reset.
	p.Observe(respAt(9, "m", 429, reject5h(now.Add(10*time.Second)), now))
	flush(t, p)
	if r, ok := fs.row(9, ""); !ok || r.Until.Before(long) {
		t.Fatalf("stored row = %+v/%v, want it not shortened below %v", r, ok, long)
	}
	got, ok := p.Cooldown(9, "m", now)
	if !ok || got.Until.Before(long) {
		t.Fatalf("Cooldown() = %+v/%v, want until >= %v", got, ok, long)
	}
}
