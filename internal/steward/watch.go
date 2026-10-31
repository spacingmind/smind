package steward

import (
	"context"
	"errors"
	"fmt"
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
	MaxMinutes    int
	BudgetUSD     float64

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
	var reason, summary string
	switch {
	case w.MaxMinutes > 0 && w.elapsed >= time.Duration(w.MaxMinutes)*time.Minute:
		w.stop()
		reason, summary = "timeout", "the steward seat ran out of its wall-clock limit"
	case w.BudgetUSD > 0 && ps.CostUSD >= w.BudgetUSD:
		w.stop()
		reason, summary = "budget", "the steward seat reached its budget"
	case w.sawRun && !ps.Running && !ps.Queued:
		reason, summary = "crashed", "the steward seat's run ended without submitting a verdict"
	default:
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
		_ = w.Svc.FlagScopeViolation(w.ReviewID)
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
