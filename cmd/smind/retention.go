package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

const runsUsage = `Usage:
  smind runs prune [--dry-run]    delete the detailed history of finished runs older than runs.historyRetentionDays
                                  (archived or system tasks only; the run, its cost and final text stay)
`

const dbUsage = `Usage:
  smind db compact                rewrite smind.db to give deleted space back to the disk (holds the write lock;
                                  needs free disk of about 1.2x the file; refused while runs are running)
`

// retentionDial is a variable so tests can stub the daemon connection.
var retentionDial = func(ctx context.Context) (wsCaller, func(), error) {
	client, err := dialDaemon(ctx)
	if err != nil {
		return nil, nil, err
	}
	return client.Call, func() { _ = client.Close() }, nil
}

func cmdRuns(args []string) int {
	return runRuns(context.Background(), args, osStdout, osStderr)
}

func cmdDB(args []string) int {
	return runDB(context.Background(), args, osStdout, osStderr)
}

type prunePrintReport struct {
	Enabled      bool    `json:"enabled"`
	Days         int     `json:"days"`
	Cutoff       string  `json:"cutoff"`
	DryRun       bool    `json:"dryRun"`
	Runs         int64   `json:"runs"`
	Rows         int64   `json:"rows"`
	Bytes        int64   `json:"bytes"`
	ExcerptRuns  int64   `json:"excerptRuns"`
	ExcerptRows  int64   `json:"excerptRows"`
	ExcerptBytes int64   `json:"excerptBytes"`
	FreedBytes   int64   `json:"freedBytes"`
	ReusableMB   float64 `json:"reusableMB"`
	Duration     string  `json:"duration"`
	Incomplete   bool    `json:"incomplete"`
}

// runRuns is cmdRuns with its environment injected. Exit codes: 0 ok, 1 a
// daemon or call error, 2 usage.
func runRuns(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	dry := false
	switch {
	case len(args) == 1 && args[0] == "prune":
	case len(args) == 2 && args[0] == "prune" && args[1] == "--dry-run":
		dry = true
	default:
		fmt.Fprint(stderr, runsUsage)
		return 2
	}
	call, closeFn, err := retentionDial(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "smind: %v\n", err)
		return 1
	}
	defer closeFn()
	var rep prunePrintReport
	if err := call(ctx, "runs.prune", map[string]any{"dryRun": dry}, &rep); err != nil {
		fmt.Fprintf(stderr, "smind: %v\n", err)
		return 1
	}
	if !rep.Enabled {
		fmt.Fprintln(stdout, "retention is off (runs.historyRetentionDays: 0): nothing is pruned")
		return 0
	}
	if t, err := time.Parse(time.RFC3339, rep.Cutoff); err == nil {
		rep.Cutoff = t.Local().Format("2006-01-02 15:04")
	}
	verb := "pruned"
	if dry {
		verb = "would prune"
	}
	fmt.Fprintf(stdout, "retention %d days (runs finished before %s)\n%s the events of %d runs: %d rows, about %s\n", rep.Days, rep.Cutoff, verb, rep.Runs, rep.Rows, mib(rep.Bytes))
	if rep.ExcerptRuns > 0 {
		fmt.Fprintf(stdout, "%s the large tool output of %d skeleton runs: %d rows, about %s (their summaries stay)\n", verb, rep.ExcerptRuns, rep.ExcerptRows, mib(rep.ExcerptBytes))
	}
	if !dry {
		fmt.Fprintf(stdout, "returned %s to the disk\n", mib(rep.FreedBytes))
		if rep.ReusableMB >= 1 {
			fmt.Fprintf(stdout, "the file also holds %.1f MiB of reusable space: run `smind db compact` once to shrink it\n", rep.ReusableMB)
		}
		if rep.Incomplete {
			fmt.Fprintln(stdout, "the pass was interrupted: run it again to finish")
		}
	}
	return 0
}

// runDB is cmdDB with its environment injected.
func runDB(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] != "compact" {
		fmt.Fprint(stderr, dbUsage)
		return 2
	}
	call, closeFn, err := retentionDial(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "smind: %v\n", err)
		return 1
	}
	defer closeFn()
	var raw json.RawMessage
	if err := call(ctx, "db.compact", map[string]any{}, &raw); err != nil {
		fmt.Fprintf(stderr, "smind: %v\n", err)
		return 1
	}
	var res struct{ BeforeBytes, AfterBytes int64 }
	if err := json.Unmarshal(raw, &res); err != nil {
		fmt.Fprintf(stderr, "smind: db.compact: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "smind.db: %s -> %s (freed %s)\n", mib(res.BeforeBytes), mib(res.AfterBytes), mib(res.BeforeBytes-res.AfterBytes))
	return 0
}
