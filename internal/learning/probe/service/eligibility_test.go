package probesvc

import (
	"context"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/store"
)

func codes(e Estimate) []string {
	out := []string{}
	for _, r := range e.Reasons {
		out = append(out, r.Code)
	}
	return out
}

func hasCode(e Estimate, code string) bool {
	for _, r := range e.Reasons {
		if r.Code == code {
			if r.Message == "" {
				return false
			}
			return true
		}
	}
	return false
}

func TestEligibility_EachReasonCode(t *testing.T) {
	t.Parallel()
	cases := []struct {
		code  string
		setup func(f *fx, d *Deps) int64 // returns the proposal id to estimate
	}{
		{ReasonLearningOff, func(f *fx, d *Deps) int64 {
			d.Config = func() config.LearningConfig { c := config.DefaultLearning(); c.Enabled = false; return c }
			return f.prop.ID
		}},
		{ReasonLearningOff, func(f *fx, d *Deps) int64 {
			if err := f.st.SetWorkspaceLearningMode(f.ws.ID, "off"); err != nil {
				t.Fatal(err)
			}
			return f.prop.ID
		}},
		{ReasonWindows, func(f *fx, d *Deps) int64 { d.GOOS = "windows"; return f.prop.ID }},
		{ReasonPlatformUnsupported, func(f *fx, d *Deps) int64 { d.GOOS = "linux"; return f.prop.ID }},
		{ReasonNotPending, func(f *fx, d *Deps) int64 {
			if err := f.st.DecideProposal(f.prop.ID, "rejected", "", "human"); err != nil {
				t.Fatal(err)
			}
			return f.prop.ID
		}},
		{ReasonProviderNotClaude, func(f *fx, d *Deps) int64 {
			r := f.addRun("acp", func(r *store.Run) { r.Provider = "acp-codex" })
			f.setCost("acp", 0.1)
			return f.addProposal("create", r.ID, gate.EvFailureRecovery, createItems("b")).ID
		}},
		{ReasonNoPrompt, func(f *fx, d *Deps) int64 {
			r := f.addRun("np", func(r *store.Run) { r.Prompt = "" })
			f.setCost("np", 0.1)
			return f.addProposal("create", r.ID, gate.EvFailureRecovery, createItems("b")).ID
		}},
		{ReasonNoBaseSHA, func(f *fx, d *Deps) int64 {
			// A run whose provenance was never captured has no base sha.
			r := f.addRunNoProvenance("nb", func(r *store.Run) {})
			f.setCost("nb", 0.1)
			return f.addProposal("create", r.ID, gate.EvFailureRecovery, createItems("b")).ID
		}},
		{ReasonDirtyCheckout, func(f *fx, d *Deps) int64 {
			r := f.addRun("dirty", func(r *store.Run) {})
			dirty := true
			if err := f.st.UpdateRunProvenance("dirty", store.RunProvenance{Dirty: &dirty}); err != nil {
				t.Fatal(err)
			}
			f.setCost("dirty", 0.1)
			return f.addProposal("create", r.ID, gate.EvFailureRecovery, createItems("b")).ID
		}},
		{ReasonContinuationRun, func(f *fx, d *Deps) int64 {
			// "src" started earlier in the same chat.
			r := f.addRun("later", func(r *store.Run) { r.StartedAt = time.Now().UTC().Add(-time.Minute) })
			f.setCost("later", 0.1)
			return f.addProposal("create", r.ID, gate.EvFailureRecovery, createItems("b")).ID
		}},
		{ReasonContinuationRun, func(f *fx, d *Deps) int64 {
			r := f.addRun("intr", func(r *store.Run) { r.ChatID = 0; r.Interrupt = true })
			f.setCost("intr", 0.1)
			return f.addProposal("create", r.ID, gate.EvFailureRecovery, createItems("b")).ID
		}},
		{ReasonSourceCostUnknown, func(f *fx, d *Deps) int64 {
			f.addRun("free", func(r *store.Run) { r.ChatID = 0 })
			return f.addProposal("create", "free", gate.EvFailureRecovery, createItems("b")).ID
		}},
		{ReasonSandboxUnavailable, func(f *fx, d *Deps) int64 {
			d.RealCLI = func() (string, error) { return "", context.DeadlineExceeded }
			return f.prop.ID
		}},
		{ReasonBudget, func(f *fx, d *Deps) int64 { f.setCost("src", 5); return f.prop.ID }},
		{ReasonQuota, func(f *fx, d *Deps) int64 {
			d.QuotaPercent = func() (float64, bool) { return 95, true }
			return f.prop.ID
		}},
		{ReasonBusy, func(f *fx, d *Deps) int64 {
			if _, err := f.st.CreateProbe(store.LearningProbe{WorkspaceID: f.ws.ID, ProposalID: f.prop.ID, Status: "running"}); err != nil {
				t.Fatal(err)
			}
			return f.prop.ID
		}},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			t.Parallel()
			f := newFx(t)
			var d Deps
			d.Store, d.Home, d.GOOS = f.st, f.home, "darwin"
			d.RealCLI = func() (string, error) { return "/usr/bin/true", nil }
			id := tc.setup(f, &d)
			s := New(d)
			s.lookPath = func(string) (string, error) { return "/usr/bin/perl", nil }
			t.Cleanup(s.Close)
			e, err := s.Estimate(context.Background(), id, false)
			if err != nil {
				t.Fatal(err)
			}
			if e.Eligible || !hasCode(e, tc.code) {
				t.Fatalf("eligible=%v codes=%v, want %s", e.Eligible, codes(e), tc.code)
			}
		})
	}

	t.Run("eligible", func(t *testing.T) {
		t.Parallel()
		f := newFx(t)
		e, err := f.svc(nil).Estimate(context.Background(), f.prop.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		if !e.Eligible || len(e.Reasons) != 0 {
			t.Fatalf("estimate = %+v", e)
		}
	})
	t.Run("collects every reason", func(t *testing.T) {
		t.Parallel()
		f := newFx(t)
		e, err := f.svc(func(d *Deps) { d.GOOS = "windows" }).Estimate(context.Background(), f.prop.ID, false)
		if err != nil || e.Eligible {
			t.Fatal(e, err)
		}
		if err := f.st.DecideProposal(f.prop.ID, "rejected", "", "human"); err != nil {
			t.Fatal(err)
		}
		e, _ = f.svc(func(d *Deps) { d.GOOS = "windows" }).Estimate(context.Background(), f.prop.ID, false)
		if !hasCode(e, ReasonWindows) || !hasCode(e, ReasonNotPending) {
			t.Fatalf("codes = %v, want windows + not_pending", codes(e))
		}
	})
}

