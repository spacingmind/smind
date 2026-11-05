package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/spacingmind/smind/internal/runstats"
)

// runRunStats is the hidden `smind debug run-stats --db <path> [--json]`: the
// ADR-0053 payload measurement. It reads the one file it is given, read-only
// and offline (no daemon), refuses a live database, and prints counts, sizes
// and percentiles only. Exit codes: 0 ok, 1 failure, 2 usage.
func runRunStats(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("debug run-stats", flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := fs.String("db", "", "database snapshot to measure (required; a copy, never the live smind.db)")
	asJSON := fs.Bool("json", false, "print JSON instead of the table")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		return 2
	}
	if *path == "" {
		fmt.Fprintln(stderr, "usage: smind debug run-stats --db <snapshot.db> [--json]")
		return 2
	}
	db, err := runstats.Open(*path)
	if err != nil {
		fmt.Fprintf(stderr, "smind: %v\n", err)
		return 1
	}
	defer db.Close()
	rep, err := runstats.Compute(ctx, db, *path, time.Now())
	if err != nil {
		fmt.Fprintf(stderr, "smind: run-stats: %v\n", err)
		return 1
	}
	if *asJSON {
		b, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Fprintln(stdout, string(b))
		return 0
	}
	runstats.Render(stdout, rep)
	return 0
}
