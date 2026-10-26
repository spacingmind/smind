package validation

import (
	"context"
	"errors"
	"time"
)

// ProbeState is what the watcher knows about a validator task's runs.
type ProbeState struct {
	// Running: a run of the validator task is executing now.
	Running bool
	// Started: the validator task has at least one run (it may already be
	// over by the first look).
	Started bool
	// Queued: a prompt of the validator's chat is held or queued (a rate-limit
	// wait re-queues the same prompt), so the validator is not finished.
	Queued bool
	// CostUSD is the reported cost of the validator task's runs so far.
	CostUSD float64
}

// Watch supervises one validation until it ends (ADR-0045 §4): it enforces the
// wall clock and the budget, writes the daemon-owned blocked verdicts when the
// validator ends without one, and runs the post-check on the lead's worktree.
// All effects are injected so Tick is tested with a fake clock.
type Watch struct {
	Svc             *Service
	ValidationID    int64
	ValidatorTaskID int64
	MaxMinutes      int
	BudgetUSD       float64

	Probe func(validatorTaskID int64) ProbeState
	// Stop stops the validator's running runs.
	Stop func()
	// LeadDigest returns the lead worktree's current Digest; Before is the
	// value recorded before the validator started ("" skips the post-check).
	LeadDigest func() (string, error)
	Before     string

	elapsed time.Duration
	sawRun  bool
}

// Tick advances the watch by dt of wall time and reports whether it is done.
// Only time during which the validator is running counts toward MaxMinutes:
// waiting out a rate limit does not.
func (w *Watch) Tick(dt time.Duration) bool {
	val, err := w.Svc.DB.GetValidation(w.ValidationID)
	if err != nil {
		return true
	}
	ps := w.Probe(w.ValidatorTaskID)
	if ps.Running || ps.Started {
		w.sawRun = true
	}
	if ps.Running {
		w.elapsed += dt
	}
	if val.VerdictJSON != "" {
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
		reason, summary = ReasonTimeout, "the validator ran out of its wall-clock limit"
	case w.BudgetUSD > 0 && ps.CostUSD >= w.BudgetUSD:
		w.stop()
		reason, summary = ReasonBudget, "the validator reached its budget"
	case w.sawRun && !ps.Running && !ps.Queued:
		reason, summary = ReasonCrashed, "the validator's run ended without submitting a verdict"
	default:
		return false
	}
	// A changed lead worktree outranks the other reasons: the verdict must
	// never read as merely "crashed" when the validator touched it.
	if w.leadChanged() {
		reason, summary = ReasonScopeViolation, "the lead's worktree changed while the validator ran"
	}
	if err := w.Svc.WriteBlocked(w.ValidationID, reason, summary); err != nil && !errors.Is(err, ErrVerdictExists) {
		return false
	}
	return true
}

func (w *Watch) stop() {
	if w.Stop != nil {
		w.Stop()
	}
}

// leadChanged compares the lead worktree's digest with the one recorded
// before the run; "" or an unreadable digest skips the check.
func (w *Watch) leadChanged() bool {
	if w.Before == "" || w.LeadDigest == nil {
		return false
	}
	after, err := w.LeadDigest()
	return err == nil && after != w.Before
}

// postCheck flags a validation whose verdict is already in when the lead
// worktree changed.
func (w *Watch) postCheck(bool) {
	if w.leadChanged() {
		_ = w.Svc.FlagScopeViolation(w.ValidationID)
	}
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
