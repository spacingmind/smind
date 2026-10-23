package main

import (
	"fmt"
	"time"
)

// rateLimitedText renders a rate_limited run's failure for humans, e.g.
// "rate limited (5h window), resets 17:35 (in 2h 3m)". The window part is
// omitted when unknown; the reset time is local.
func rateLimitedText(window string, resetAt *time.Time, now time.Time) string {
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
