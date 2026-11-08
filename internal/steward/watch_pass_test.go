package steward

import (
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/validation"
)

// passWatch is a pass watch over the fixture's pass, with a probe that
// returns whatever *ps holds.
func (fx twoAxisFixture) passWatch(axis Axis, ps *validation.ProbeState, stopped *int) *Watch {
	return &Watch{Svc: fx.svc, ReviewID: fx.row.ID, StewardTaskID: 10 + int64(len(axis)), Axis: axis, MaxMinutes: 20, BudgetUSD: 0.4,
		Probe: func(int64) validation.ProbeState { return *ps },
		Stop:  func() { *stopped++ }}
}

func TestWatch_PassBudgetBlocksOnlyThatPass(t *testing.T) {
	t.Parallel()

	fx := openTwoAxis(t, axisDigest("AC1"))
	ps := validation.ProbeState{Running: true, Started: true, CostUSD: 0.5}
	stopped := 0
	w := fx.passWatch(AxisStandards, &ps, &stopped)
	if !w.Tick(time.Minute) {
		t.Fatal("Tick(over the pass budget) = not done")
	}
	std, _ := fx.svc.DB.GetStewardReviewPass(fx.row.ID, string(AxisStandards))
	spec, _ := fx.svc.DB.GetStewardReviewPass(fx.row.ID, string(AxisSpec))
	if std.Status != PassBlocked || std.BlockedReason != "budget" || std.CostUSD != 0.5 || stopped != 1 {
		t.Fatalf("standards pass = %+v stopped %d, want blocked/budget at the real cost with its run stopped", std, stopped)
	}
	if spec.Status != PassRunning {
		t.Fatalf("spec pass = %+v, want it untouched", spec)
	}
	if row, _ := fx.svc.DB.GetStewardReview(fx.row.ID); row.Status != "running" {
		t.Fatalf("review = %+v, want it open while the other pass runs", row)
	}
}

func TestWatch_PassTimeoutAndCrash(t *testing.T) {
	t.Parallel()

	t.Run("timeout", func(t *testing.T) {
		t.Parallel()
		fx := openTwoAxis(t, axisDigest("AC1"))
		ps := validation.ProbeState{Running: true, Started: true}
		stopped := 0
		w := fx.passWatch(AxisSpec, &ps, &stopped)
		if w.Tick(10 * time.Minute) {
			t.Fatal("Tick(10m of 20m) = done")
		}
		if !w.Tick(10 * time.Minute) {
			t.Fatal("Tick(20m of 20m) = not done")
		}
		if p, _ := fx.svc.DB.GetStewardReviewPass(fx.row.ID, string(AxisSpec)); p.BlockedReason != "timeout" {
			t.Fatalf("spec pass = %+v, want timeout", p)
		}
	})
	t.Run("crashed", func(t *testing.T) {
		t.Parallel()
		fx := openTwoAxis(t, axisDigest("AC1"))
		ps := validation.ProbeState{Running: true, Started: true}
		stopped := 0
		w := fx.passWatch(AxisStandards, &ps, &stopped)
		if w.Tick(time.Minute) {
			t.Fatal("Tick(running) = done")
		}
		ps = validation.ProbeState{Started: true}
		if !w.Tick(time.Minute) {
			t.Fatal("Tick(run ended without a verdict) = not done")
		}
		if p, _ := fx.svc.DB.GetStewardReviewPass(fx.row.ID, string(AxisStandards)); p.BlockedReason != "crashed" {
			t.Fatalf("standards pass = %+v, want crashed", p)
		}
	})
}

