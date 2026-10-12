package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spacingmind/smind/internal/quota"
)

// osStderr is os.Stdout's stderr sibling (see osStdout in version.go):
// the command wrappers print usage errors to it, tests swap it.
var osStderr io.Writer = os.Stderr

// accountUsageEntry and accountUsageResponse mirror the `account.usage`
// wire result (PR-C wire contract): only the keys the daemon sends, no
// credential material exists on this wire at all. The nested report
// reuses quota.Report, whose JSON shape is the stored/transported one.
type accountUsageEntry struct {
	AccountID      int64         `json:"accountId"`
	Key            string        `json:"key"`
	Provider       string        `json:"provider"`
	DisplayName    string        `json:"displayName"`
	CredentialType string        `json:"credentialType"`
	Stale          bool          `json:"stale"`
	NextRefreshAt  *string       `json:"nextRefreshAt"`
	RefreshBlocked *string       `json:"refreshBlocked"`
	InFlight       bool          `json:"inFlight"`
	LastError      *string       `json:"lastError"`
	Report         *quota.Report `json:"report"`
}

type accountUsageResponse struct {
	Enabled    bool                `json:"enabled"`
	TTLSeconds int                 `json:"ttlSeconds"`
	Reports    []accountUsageEntry `json:"reports"`
}

// wsCaller is the slice of *wsclient.Client the command needs: one
// request/response round trip.
type wsCaller func(ctx context.Context, method string, params any, result any) error

// accountUsageDial is a variable so tests can stub the daemon connection
// without ever starting a real daemon or touching ~/.smind.
var accountUsageDial = func(ctx context.Context) (wsCaller, error) {
	client, err := dialDaemon(ctx)
	if err != nil {
		return nil, err
	}
	return client.Call, nil
}

func cmdAccountUsage(args []string) int {
	refresh, jsonOut := false, false
	for _, arg := range args {
		switch arg {
		case "--refresh":
			refresh = true
		case "--json":
			jsonOut = true
		default:
			fmt.Fprintln(osStderr, "usage: smind account usage [--refresh] [--json]")
			return 2
		}
	}
	return runAccountUsage(context.Background(), accountUsageDial, refresh, jsonOut, time.Now(), osStdout, osStderr)
}

