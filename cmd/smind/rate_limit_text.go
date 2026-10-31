package main

import (
	"fmt"
	"time"

	"github.com/spacingmind/smind/internal/runs/failure"
)

// rateLimitedText renders a rate_limited run's failure for humans, e.g.
// "rate limited (5h window), resets 17:35 (in 2h 3m)". The window part is
// omitted when unknown; the reset time is local. An inferred reset (zone
// assumed) reads "resets ≈17:35 (in ≈2h 3m)".
func rateLimitedText(window string, resetAt *time.Time, inferred bool, now time.Time) string {
	head := "rate limited"
	if window != "" {
		head += " (" + window + " window)"
	}
	if resetAt == nil {
		return head + ", reset time unknown"
	}
	if !resetAt.After(now) {
		return head + ", reset time has passed"
	}
	local := resetAt.In(now.Location())
	d := resetAt.Sub(now)
	var at string
	switch {
	case d < 24*time.Hour && sameDay(local, now):
		at = local.Format("15:04")
	case d < 7*24*time.Hour:
		at = local.Format("Mon 15:04")
	default:
		at = local.Format("2006-01-02 15:04")
	}
	if inferred {
		at = "≈" + at
		return fmt.Sprintf("%s, resets %s (in ≈%s)", head, at, rateLimitIn(d))
	}
	return fmt.Sprintf("%s, resets %s (in %s)", head, at, rateLimitIn(d))
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// rateLimitIn renders d as hours and minutes only: "45m", "2h 3m".
func rateLimitIn(d time.Duration) string {
	m := int(d.Minutes())
	if m >= 60 {
		return fmt.Sprintf("%dh %dm", m/60, m%60)
	}
	return fmt.Sprintf("%dm", m)
}

// failureText renders a failed run's typed failure for humans: the class in
// words (a plan limit keeps rateLimitedText's window/reset sentence) and, when
// the daemon sent one, " · next: <action label>".
func failureText(kind, window string, resetAt *time.Time, inferred bool, action *failure.Action, now time.Time) string {
	var head string
	switch kind {
	case failure.KindPlanWindowExhausted, failure.KindRateLimited:
		head = rateLimitedText(window, resetAt, inferred, now)
	case failure.KindTransient429:
		head = "transient 429 (provider throttle)"
	case failure.KindAuthExpired:
		head = "auth expired"
	case failure.KindModelNotInPlan:
		head = "model not in plan"
	default:
		head = kind
	}
	if action != nil && action.Label != "" {
		head += " · next: " + action.Label
	}
	return head
}
