package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// runStartedCell is the STARTED column: a clock time for today, a short date
// otherwise (local time), "-" when unknown.
func runStartedCell(t, now time.Time) string {
	if t.IsZero() {
		return "-"
	}
	t, now = t.Local(), now.Local()
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Format("15:04:05")
	}
	return t.Format("Jan 02 15:04")
}

// compactDuration formats d as 1h02m, 3m12s, 12s or <1s.
func compactDuration(d time.Duration) string {
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d/time.Hour), int(d%time.Hour/time.Minute))
	case d >= time.Minute:
		return fmt.Sprintf("%dm%02ds", int(d/time.Minute), int(d%time.Minute/time.Second))
	case d >= time.Second:
		return fmt.Sprintf("%ds", int(d/time.Second))
	}
	return "<1s"
}

// runDurationCell is the DURATION column: finish-start, or now-start while
// the run is still running.
func runDurationCell(r runSummaryRow, now time.Time) string {
	if r.StartedAt.IsZero() {
		return "-"
	}
	end := now
	if r.Status != "running" {
		if r.FinishedAt == nil {
			return "-"
		}
		end = *r.FinishedAt
	}
	d := end.Sub(r.StartedAt)
	if d < 0 {
		d = 0
	}
	return compactDuration(d)
}

// compactCount abbreviates a token count: 123, 1.2k, 12k, 1.2M.
func compactCount(n int64) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 10000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	case n < 1_000_000:
		return fmt.Sprintf("%dk", n/1000)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
}

// runUsageCell is the USAGE column: "<in>/<out> tok" plus " $cost".
func runUsageCell(r runSummaryRow) string {
	u := r.Usage
	if u == nil || (u.InputTokens == nil && u.OutputTokens == nil && u.CostUSD == nil) {
		return "-"
	}
	cost := ""
	if u.CostUSD != nil {
		cost = fmt.Sprintf("$%.2f", *u.CostUSD)
	}
	if u.InputTokens == nil && u.OutputTokens == nil {
		return cost
	}
	cell := func(p *int64) string {
		if p == nil {
			return "?"
		}
		return compactCount(*p)
	}
	s := cell(u.InputTokens) + "/" + cell(u.OutputTokens) + " tok"
	if cost != "" {
		s += " " + cost
	}
	return s
}

// runCommitCount counts the commits in base..head of the repo at worktree.
func runCommitCount(worktree, base, head string) (int, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", worktree, "rev-list", "--count", base+".."+head)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.Output()
	if err != nil {
		return 0, false
	}
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &n); err != nil {
		return 0, false
	}
	return n, true
}

// runCommitsCell is the COMMITS column: commits the run added, "-" unknown.
func runCommitsCell(r runSummaryRow, worktree string) string {
	if r.BaseSHA == nil || r.HeadSHA == nil || *r.BaseSHA == "" || *r.HeadSHA == "" || worktree == "" {
		return "-"
	}
	if *r.BaseSHA == *r.HeadSHA {
		return "0"
	}
	n, ok := runCommitCount(worktree, *r.BaseSHA, *r.HeadSHA)
	if !ok {
		return "-"
	}
	return fmt.Sprintf("%d", n)
}

// runErrCause is the first line of an error text, whitespace-collapsed and cut
// to 80 runes.
func runErrCause(errText string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(errText), "\n")
	s := strings.Join(strings.Fields(first), " ")
	if r := []rune(s); len(r) > 80 {
		s = string(r[:80]) + "…"
	}
	return s
}