// runAccountUsage is the testable core of `smind account usage`: dial is
// injected so unit tests never need a daemon, and now is injected so
// countdowns are deterministic.
func runAccountUsage(ctx context.Context, dial func(context.Context) (wsCaller, error), refresh, jsonOut bool, now time.Time, stdout, stderr io.Writer) int {
	call, err := dial(ctx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	mode := "touch"
	if refresh {
		mode = "refresh"
	}
	var result accountUsageResponse
	if err := call(ctx, "account.usage", map[string]any{"mode": mode}, &result); err != nil {
		fmt.Fprintf(stderr, "account usage: %v\n", err)
		return 1
	}

	if jsonOut {
		b, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "account usage: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(b))
		return 0
	}
	renderAccountUsage(stdout, &result, refresh, now)
	return 0
}

// renderAccountUsage prints the text view of an account.usage result.
// now is injected for deterministic countdowns. An unavailable account is
// data, not an error: exit 0 is the caller's business.
func renderAccountUsage(w io.Writer, result *accountUsageResponse, refresh bool, now time.Time) {
	if !result.Enabled {
		fmt.Fprintln(w, "account usage is disabled (accountUsage.enabled in config.yaml)")
		return
	}
	if len(result.Reports) == 0 {
		fmt.Fprintln(w, "no accounts")
		return
	}
	for i := range result.Reports {
		if i > 0 {
			fmt.Fprintln(w)
		}
		renderAccountUsageOne(w, &result.Reports[i], refresh, now)
	}
}

func renderAccountUsageOne(w io.Writer, e *accountUsageEntry, refresh bool, now time.Time) {
	header := e.Provider
	if e.DisplayName != "" {
		header = fmt.Sprintf("%s (%s", e.DisplayName, e.Provider)
		if e.Report != nil && e.Report.PlanLabel != "" {
			header += " · " + e.Report.PlanLabel
		}
		header += ")"
	}
	fmt.Fprintf(w, "%s\n", header)

	switch {
	case e.Report == nil:
		if e.LastError != nil {
			fmt.Fprintf(w, "  couldn't fetch usage (%s)\n", *e.LastError)
		}
	case e.Report.Status == quota.StatusUnavailable:
		fmt.Fprintf(w, "  %s\n", unavailableWording(e.Report))
	case e.Report.Status == quota.StatusError:
		errMsg := ""
		if e.LastError != nil {
			errMsg = *e.LastError
		} else if e.Report.Error != "" {
			errMsg = e.Report.Error
		}
		fmt.Fprintf(w, "  couldn't fetch usage (%s)\n", errMsg)
	default:
		tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
		for _, win := range e.Report.Windows {
			pct := "—"
			if win.UsedPct != nil {
				pct = fmt.Sprintf("%d%% used", int(math.Round(*win.UsedPct)))
			}
			line := fmt.Sprintf("  %s\t%s", windowLabel(win), pct)
			if win.ResetsAt != nil && win.ResetsAt.After(now) {
				line += " · resets in " + formatCountdown(win.ResetsAt.Sub(now))
			} else if win.ResetsAt != nil {
				line += " · reset"
			}
			fmt.Fprintln(tw, line)
		}
		tw.Flush()
		for _, b := range e.Report.Balances {
			val := "—"
			switch {
			case b.Remaining != nil:
				val = trimFloat(*b.Remaining)
			case b.Used != nil:
				val = trimFloat(*b.Used)
			}
			fmt.Fprintf(w, "  %s: %s %s\n", b.Label, val, b.Unit)
		}
		if !e.Report.FetchedAt.IsZero() {
			fmt.Fprintf(w, "  updated %s ago\n", formatCountdown(now.Sub(e.Report.FetchedAt)))
		}
	}

	if e.Stale {
		fmt.Fprintln(w, "  (stale: showing the last good report)")
	}
	if refresh && e.RefreshBlocked != nil {
		fmt.Fprintf(w, "  refresh not available (%s)%s\n", *e.RefreshBlocked, blockedSuffix(*e.RefreshBlocked, e.NextRefreshAt, now))
	}
	if e.InFlight {
		fmt.Fprintln(w, "  still refreshing…")
	}
}

// windowLabel prefers the human Label, falling back to ID; shortLabel is
// redundant with Label in the fixtures we render.
func windowLabel(win quota.Window) string {
	if win.Label != "" {
		return win.Label
	}
	return win.ID
}

// unavailableWording renders a StatusUnavailable report's Problem as a
// sentence; the detail strings are the poller's fixed vocabulary.
func unavailableWording(r *quota.Report) string {
	if r.Problem == nil {
		return "no plan windows (API-key account)"
	}
	switch r.Problem.Kind {
	case quota.ProblemExpired:
		return "login expired — sign in again"
	case quota.ProblemRejected:
		return "login rejected by the provider"
	default: // no_quota and anything else
		if r.Problem.Detail != "" {
			return r.Problem.Detail
		}
		return "no plan windows (API-key account)"
	}
}

// blockedSuffix: refreshBlocked reasons have distinct "until" wording:
// rejected means a new login is required (no clock), the others resume on
// a schedule, rendered as a local clock time plus a countdown.
func blockedSuffix(reason string, nextRefreshAt *string, now time.Time) string {
	if reason == "rejected" {
		return " — until the login is replaced"
	}
	if nextRefreshAt == nil {
		return ""
	}
	at, err := time.Parse(time.RFC3339, *nextRefreshAt)
	if err != nil {
		return ""
	}
	if !at.After(now) {
		return ""
	}
	return fmt.Sprintf(" — next at %s / in %s", at.Local().Format("15:04"), formatCountdown(at.Sub(now)))
}

// formatCountdown renders a duration the way people read reset windows:
// biggest one or two units, truncating (not rounding) the remainder.
func formatCountdown(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	sec := int64(d.Seconds())
	switch {
	case sec < 1:
		return "<1m"
	case sec < 60:
		return fmt.Sprintf("%ds", sec)
	case sec < 60*90:
		return fmt.Sprintf("%dm", sec/60)
	}
	days := sec / 86400
	if days >= 1 {
		return fmt.Sprintf("%dd %dh", days, (sec%86400)/3600)
	}
	hours := sec / 3600
	mins := (sec % 3600) / 60
	if mins == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh %dm", hours, mins)
}

// trimFloat renders balance values without trailing-zero noise.
func trimFloat(f float64) string {
	s := fmt.Sprintf("%.2f", f)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" || s == "-" {
		return "0"
	}
	return s
}
