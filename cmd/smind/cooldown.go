package main

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"
	"time"
)

const cooldownUsage = `Usage:
  smind cooldown ls                    list providers that are cooling down after a rate limit
  smind cooldown clear <provider>      I reset my quota: clear the cooldown and retry held runs now (humans only)
`

// cooldownDial is a variable so tests can stub the daemon connection.
var cooldownDial = func(ctx context.Context) (wsCaller, func(), error) {
	client, err := dialDaemon(ctx)
	if err != nil {
		return nil, nil, err
	}
	return client.Call, func() { _ = client.Close() }, nil
}

// cooldownProvider carries just the two fields `cooldown ls` shows.
type cooldownProvider struct {
	ID            string `json:"id"`
	CooldownUntil string `json:"cooldownUntil"`
}

func cmdCooldown(args []string) int {
	return runCooldown(context.Background(), args, osStdout, osStderr)
}

// runCooldown is cmdCooldown with its environment injected. Exit codes:
// 0 the listing or clear succeeded, 1 a daemon or call error, 2 usage.
func runCooldown(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	switch {
	case len(args) == 1 && args[0] == "ls":
		return runCooldownLs(ctx, stdout, stderr)
	case len(args) == 2 && args[0] == "clear" && args[1] != "":
		return runCooldownClear(ctx, args[1], stdout, stderr)
	}
	fmt.Fprint(stderr, cooldownUsage)
	return 2
}

func runCooldownLs(ctx context.Context, stdout, stderr io.Writer) int {
	call, closeFn, err := cooldownDial(ctx)
	if err != nil {
		return cooldownDialError(err, stderr)
	}
	defer closeFn()

	var res struct {
		Providers []cooldownProvider `json:"providers"`
	}
	if err := call(ctx, "provider.list", map[string]any{}, &res); err != nil {
		fmt.Fprintf(stderr, "smind: %v\n", err)
		return 1
	}

	now := time.Now()
	type cooldownRow struct{ id, at, in string }
	var rows []cooldownRow
	for _, p := range res.Providers {
		if p.CooldownUntil == "" {
			continue
		}
		reset, err := time.Parse(time.RFC3339, p.CooldownUntil)
		if err != nil {
			continue
		}
		rows = append(rows, cooldownRow{p.ID, reset.Local().Format("15:04"), cooldownIn(reset.Sub(now))})
	}
	if len(rows) == 0 {
		fmt.Fprintln(stdout, "No provider is cooling down.")
		return 0
	}
	tw := tabwriter.NewWriter(stdout, 0, 8, 2, ' ', 0)
	fmt.Fprintln(tw, "PROVIDER\tRESETS AT\tIN")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", r.id, r.at, r.in)
	}
	if err := tw.Flush(); err != nil {
		fmt.Fprintf(stderr, "smind: %v\n", err)
		return 1
	}
	return 0
}

func runCooldownClear(ctx context.Context, provider string, stdout, stderr io.Writer) int {
	call, closeFn, err := cooldownDial(ctx)
	if err != nil {
		return cooldownDialError(err, stderr)
	}
	defer closeFn()

	var res struct {
		Cleared bool `json:"cleared"`
		Woken   int  `json:"woken"`
	}
	if err := call(ctx, "provider.clearCooldown", map[string]any{"provider": provider}, &res); err != nil {
		fmt.Fprintf(stderr, "smind: %v\n", err)
		return 1
	}
	switch {
	case res.Woken > 0:
		fmt.Fprintf(stdout, "Cleared the cooldown on %s; retrying %d held run(s) now.\n", provider, res.Woken)
	case res.Cleared:
		fmt.Fprintf(stdout, "Cleared the cooldown on %s.\n", provider)
	default:
		fmt.Fprintf(stdout, "%s had no active cooldown.\n", provider)
	}
	return 0
}

// cooldownDialError reports a failed daemon connection the way push does.
func cooldownDialError(err error, stderr io.Writer) int {
	if isNoDaemon(err) {
		fmt.Fprintln(stderr, "smind: no daemon is running (start it with `smind serve`)")
	} else {
		fmt.Fprintf(stderr, "smind: %v\n", err)
	}
	return 1
}

// cooldownIn renders the time left in a cooldown, floored at "1m": a
// provider still listed with a reset time that just passed is due out.
func cooldownIn(d time.Duration) string {
	if d < time.Minute {
		return "1m"
	}
	return rateLimitIn(d)
}
