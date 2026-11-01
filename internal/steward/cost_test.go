package steward

import (
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/validation"
)

// The pilot found costUsd stuck at 0: the verdict closes the review while
// the seat's run is still going, and the run's usage was never rolled in
// afterwards. The real number must reach the row, and only ever rise.
func TestWatch_CostRollsIntoRowAfterVerdict(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	row, token := openReview(t, svc, digestWithGates(Gate{ID: "ci", State: GatePass}))
	ps := validation.ProbeState{Running: true, Started: true, CostUSD: 0.004}
	w := &Watch{Svc: svc, ReviewID: row.ID, StewardTaskID: row.StewardTaskID, MaxMinutes: 60, BudgetUSD: 1,
		Probe: func(int64) validation.ProbeState { return ps }}
	if w.Tick(time.Second) {
		t.Fatal("Tick before the verdict = done")
	}
	if _, err := svc.SubmitVerdict(row.ID, token, row.RunID, verdictRaw(row.ID, VerdictAccepted, "ci")); err != nil {
		t.Fatal(err)
	}
	ps.CostUSD = 0.0126
	if w.Tick(time.Second) {
		t.Fatal("Tick with the run still going = done")
	}
	ps = validation.ProbeState{Started: true, CostUSD: 0.0126}
	if !w.Tick(time.Second) {
		t.Fatal("Tick after the run ended = not done")
	}
	got, err := svc.DB.GetStewardReview(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CostUSD != 0.0126 || got.Verdict != VerdictAccepted {
		t.Fatalf("row = verdict %s cost %v, want ACCEPTED at the real 0.0126", got.Verdict, got.CostUSD)
	}
	// A lower or unknown (0) probe never lowers a recorded cost.
	ps.CostUSD = 0
	w.Tick(time.Second)
	if got, _ = svc.DB.GetStewardReview(row.ID); got.CostUSD != 0.0126 {
		t.Fatalf("cost = %v after a zero probe, want it kept", got.CostUSD)
	}
}

// AC19/AC21: a seat run whose cost passes budgetUsd is stopped and the review
// is BLOCKED with the daemon-written reason budget, carrying that cost; one
// under budget lets the verdict stand.
func TestWatch_BudgetRailFiresOnRealCost(t *testing.T) {
	t.Parallel()
	t.Run("over budget is blocked", func(t *testing.T) {
		t.Parallel()
		svc := newTestService(t)
		row, _ := openReview(t, svc, digestWithGates(Gate{ID: "ci", State: GatePass}))
		stopped := false
		w := &Watch{Svc: svc, ReviewID: row.ID, StewardTaskID: row.StewardTaskID, MaxMinutes: 60, BudgetUSD: 0.01,
			Stop: func() { stopped = true },
			Probe: func(int64) validation.ProbeState {
				return validation.ProbeState{Running: true, Started: true, CostUSD: 0.0126}
			}}
		if !w.Tick(time.Second) {
			t.Fatal("Tick over budget = not done")
		}
		got, _ := svc.DB.GetStewardReview(row.ID)
		if !stopped || got.Verdict != VerdictBlocked || got.BlockedReason != "budget" || got.CostUSD != 0.0126 {
			t.Fatalf("stopped=%v row=%s/%s cost %v, want BLOCKED/budget at 0.0126", stopped, got.Verdict, got.BlockedReason, got.CostUSD)
		}
	})
	t.Run("under budget the verdict stands", func(t *testing.T) {
		t.Parallel()
		svc := newTestService(t)
		row, token := openReview(t, svc, digestWithGates(Gate{ID: "ci", State: GatePass}))
		ps := validation.ProbeState{Running: true, Started: true, CostUSD: 0.0126}
		w := &Watch{Svc: svc, ReviewID: row.ID, StewardTaskID: row.StewardTaskID, MaxMinutes: 60, BudgetUSD: 0.5,
			Probe: func(int64) validation.ProbeState { return ps }}
		if w.Tick(time.Second) {
			t.Fatal("Tick under budget = done")
		}
		if _, err := svc.SubmitVerdict(row.ID, token, row.RunID, verdictRaw(row.ID, VerdictAccepted, "ci")); err != nil {
			t.Fatal(err)
		}
		ps = validation.ProbeState{Started: true, CostUSD: 0.0126}
		w.Tick(time.Second)
		got, _ := svc.DB.GetStewardReview(row.ID)
		if got.Verdict != VerdictAccepted || got.BlockedReason != "" || got.CostUSD != 0.0126 {
			t.Fatalf("row = %s/%q cost %v, want ACCEPTED with the real cost", got.Verdict, got.BlockedReason, got.CostUSD)
		}
	})
}
