package steward

import (
	"errors"
	"fmt"
)

// Pass statuses (ADR-0054 §2), the same strings steward_review_passes holds.
const (
	PassPending = "pending"
	PassRunning = "running"
	PassDone    = "done"
	PassBlocked = "blocked"
	PassSkipped = "skipped"
)

// PassResult is one pass of a two-axis review as Combine reads it.
type PassResult struct {
	Axis          string
	Status        string
	SeatVerdict   string // the pass's own verdict, when Status is PassDone
	BlockedReason string // a DaemonBlockedReasons value, when Status is PassBlocked
}

// Combined is what the passes decide before the gates are applied.
type Combined struct {
	// SeatVerdict is the combined seat verdict to hand to Clamp; empty when
	// Blocked.
	SeatVerdict string
	// Blocked: no pass produced a verdict, so the review ends BLOCKED with
	// BlockedReason (the first failed pass's, standards first).
	Blocked       bool
	BlockedReason string
	// Notes say which axis lowered or failed the review, in pass order.
	Notes []string
}

// ErrPassesOpen is returned by Combine while a pass has not finished.
var ErrPassesOpen = errors.New("a steward review pass is still open")

// Combine merges the passes of one review (ADR-0054 §6): the combined seat
// verdict is the lowest-ranked verdict of the passes that submitted one, so a
// pass can only lower it and never raise it. A pass that ended blocked never
// lets the review be ACCEPTED: with another pass's verdict the result is at
// most CHANGES; with none the review is BLOCKED. A skipped pass (no spec) is
// neither a verdict nor a failure. An unrecognised verdict ranks below
// BLOCKED, as in Clamp (fail closed). Combine does not apply the gates: the
// caller passes its result to Clamp, which is unchanged.
func Combine(passes []PassResult) (Combined, error) {
	var res Combined
	best, have := VerdictAccepted, false
	failed := false
	for _, p := range passes {
		switch p.Status {
		case PassDone:
			have = true
			v := p.SeatVerdict
			if Rank(v) < 0 {
				v = VerdictBlocked
			}
			if Rank(v) < Rank(best) {
				best = v
			}
			if v != VerdictAccepted {
				res.Notes = append(res.Notes, fmt.Sprintf("axis %s: %s", p.Axis, v))
			}
		case PassBlocked:
			if !failed {
				res.BlockedReason = p.BlockedReason
			}
			failed = true
			res.Notes = append(res.Notes, fmt.Sprintf("axis %s did not complete (%s)", p.Axis, p.BlockedReason))
		case PassSkipped:
			res.Notes = append(res.Notes, fmt.Sprintf("axis %s skipped", p.Axis))
		default:
			return Combined{}, ErrPassesOpen
		}
	}
	switch {
	case !have:
		res.Blocked = true
		if res.BlockedReason == "" {
			res.BlockedReason = "crashed"
		}
	case failed && Rank(best) > Rank(VerdictChanges):
		res.SeatVerdict = VerdictChanges
	default:
		res.SeatVerdict = best
	}
	return res, nil
}
