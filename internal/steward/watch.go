package steward

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/validation"
)

// Watch supervises one steward review until it ends: it enforces the wall
// clock and the budget (counted only while the seat's run is executing), it
// writes the daemon-owned blocked reasons when the seat's run ends without a
// verdict, and it runs the post-check on the lead's worktree. All effects
// are injected so Tick is tested with a fake clock.
type Watch struct {
	Svc           *Service
	ReviewID      int64
	StewardTaskID int64
	// Axis is the pass this watch supervises in a two-axis review (ADR-0054);
	// empty for the single review's one seat.
	Axis       Axis
	MaxMinutes int
	BudgetUSD  float64

	Probe func(taskID int64) validation.ProbeState
	// Stop stops the seat's running runs.
	Stop func()
	// LeadSnapshot reads the lead worktree now; Before is the Snapshot
	// taken before the seat started (the zero value skips the post-check).
	LeadSnapshot func() (validation.Snapshot, error)
	Before       validation.Snapshot

	elapsed time.Duration
	sawRun  bool
}

// Tick advances the watch by dt of wall time and reports whether it is done.
// Only time during which the seat's run is executing counts toward
// MaxMinutes: waiting out a rate limit does not.
func (w *Watch) Tick(dt time.Duration) bool {
	if w.Axis != "" {
		return w.tickPass(dt)
	}
	row, err := w.Svc.DB.GetStewardReview(w.ReviewID)
	if err != nil {
		return true
	}
	ps := w.Probe(w.StewardTaskID)
	if ps.Running || ps.Started {
		w.sawRun = true
	}
	if ps.Running {
		w.elapsed += dt
	}
	if err := w.Svc.DB.SetStewardReviewCost(w.ReviewID, ps.CostUSD); err != nil && !errors.Is(err, store.ErrStewardReviewClosed) {
		return false
	}
	if row.Status == "done" || row.Status == "blocked" {
		// The verdict closed the row mid-run: keep rolling the run's real
		// cost in until the run ends (nil/unknown cost is never estimated).
		if err := w.Svc.DB.RaiseStewardReviewCost(w.ReviewID, ps.CostUSD); err != nil {
			return false
		}
	}
	if row.Status == "done" || row.Status == "blocked" {
		// The verdict is in; the post-check waits for the run to finish (or
		// the wall clock) so changes made after submitting are seen too.
		if ps.Running && (w.MaxMinutes <= 0 || w.elapsed < time.Duration(w.MaxMinutes)*time.Minute) {
			return false
		}
		if ps.Running {
			w.stop()
		}
		w.postCheck(true)
		return true
	}
	reason, summary, ended := w.endReason(ps)
	if !ended {
		return false
	}
	// A changed lead worktree outranks the other reasons: the review must
	// never read as merely "crashed" when the seat touched it.
	if changed := w.leadChanged(); len(changed) > 0 {
		reason, summary = "scope_violation", scopeSummary(changed)
	}
	if err := w.Svc.WriteBlocked(w.ReviewID, reason, summary, ps.CostUSD); err != nil && !errors.Is(err, ErrVerdictExists) {
		return false
	}
	return true
}

// endReason says why the seat's run must be ended without a verdict, if it
// must: its wall clock or budget ran out (the run is stopped), or it ended
// by itself without submitting.
func (w *Watch) endReason(ps validation.ProbeState) (reason, summary string, ended bool) {
	switch {
	case w.MaxMinutes > 0 && w.elapsed >= time.Duration(w.MaxMinutes)*time.Minute:
		w.stop()
		return "timeout", "the steward seat ran out of its wall-clock limit", true
	case w.BudgetUSD > 0 && ps.CostUSD >= w.BudgetUSD:
		w.stop()
		return "budget", "the steward seat reached its budget", true
	case w.sawRun && !ps.Running && !ps.Queued:
		return "crashed", "the steward seat's run ended without submitting a verdict", true
	}
	return "", "", false
}

func (w *Watch) stop() {
	if w.Stop != nil {
		w.Stop()
	}
}

// leadChanged compares the lead worktree with the Snapshot taken before the
// run and returns the changed paths (nil: unchanged, skipped or unreadable).
func (w *Watch) leadChanged() []string {
	if w.Before.IsZero() || w.LeadSnapshot == nil {
		return nil
	}
	after, err := w.LeadSnapshot()
	if err != nil {
		return nil
	}
	return w.Before.Changed(after)
}

// postCheck flags a review whose verdict is already in when the lead
// worktree changed.
func (w *Watch) postCheck(bool) {
	if changed := w.leadChanged(); len(changed) > 0 {
		if err := w.Svc.FlagScopeViolation(w.ReviewID); err != nil {
			slog.Warn("steward watch: scope violation not flagged", "review", w.ReviewID, "err", err)
		}
	}
}

