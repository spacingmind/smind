package routing

import (
	"context"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

// A transient bare-429 back-off must not rewrite session affinity; real
// exhaustion or a real cooldown still does.

func decisionsFor(t *testing.T, s *store.Store, key string) []store.RoutingDecision {
	t.Helper()
	all, err := s.ListRoutingDecisions()
	if err != nil {
		t.Fatalf("ListRoutingDecisions() error = %v", err)
	}
	var out []store.RoutingDecision
	for _, d := range all {
		if d.SessionKey == key {
			out = append(out, d)
		}
	}
	return out
}

func TestRoute_affinity_bare_429_backoff_does_not_rewrite_affinity(t *testing.T) {
	t.Parallel()
	r, s, reg, fr, clk := newFakeRouter(t)
	a := newAccount(t, reg, "a")
	b := newAccount(t, reg, "b")
	ctx := context.Background()

	if got, err := r.Route(ctx, "sess", PolicyPool, []int64{a, b}); err != nil || got.ID != a {
		t.Fatalf("first Route() = %d, %v; want %d", got.ID, err, a)
	}
	pinned := decisionsFor(t, s, "sess")
	if len(pinned) != 1 {
		t.Fatalf("decisions after first Route = %d; want 1", len(pinned))
	}

	fr.setCooldown(a, "", backoff(testNow.Add(30*time.Second), ""))
	if got, err := r.Route(ctx, "sess", PolicyPool, []int64{a, b}); err != nil || got.ID != b {
		t.Fatalf("during back-off Route() = %d, %v; want %d", got.ID, err, b)
	}
	after := decisionsFor(t, s, "sess")
	if len(after) != 1 || after[0].AccountID != a || !after[0].ExpiresAt.Equal(pinned[0].ExpiresAt) {
		t.Fatalf("decisions = %+v; want the original pin to %d untouched", after, a)
	}

	clk.Advance(31 * time.Second)
	if got, err := r.Route(ctx, "sess", PolicyPool, []int64{a, b}); err != nil || got.ID != a {
		t.Fatalf("after back-off Route() = %d, %v; want %d", got.ID, err, a)
	}
	if n := len(decisionsFor(t, s, "sess")); n != 1 {
		t.Errorf("decisions = %d; want still 1", n)
	}
}

func TestRoute_affinity_cooldown_still_moves_affinity(t *testing.T) {
	t.Parallel()
	r, s, reg, fr, _ := newFakeRouter(t)
	a := newAccount(t, reg, "a")
	b := newAccount(t, reg, "b")
	ctx := context.Background()

	if _, err := r.Route(ctx, "sess", PolicyPool, []int64{a, b}); err != nil {
		t.Fatalf("first Route() error = %v", err)
	}
	fr.setCooldown(a, "", rejected(testNow.Add(time.Hour)))
	if got, err := r.Route(ctx, "sess", PolicyPool, []int64{a, b}); err != nil || got.ID != b {
		t.Fatalf("Route() = %d, %v; want %d", got.ID, err, b)
	}
	ds := decisionsFor(t, s, "sess")
	if len(ds) != 2 {
		t.Fatalf("decisions = %d; want the move persisted", len(ds))
	}
	if got, _, _ := r.latestDecision("sess"); got.AccountID != b {
		t.Errorf("latest decision account = %d; want %d", got.AccountID, b)
	}
}

func TestRoute_affinity_fetched_exhaustion_still_moves_affinity(t *testing.T) {
	t.Parallel()
	r, s, reg, fr, _ := newFakeRouter(t)
	a := newAccount(t, reg, "a")
	b := newAccount(t, reg, "b")
	ctx := context.Background()

	if _, err := r.Route(ctx, "sess", PolicyPool, []int64{a, b}); err != nil {
		t.Fatalf("first Route() error = %v", err)
	}
	fr.set(a, exhaustedResult(testNow.Add(time.Hour)))
	if got, err := r.Route(ctx, "sess", PolicyPool, []int64{a, b}); err != nil || got.ID != b {
		t.Fatalf("Route() = %d, %v; want %d", got.ID, err, b)
	}
	if n := len(decisionsFor(t, s, "sess")); n != 2 {
		t.Fatalf("decisions = %d; want the move persisted", n)
	}
}

func TestRoute_affinity_transient_plus_fetched_exhaustion_is_real_exhaustion(t *testing.T) {
	t.Parallel()
	r, s, reg, fr, _ := newFakeRouter(t)
	a := newAccount(t, reg, "a")
	b := newAccount(t, reg, "b")
	ctx := context.Background()

	if _, err := r.Route(ctx, "sess", PolicyPool, []int64{a, b}); err != nil {
		t.Fatalf("first Route() error = %v", err)
	}
	fr.set(a, exhaustedResult(testNow.Add(time.Hour)))
	fr.setCooldown(a, "", backoff(testNow.Add(30*time.Second), ""))
	if got, err := r.Route(ctx, "sess", PolicyPool, []int64{a, b}); err != nil || got.ID != b {
		t.Fatalf("Route() = %d, %v; want %d", got.ID, err, b)
	}
	if n := len(decisionsFor(t, s, "sess")); n != 2 {
		t.Fatalf("decisions = %d; want the move persisted", n)
	}
}

func TestRoute_affinity_transient_backoff_other_model_keeps_affinity_untouched(t *testing.T) {
	t.Parallel()
	r, s, reg, fr, _ := newFakeRouter(t)
	a := newAccount(t, reg, "a")
	b := newAccount(t, reg, "b")
	const opus, sonnet = "claude-opus-4-1", "claude-sonnet-4-5"
	ctx := context.Background()

	if _, err := r.Route(ctx, "sess", PolicyPool, []int64{a, b}, WithModel(sonnet)); err != nil {
		t.Fatalf("first Route() error = %v", err)
	}
	fr.setCooldown(a, opus, backoff(testNow.Add(30*time.Second), opus))
	if got, err := r.Route(ctx, "sess", PolicyPool, []int64{a, b}, WithModel(sonnet)); err != nil || got.ID != a {
		t.Fatalf("sonnet Route() = %d, %v; want pinned %d", got.ID, err, a)
	}
	if n := len(decisionsFor(t, s, "sess")); n != 1 {
		t.Errorf("decisions = %d; want 1", n)
	}
	// The opus request routes around the back-off without moving the pin.
	if got, err := r.Route(ctx, "sess", PolicyPool, []int64{a, b}, WithModel(opus)); err != nil || got.ID != b {
		t.Fatalf("opus Route() = %d, %v; want %d", got.ID, err, b)
	}
	if ds := decisionsFor(t, s, "sess"); len(ds) != 1 || ds[0].AccountID != a {
		t.Errorf("decisions = %+v; want the pin to %d untouched", ds, a)
	}
}

func TestRoute_affinity_no_decision_session_picks_second_candidate_and_persists_it(t *testing.T) {
	t.Parallel()
	r, s, reg, fr, _ := newFakeRouter(t)
	a := newAccount(t, reg, "a")
	b := newAccount(t, reg, "b")
	fr.setCooldown(a, "", backoff(testNow.Add(30*time.Second), ""))

	if got, err := r.Route(context.Background(), "new", PolicyPool, []int64{a, b}); err != nil || got.ID != b {
		t.Fatalf("Route() = %d, %v; want %d", got.ID, err, b)
	}
	if ds := decisionsFor(t, s, "new"); len(ds) != 1 || ds[0].AccountID != b {
		t.Fatalf("decisions = %+v; want one decision for %d", ds, b)
	}
}