func TestEstimate_SourceCostTimesHeadroom(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	s := f.svc(nil)
	e, err := s.Estimate(context.Background(), f.prop.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	near := func(a, b float64) bool { d := a - b; return d < 1e-9 && d > -1e-9 }
	if !near(e.EstArmUSD, 0.12) || !near(e.EstUSD, 0.36) || e.Arms != 3 {
		t.Fatalf("full: %+v", e)
	}
	q, _ := s.Estimate(context.Background(), f.prop.ID, true)
	if !near(q.EstUSD, 0.24) || q.Arms != 2 || !q.Quick {
		t.Fatalf("quick: %+v", q)
	}
	// source ran 5 minutes -> 1.2 x 5 = 6 min per arm, 3 arms = 18 min,
	// under the 15 min/arm cap.
	if e.ExpectedMinutes != 18 {
		t.Fatalf("expectedMinutes = %d, want 18", e.ExpectedMinutes)
	}
	if e.PerProbeCapUSD != 1.5 || e.DailyCapUSD != 3 || e.SpentTodayUSD != 0 || e.QuotaPercent != nil {
		t.Fatalf("caps: %+v", e)
	}
}

func TestEstimate_DryRunWritesNothing(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	count := func() [3]int {
		p, a, r := f.countRows()
		return [3]int{p, a, r}
	}
	before := count()
	s := f.svc(func(d *Deps) { d.QuotaPercent = func() (float64, bool) { return 10, true } })
	for _, quick := range []bool{false, true} {
		if _, err := s.Estimate(context.Background(), f.prop.ID, quick); err != nil {
			t.Fatal(err)
		}
	}
	if after := count(); after != before {
		t.Fatalf("rows %v -> %v", before, after)
	}
}

func TestEstimate_DisabledOverPerProbeCap(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	f.setCost("src", 0.40) // 3 x 0.48 = 1.44 fits; quick fits
	s := f.svc(nil)
	if e, _ := s.Estimate(context.Background(), f.prop.ID, false); !e.Eligible {
		t.Fatalf("1.44 under the 1.50 cap: %v", codes(e))
	}
	f.setCost("src", 0.45) // 3 x 0.54 = 1.62 > 1.50
	e, _ := s.Estimate(context.Background(), f.prop.ID, false)
	if e.Eligible || !hasCode(e, ReasonBudget) {
		t.Fatalf("over per-probe cap: %v", codes(e))
	}
	if q, _ := s.Estimate(context.Background(), f.prop.ID, true); !q.Eligible {
		t.Fatalf("quick (1.08) should fit: %v", codes(q))
	}
}

func TestEstimate_DisabledOverDailyCap(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	f.seedProbeSpend(2.80) // a finished probe earlier today
	s := f.svc(nil)
	e, err := s.Estimate(context.Background(), f.prop.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if e.Eligible || !hasCode(e, ReasonBudget) || e.SpentTodayUSD < 2.79 {
		t.Fatalf("estimate = %+v codes %v", e, codes(e))
	}
	// Yesterday's spend does not count.
	g := newFx(t)
	g.seedProbeSpend(2.80)
	e, _ = g.svc(func(d *Deps) { d.Now = func() time.Time { return time.Now().Add(25 * time.Hour) } }).Estimate(context.Background(), g.prop.ID, false)
	if hasCode(e, ReasonBudget) {
		t.Fatalf("spend older than 24h counted: %+v", e)
	}
}

func TestBudget_ProbeBucketSeparateFromLearning(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	s := f.svc(nil)
	// Learning (reflect) spend, far above the probe daily cap, must not
	// reduce probe headroom.
	f.addPurposeRun("refl", "reflect", 50)
	e, _ := s.Estimate(context.Background(), f.prop.ID, false)
	if !e.Eligible || e.SpentTodayUSD != 0 {
		t.Fatalf("learning spend leaked into the probe bucket: %+v", e)
	}
	// And probe spend must not show up in the learning bucket.
	f.addPurposeRun("prb", "probe", 2)
	if c, _, _, err := f.st.SumLearningUsageForWorkspace(f.ws.ID, time.Now().Add(-24*time.Hour)); err != nil || c < 49.99 || c > 50.01 {
		t.Fatalf("learning spend = %v, %v; want 50 (probe run excluded)", c, err)
	}
	e, _ = s.Estimate(context.Background(), f.prop.ID, false)
	if e.SpentTodayUSD < 1.99 || e.SpentTodayUSD > 2.01 {
		t.Fatalf("probe spend = %v, want 2", e.SpentTodayUSD)
	}
}

func TestQuota_ReadsCacheOnlyZeroEndpointCalls(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	calls := 0
	s := f.svc(func(d *Deps) {
		d.QuotaPercent = func() (float64, bool) { calls++; return 85, true }
	})
	e, _ := s.Estimate(context.Background(), f.prop.ID, false)
	if calls < 1 || !hasCode(e, ReasonQuota) || e.QuotaPercent == nil || *e.QuotaPercent != 85 {
		t.Fatalf("calls=%d estimate=%+v", calls, e)
	}
	// Unknown quota is not a reason.
	s2 := f.svc(func(d *Deps) { d.QuotaPercent = func() (float64, bool) { return 0, false } })
	if e, _ := s2.Estimate(context.Background(), f.prop.ID, false); hasCode(e, ReasonQuota) || e.QuotaPercent != nil {
		t.Fatalf("unknown quota skipped the probe: %+v", e)
	}
	// Deps has exactly one quota accessor and it is the cached snapshot:
	// no provider usage function exists to call.
	if _, ok := any(Deps{}).(interface{ FetchUsage() }); ok {
		t.Fatal("Deps exposes a usage fetcher")
	}
}
