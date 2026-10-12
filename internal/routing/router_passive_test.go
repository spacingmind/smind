package routing

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/quota"
)

// Passive rejection cooldown routing (ADR-0028 §7, AC64). The fake Reader
// answers Cooldown from memory; the Router must treat it as exhaustion.

func rejected(until time.Time) quota.Cooldown {
	return quota.Cooldown{Until: until, Scope: quota.CooldownAccount, Reason: quota.ReasonRejected}
}

func backoff(until time.Time, model string) quota.Cooldown {
	return quota.Cooldown{Until: until, Scope: quota.CooldownModel, Model: model, Reason: quota.ReasonBackoff}
}

func TestRoute_passive_pool_skips_cooled_account(t *testing.T) {
	t.Parallel()
	r, _, reg, fr, _ := newFakeRouter(t)
	a := newAccount(t, reg, "a")
	b := newAccount(t, reg, "b")
	fr.setCooldown(a, "", rejected(testNow.Add(time.Hour)))

	got, err := r.Route(context.Background(), "s", PolicyPool, []int64{a, b})
	if err != nil || got.ID != b {
		t.Fatalf("Route() = %d, %v; want %d", got.ID, err, b)
	}
}

func TestRoute_passive_hard_policy_error_names_cooldown_end(t *testing.T) {
	t.Parallel()
	r, _, reg, fr, _ := newFakeRouter(t)
	a := newAccount(t, reg, "a")
	until := testNow.Add(90 * time.Minute)
	fr.setCooldown(a, "", rejected(until))

	_, err := r.Route(context.Background(), "s", PolicyHard, []int64{a})
	if err == nil || !strings.Contains(err.Error(), until.Format(time.RFC3339)) {
		t.Fatalf("Route() error = %v; want it to name %s", err, until.Format(time.RFC3339))
	}
	if !strings.Contains(err.Error(), "exhausted") {
		t.Errorf("error %q lost the existing exhausted wording", err)
	}
}

func TestRoute_passive_affinity_replaces_cooled_pinned_account(t *testing.T) {
	t.Parallel()
	r, _, reg, fr, _ := newFakeRouter(t)
	a := newAccount(t, reg, "a")
	b := newAccount(t, reg, "b")
	ctx := context.Background()

	first, err := r.Route(ctx, "sess", PolicyPool, []int64{a, b})
	if err != nil || first.ID != a {
		t.Fatalf("first Route() = %d, %v; want %d", first.ID, err, a)
	}
	fr.setCooldown(a, "", rejected(testNow.Add(time.Hour)))
	second, err := r.Route(ctx, "sess", PolicyPool, []int64{a, b})
	if err != nil || second.ID != b {
		t.Fatalf("second Route() = %d, %v; want pinned %d replaced by %d", second.ID, err, a, b)
	}
}

func TestRoute_passive_model_scoped_cooldown_only_excludes_that_model_in_pool_and_hard(t *testing.T) {
	t.Parallel()
	r, _, reg, fr, _ := newFakeRouter(t)
	a := newAccount(t, reg, "a")
	b := newAccount(t, reg, "b")
	const opus, sonnet = "claude-opus-4-1", "claude-sonnet-4-5"
	fr.setCooldown(a, opus, quota.Cooldown{Until: testNow.Add(time.Hour), Scope: quota.CooldownModel, Model: opus, Reason: quota.ReasonRejected})
	ctx := context.Background()

	if got, err := r.Route(ctx, "p-opus", PolicyPool, []int64{a, b}, WithModel(opus)); err != nil || got.ID != b {
		t.Fatalf("pool opus = %d, %v; want %d", got.ID, err, b)
	}
	if got, err := r.Route(ctx, "p-sonnet", PolicyPool, []int64{a, b}, WithModel(sonnet)); err != nil || got.ID != a {
		t.Fatalf("pool sonnet = %d, %v; want %d", got.ID, err, a)
	}
	if _, err := r.Route(ctx, "h-opus", PolicyHard, []int64{a}, WithModel(opus)); err == nil {
		t.Fatal("hard opus: want exhausted error")
	}
	if got, err := r.Route(ctx, "h-sonnet", PolicyHard, []int64{a}, WithModel(sonnet)); err != nil || got.ID != a {
		t.Fatalf("hard sonnet = %d, %v; want %d", got.ID, err, a)
	}
}