// scopeSummary names what changed (at most five paths), so a person can tell
// a seat that edited a file from a build artifact the lead's own tools wrote.
func scopeSummary(changed []string) string {
	shown := changed
	more := ""
	if len(shown) > 5 {
		more = fmt.Sprintf(" and %d more", len(shown)-5)
		shown = shown[:5]
	}
	return "the lead's worktree changed while the steward seat ran: " + strings.Join(shown, ", ") + more
}

// Run ticks every period until the watch is done or ctx is cancelled.
func (w *Watch) Run(ctx context.Context, period time.Duration) {
	t := time.NewTicker(period)
	defer t.Stop()
	last := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			if w.Tick(now.Sub(last)) {
				return
			}
			last = now
		}
	}
}

// tickPass is Tick for one pass of a two-axis review: its cost, wall clock
// and budget are the pass's own, and ending it writes the pass's blocked row
// (the review is decided when every pass is).
func (w *Watch) tickPass(dt time.Duration) bool {
	review, err := w.Svc.DB.GetStewardReview(w.ReviewID)
	if err != nil {
		return true
	}
	pass, err := w.Svc.DB.GetStewardReviewPass(w.ReviewID, string(w.Axis))
	if err != nil {
		return true
	}
	ps := w.Probe(w.StewardTaskID)
	// Until the run has been seen the pass's clock runs from its creation, so a
	// run that never starts still times out; once it has run, only the time it
	// executes counts and a rate-limit wait does not (ADR-0047 §4).
	if ps.Running || !w.sawRun && !ps.Started {
		w.elapsed += dt
	}
	if ps.Running || ps.Started {
		w.sawRun = true
	}
	// The cost only rises, in any status: the verdict usually lands while the
	// run is still going and its usage row is only complete once it ends.
	if err := w.Svc.DB.RaiseStewardPassCost(w.ReviewID, string(w.Axis), ps.CostUSD); err != nil {
		slog.Warn("steward pass watch: cost not recorded", "review", w.ReviewID, "axis", w.Axis, "err", err)
		return false
	}
	passOpen := pass.Status == PassPending || pass.Status == PassRunning
	reviewOpen := review.Status == "pending" || review.Status == "running"
	switch {
	case !reviewOpen && passOpen:
		// The review was decided without this pass (the other pass broke
		// scope, or the Human cancelled): this run has nothing left to judge.
		w.stop()
		// Closing the pass is what keeps it from reading as running forever with
		// no watcher: retry once, and if it still fails keep watching so the next
		// tick tries again (a restart's reconcile closes it as well).
		err := w.Svc.DB.SetStewardPassBlocked(w.ReviewID, string(w.Axis), "cancelled", "the review ended before this pass did", ps.CostUSD, w.Svc.now())
		if err != nil && !errors.Is(err, store.ErrStewardPassClosed) {
			err = w.Svc.DB.SetStewardPassBlocked(w.ReviewID, string(w.Axis), "cancelled", "the review ended before this pass did", ps.CostUSD, w.Svc.now())
		}
		if err != nil && !errors.Is(err, store.ErrStewardPassClosed) {
			slog.Warn("steward pass watch: the pass could not be closed", "review", w.ReviewID, "axis", w.Axis, "err", err)
			return false
		}
		return true
	case !passOpen:
		// The verdict is in; the post-check waits for the run to finish (or
		// the wall clock) so changes made after submitting are seen too.
		// The per-pass budgets sum to the review's cap only if spend stops at
		// the budget after the verdict too, not just before it.
		overBudget := w.BudgetUSD > 0 && ps.CostUSD >= w.BudgetUSD
		if ps.Running && !overBudget && (w.MaxMinutes <= 0 || w.elapsed < time.Duration(w.MaxMinutes)*time.Minute) {
			return false
		}
		if ps.Running {
			w.stop()
		}
		w.postCheck(true)
		return true
	}
	reason, summary, ended := w.endReason(ps)
	if !ended {
		return false
	}
	if changed := w.leadChanged(); len(changed) > 0 {
		reason, summary = "scope_violation", scopeSummary(changed)
		if err := w.Svc.FlagScopeViolation(w.ReviewID); err != nil {
			slog.Warn("steward pass watch: scope violation not flagged", "review", w.ReviewID, "axis", w.Axis, "err", err)
		}
	}
	if err := w.Svc.WritePassBlocked(w.ReviewID, w.Axis, reason, summary, ps.CostUSD); err != nil && !errors.Is(err, ErrVerdictExists) {
		return false
	}
	return true
}
