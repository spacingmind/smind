package steward

import "fmt"

// DaemonBlockedReasons are the BLOCKED reasons only the daemon may write
// (AC19): the seat and validators cannot claim them.
var DaemonBlockedReasons = []string{"timeout", "budget", "crashed", "cancelled", "scope_violation"}

// IsDaemonBlockedReason reports whether reason is one a daemon may write.
func IsDaemonBlockedReason(reason string) bool {
	for _, r := range DaemonBlockedReasons {
		if r == reason {
			return true
		}
	}
	return false
}

// Rank orders the verdicts BLOCKED < CHANGES < ACCEPTED. Anything else is
// unrecognised (-1) and Clamp treats it as BLOCKED, fail closed.
func Rank(verdict string) int {
	switch verdict {
	case VerdictBlocked:
		return 0
	case VerdictChanges:
		return 1
	case VerdictAccepted:
		return 2
	default:
		return -1
	}
}

// ClampResult is what Clamp publishes: the stored verdict (empty while
// held), whether the review is held for an undecided gate, whether the
// clamp lowered the seat's value, and the per-gate reasons in gate order.
type ClampResult struct {
	Verdict string
	Hold    bool
	Capped  bool
	Reasons []string
}

// Clamp computes the stored verdict from the seat's verdict and the digest's
// gates (AC18). A gate still unknown holds the review and publishes nothing.
// Otherwise the cap starts at ACCEPTED and each failing gate lowers it:
// not_stacked and no_secrets to BLOCKED, any other to CHANGES; the stored
// verdict is the lower-ranked of the seat's value and the cap, so the seat
// can only lower a verdict and an unrecognised seat value fails closed. The
// result is ACCEPTED only when the seat said ACCEPTED and no gate fails.
func Clamp(seatVerdict string, gates []Gate) ClampResult {
	res := ClampResult{Reasons: []string{}}
	capVerdict := VerdictAccepted
	hold := false
	for _, g := range gates {
		switch g.State {
		case GateUnknown:
			hold = true
			res.Reasons = append(res.Reasons, gateReason(g, "unknown"))
		case GateFail:
			res.Reasons = append(res.Reasons, gateReason(g, "failed"))
			lowers := VerdictChanges
			if g.ID == "not_stacked" || g.ID == "no_secrets" {
				lowers = VerdictBlocked
			}
			if Rank(lowers) < Rank(capVerdict) {
				capVerdict = lowers
			}
		}
	}
	if hold {
		res.Hold = true
		res.Capped = res.Verdict != seatVerdict
		return res
	}
	seat := seatVerdict
	if Rank(seat) < 0 {
		seat = VerdictBlocked
	}
	res.Verdict = seat
	if Rank(capVerdict) < Rank(seat) {
		res.Verdict = capVerdict
	}
	res.Capped = res.Verdict != seatVerdict
	return res
}

// gateReason names one gate and what kept it from passing, with its detail
// when it has one.
func gateReason(g Gate, what string) string {
	if g.Detail == "" {
		return fmt.Sprintf("gate %s %s", g.ID, what)
	}
	return fmt.Sprintf("gate %s %s: %s", g.ID, what, g.Detail)
}