func TestRoute_passive_overage_enabled_does_not_suppress_cooldown(t *testing.T) {
	t.Parallel()
	r, _, reg, fr, _ := newFakeRouter(t)
	a := newAccount(t, reg, "a")
	b := newAccount(t, reg, "b")
	overage := true
	res := exhaustedResult(testNow.Add(time.Hour))
	res.Report.OverageEnabled = &overage
	fr.set(a, res)
	fr.setCooldown(a, "", rejected(testNow.Add(30*time.Minute)))

	got, err := r.Route(context.Background(), "s", PolicyPool, []int64{a, b})
	if err != nil || got.ID != b {
		t.Fatalf("Route() = %d, %v; want %d", got.ID, err, b)
	}
}

func TestRoute_passive_newer_fetched_report_does_not_clear_cooldown(t *testing.T) {
	t.Parallel()
	r, _, reg, fr, clk := newFakeRouter(t)
	a := newAccount(t, reg, "a")
	b := newAccount(t, reg, "b")
	until := testNow.Add(time.Hour)
	fr.setCooldown(a, "", rejected(until))

	clk.Advance(time.Minute)
	fresh := availableResult(window("five_hour", 3, tptr(testNow.Add(4*time.Hour))))
	fresh.Report.FetchedAt = clk.Now()
	fr.set(a, fresh)

	got, err := r.Route(context.Background(), "s", PolicyPool, []int64{a, b})
	if err != nil || got.ID != b {
		t.Fatalf("Route() = %d, %v; want %d (3%% used report must not clear the cooldown)", got.ID, err, b)
	}
}

func TestRoute_passive_cooldown_applies_with_cold_cache_and_polling_cache_unreadable(t *testing.T) {
	t.Parallel()
	until := testNow.Add(time.Hour)
	results := map[string]quota.Result{
		"cold":        {Enabled: true},
		"disabled":    {Enabled: false},
		"error":       {Enabled: true, Report: &quota.Report{Status: quota.StatusError, FetchedAt: testNow}, Stale: true, LastError: "timeout"},
		"unavailable": {Enabled: true, Report: &quota.Report{Status: quota.StatusUnavailable, FetchedAt: testNow}},
	}
	for name, res := range results {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r, _, reg, fr, _ := newFakeRouter(t)
			a := newAccount(t, reg, "a")
			b := newAccount(t, reg, "b")
			fr.set(a, res)
			fr.setCooldown(a, "", rejected(until))
			got, err := r.Route(context.Background(), "s", PolicyPool, []int64{a, b})
			if err != nil || got.ID != b {
				t.Fatalf("Route() = %d, %v; want %d", got.ID, err, b)
			}
		})
	}
}

func TestRoute_passive_cooldown_expires_at_until_without_fetch(t *testing.T) {
	t.Parallel()
	r, _, reg, fr, clk := newFakeRouter(t)
	a := newAccount(t, reg, "a")
	b := newAccount(t, reg, "b")
	until := testNow.Add(10 * time.Minute)
	fr.setCooldown(a, "", rejected(until))
	ctx := context.Background()

	clk.Advance(10*time.Minute - time.Second)
	if got, err := r.Route(ctx, "before", PolicyPool, []int64{a, b}); err != nil || got.ID != b {
		t.Fatalf("before until: Route() = %d, %v; want %d", got.ID, err, b)
	}
	clk.Advance(time.Second) // now == until: eligible again
	if got, err := r.Route(ctx, "at", PolicyPool, []int64{a, b}); err != nil || got.ID != a {
		t.Fatalf("at until: Route() = %d, %v; want %d", got.ID, err, a)
	}
	clk.Advance(time.Hour)
	if got, err := r.Route(ctx, "after", PolicyPool, []int64{a, b}); err != nil || got.ID != a {
		t.Fatalf("after until: Route() = %d, %v; want %d", got.ID, err, a)
	}
	// Recovery is time-based: only the Reader's memory was consulted, one
	// Peek per exhaustion check (never more than one per Cooldown call).
	if p, c := fr.peeks.Load(), fr.cooldownCalls.Load(); p != c {
		t.Errorf("Peek calls = %d, Cooldown calls = %d; the cooldown path must not add Peeks", p, c)
	}
}