func TestWatch_PassStopsWhenTheReviewWasDecidedWithoutIt(t *testing.T) {
	t.Parallel()

	fx := openTwoAxis(t, axisDigest("AC1"))
	if err := fx.svc.WritePassBlocked(fx.row.ID, AxisSpec, "scope_violation", "the lead's worktree changed", 0); err != nil {
		t.Fatal(err)
	}
	ps := validation.ProbeState{Running: true, Started: true, CostUSD: 0.1}
	stopped := 0
	w := fx.passWatch(AxisStandards, &ps, &stopped)
	if !w.Tick(time.Minute) || stopped != 1 {
		t.Fatalf("watch of a pass whose review is already blocked: stopped %d, want it ended and its run stopped", stopped)
	}
	if p, _ := fx.svc.DB.GetStewardReviewPass(fx.row.ID, string(AxisStandards)); p.Status != PassBlocked || p.BlockedReason != "cancelled" {
		t.Fatalf("standards pass = %+v, want closed as cancelled", p)
	}
}

func TestWatch_PassScopeViolationBlocksTheWholeReview(t *testing.T) {
	t.Parallel()

	fx := openTwoAxis(t, axisDigest("AC1"))
	before := validation.Snapshot{Head: "h1"}
	ps := validation.ProbeState{Running: true, Started: true}
	stopped := 0
	w := fx.passWatch(AxisStandards, &ps, &stopped)
	w.Before = before
	w.LeadSnapshot = func() (validation.Snapshot, error) { return validation.Snapshot{Head: "h2"}, nil }
	if w.Tick(time.Minute) {
		t.Fatal("Tick(running) = done")
	}
	ps = validation.ProbeState{Started: true}
	if !w.Tick(time.Minute) {
		t.Fatal("Tick(run ended, lead changed) = not done")
	}
	v, _ := fx.svc.View(fx.row.ID)
	if v.Verdict != VerdictBlocked || v.BlockedReason != "scope_violation" || !v.ScopeViolation || v.MergeReady {
		t.Fatalf("view = %+v, want the whole review BLOCKED with scope_violation", v)
	}
}

func TestWatch_PassAfterVerdictWaitsForTheRunThenPostChecks(t *testing.T) {
	t.Parallel()

	fx := openTwoAxis(t, axisDigest("AC1"))
	if _, err := fx.submit(t, AxisStandards, standardsVerdict(VerdictAccepted)); err != nil {
		t.Fatal(err)
	}
	ps := validation.ProbeState{Running: true, Started: true, CostUSD: 0.2}
	stopped := 0
	w := fx.passWatch(AxisStandards, &ps, &stopped)
	w.Before = validation.Snapshot{Head: "h1"}
	w.LeadSnapshot = func() (validation.Snapshot, error) { return validation.Snapshot{Head: "h2"}, nil }
	if w.Tick(time.Minute) {
		t.Fatal("Tick(verdict in, run still going) = done, want it to wait")
	}
	ps.Running = false
	if !w.Tick(time.Minute) {
		t.Fatal("Tick(verdict in, run ended) = not done")
	}
	if p, _ := fx.svc.DB.GetStewardReviewPass(fx.row.ID, string(AxisStandards)); p.CostUSD != 0.2 {
		t.Fatalf("pass cost = %v, want the real cost rolled in after the verdict", p.CostUSD)
	}
	if row, _ := fx.svc.DB.GetStewardReview(fx.row.ID); !row.ScopeViolation {
		t.Fatal("a worktree change after the verdict was not flagged")
	}
}

func TestWatch_PassBudgetAlsoBoundsSpendAfterTheVerdict(t *testing.T) {
	t.Parallel()

	fx := openTwoAxis(t, axisDigest("AC1"))
	if _, err := fx.submit(t, AxisStandards, standardsVerdict(VerdictAccepted)); err != nil {
		t.Fatal(err)
	}
	ps := validation.ProbeState{Running: true, Started: true, CostUSD: 0.1}
	stopped := 0
	w := fx.passWatch(AxisStandards, &ps, &stopped)
	if w.Tick(time.Minute) || stopped != 0 {
		t.Fatalf("under budget after the verdict: stopped %d, want it left running", stopped)
	}
	ps.CostUSD = 0.45
	if !w.Tick(time.Minute) || stopped != 1 {
		t.Fatalf("over budget after the verdict: stopped %d, want the run stopped", stopped)
	}
}
