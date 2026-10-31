package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"
)

// hostAuditRow is one row of the fleet.host.audit answer.
type hostAuditRow struct {
	ID        int64     `json:"id"`
	Time      time.Time `json:"time"`
	Direction string    `json:"direction"`
	HostID    string    `json:"hostId"`
	Peer      string    `json:"peer"`
	Method    string    `json:"method"`
	Decision  string    `json:"decision"`
	Code      string    `json:"code"`
	Summary   string    `json:"summary"`
	LatencyMs int64     `json:"latencyMs"`
}

// runHostAudit is `smind host audit [--host alias] [--limit N] [--json]`: the
// human's read-only view of the fleet audit log, newest first. The daemon refuses
// the method to anything but a local human connection, and the rows hold a hash and
// a masked, redacted prefix of each request, never a prompt.
func runHostAudit(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("host audit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	host := fs.String("host", "", "only exchanges with this host (alias or host id)")
	limit := fs.Int("limit", 50, "how many rows to show (1-200)")
	jsonOut := fs.Bool("json", false, "print the daemon answer as JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 || *limit < 1 || *limit > 200 {
		fmt.Fprint(stderr, hostUsage)
		return 2
	}
	call, closeFn, ok := openHost(ctx, stderr)
	if !ok {
		return 1
	}
	defer closeFn()

	params := map[string]any{"limit": *limit}
	if *host != "" {
		params["host"] = *host
	}
	var raw json.RawMessage
	if err := call(ctx, "fleet.host.audit", params, &raw); err != nil {
		fmt.Fprintf(stderr, "smind host audit: %s\n", hostErrMessage(err))
		return 1
	}
	if *jsonOut {
		fmt.Fprintln(stdout, string(raw))
		return 0
	}
	var res struct {
		Rows []hostAuditRow `json:"rows"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		fmt.Fprintf(stderr, "smind host audit: bad answer: %v\n", err)
		return 1
	}
	if len(res.Rows) == 0 {
		fmt.Fprintln(stdout, "(no fleet activity)")
		return 0
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TIME\tDIR\tPEER\tMETHOD\tDECISION\tSUMMARY")
	for _, r := range res.Rows {
		decision := r.Decision
		if r.Code != "" {
			decision += " (" + r.Code + ")"
		}
		peer := r.Peer
		if peer == "" {
			peer = shortHostID(r.HostID)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			r.Time.Local().Format("2006-01-02 15:04:05"), hostSafeText(r.Direction), hostSafeText(peer),
			hostSafeText(r.Method), hostSafeText(decision), auditSummaryOneLine(r.Summary))
	}
	_ = tw.Flush()
	return 0
}

// auditSummaryOneLine shortens the hash to 12 hex digits and keeps the rest on one
// line; the full text is in --json.
func auditSummaryOneLine(s string) string {
	s = hostSafeText(s)
	if rest, ok := strings.CutPrefix(s, "sha256="); ok {
		hash, tail, _ := strings.Cut(rest, " ")
		if len(hash) > 12 {
			hash = hash[:12]
		}
		return "sha256=" + hash + " " + strings.TrimSpace(tail)
	}
	return s
}