func TestRoute_passive_fetched_100_percent_rule_unchanged_by_cooldown_reader(t *testing.T) {
	t.Parallel()
	// A Reader with no cooldowns: the fetched 100% rule behaves as before.
	r, _, reg, fr, _ := newFakeRouter(t)
	a := newAccount(t, reg, "a")
	b := newAccount(t, reg, "b")
	fr.set(a, exhaustedResult(testNow.Add(time.Hour)))
	if got, err := r.Route(context.Background(), "s", PolicyPool, []int64{a, b}); err != nil || got.ID != b {
		t.Fatalf("Route() = %d, %v; want %d", got.ID, err, b)
	}
	fr.set(a, availableResult(window("five_hour", 99, tptr(testNow.Add(time.Hour)))))
	if got, err := r.Route(context.Background(), "s2", PolicyPool, []int64{a, b}); err != nil || got.ID != a {
		t.Fatalf("99%% Route() = %d, %v; want %d", got.ID, err, a)
	}
}

func TestRoute_passive_cooldown_and_fetched_window_combine_to_later_end(t *testing.T) {
	t.Parallel()
	r, _, reg, fr, _ := newFakeRouter(t)
	a := newAccount(t, reg, "a")
	fr.set(a, exhaustedResult(testNow.Add(3*time.Hour)))
	fr.setCooldown(a, "", rejected(testNow.Add(time.Hour)))
	exhausted, until := r.exhaustion(a, "", testNow)
	if !exhausted || !until.Equal(testNow.Add(3*time.Hour)) {
		t.Fatalf("exhaustion() = %v, %v; want true until the later fetched reset", exhausted, until)
	}
	fr.setCooldown(a, "", rejected(testNow.Add(5*time.Hour)))
	if _, until := r.exhaustion(a, "", testNow); !until.Equal(testNow.Add(5 * time.Hour)) {
		t.Fatalf("until = %v; want the later cooldown end", until)
	}
}

func TestRoute_passive_transient_backoff_routes_around_for_that_model_only(t *testing.T) {
	t.Parallel()
	r, _, reg, fr, clk := newFakeRouter(t)
	a := newAccount(t, reg, "a")
	b := newAccount(t, reg, "b")
	const opus, sonnet = "claude-opus-4-1", "claude-sonnet-4-5"
	cd := backoff(testNow.Add(30*time.Second), opus)
	if !cd.Transient() {
		t.Fatal("test cooldown is not transient")
	}
	fr.setCooldown(a, opus, cd)
	ctx := context.Background()

	if got, err := r.Route(ctx, "o", PolicyPool, []int64{a, b}, WithModel(opus)); err != nil || got.ID != b {
		t.Fatalf("opus = %d, %v; want %d", got.ID, err, b)
	}
	if got, err := r.Route(ctx, "s", PolicyPool, []int64{a, b}, WithModel(sonnet)); err != nil || got.ID != a {
		t.Fatalf("sonnet = %d, %v; want %d", got.ID, err, a)
	}
	clk.Advance(31 * time.Second)
	if got, err := r.Route(ctx, "o2", PolicyPool, []int64{a, b}, WithModel(opus)); err != nil || got.ID != a {
		t.Fatalf("opus after back-off = %d, %v; want %d", got.ID, err, a)
	}
}
