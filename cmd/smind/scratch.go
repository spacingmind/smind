package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// scratchUsageResult and scratchCleanResult mirror the `scratch.usage` /
// `scratch.clean` wire results: plain counters, no paths, so nothing about
// the machine's filesystem leaks past what a human asked for.
type scratchUsageResult struct {
	TotalBytes       int64 `json:"totalBytes"`
	TotalFiles       int   `json:"totalFiles"`
	Dirs             int   `json:"dirs"`
	ReclaimableDirs  int   `json:"reclaimableDirs"`
	ReclaimableBytes int64 `json:"reclaimableBytes"`
}

type scratchCleanResult struct {
	Files int   `json:"files"`
	Bytes int64 `json:"bytes"`
	Dirs  int   `json:"dirs"`
}

// scratchDial is a variable so tests can stub the daemon connection
// without ever starting a real daemon or touching ~/.smind.
var scratchDial = func(ctx context.Context) (wsCaller, error) {
	client, err := dialDaemon(ctx)
	if err != nil {
		return nil, err
	}
	return client.Call, nil
}

func cmdScratch(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: smind scratch <usage|clean> [--json]")
		return 2
	}
	switch args[0] {
	case "usage":
		return cmdScratchUsage(args[1:])
	case "clean":
		return cmdScratchClean(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "smind scratch: unknown subcommand %q\n", args[0])
		return 2
	}
}

func cmdScratchUsage(args []string) int {
	jsonOut := false
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonOut = true
		default:
			fmt.Fprintln(osStderr, "usage: smind scratch usage [--json]")
			return 2
		}
	}
	return runScratchUsage(context.Background(), scratchDial, jsonOut, osStdout, osStderr)
}

func cmdScratchClean(args []string) int {
	jsonOut := false
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonOut = true
		default:
			fmt.Fprintln(osStderr, "usage: smind scratch clean [--json]")
			return 2
		}
	}
	return runScratchClean(context.Background(), scratchDial, jsonOut, osStdout, osStderr)
}

// runScratchUsage is the testable core of `smind scratch usage`: dial is
// injected so unit tests never need a daemon.
func runScratchUsage(ctx context.Context, dial func(context.Context) (wsCaller, error), jsonOut bool, stdout, stderr io.Writer) int {
	call, err := dial(ctx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	var raw json.RawMessage
	if err := call(ctx, "scratch.usage", nil, &raw); err != nil {
		fmt.Fprintf(stderr, "scratch usage: %v\n", err)
		return 1
	}

	if jsonOut {
		// The daemon's result verbatim, so keys this CLI does not model
		// are not dropped.
		var buf bytes.Buffer
		if err := json.Indent(&buf, raw, "", "  "); err != nil {
			fmt.Fprintf(stderr, "scratch usage: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, buf.String())
		return 0
	}
	var result scratchUsageResult
	if err := json.Unmarshal(raw, &result); err != nil {
		fmt.Fprintf(stderr, "scratch usage: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Scratch: %s in %d task dir(s)\n", humanBytes(result.TotalBytes), result.Dirs)
	fmt.Fprintf(stdout, "Reclaimable (closed tasks): %s in %d dir(s)\n", humanBytes(result.ReclaimableBytes), result.ReclaimableDirs)
	return 0
}

// runScratchClean is the testable core of `smind scratch clean`.
func runScratchClean(ctx context.Context, dial func(context.Context) (wsCaller, error), jsonOut bool, stdout, stderr io.Writer) int {
	call, err := dial(ctx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	var raw json.RawMessage
	if err := call(ctx, "scratch.clean", nil, &raw); err != nil {
		fmt.Fprintf(stderr, "scratch clean: %v\n", err)
		return 1
	}

	if jsonOut {
		var buf bytes.Buffer
		if err := json.Indent(&buf, raw, "", "  "); err != nil {
			fmt.Fprintf(stderr, "scratch clean: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, buf.String())
		return 0
	}
	var result scratchCleanResult
	if err := json.Unmarshal(raw, &result); err != nil {
		fmt.Fprintf(stderr, "scratch clean: %v\n", err)
		return 1
	}
	if result.Files == 0 && result.Dirs == 0 {
		fmt.Fprintln(stdout, "Nothing to clean.")
		return 0
	}
	fmt.Fprintf(stdout, "Removed %s (%d file(s)) from %d closed task dir(s).\n", humanBytes(result.Bytes), result.Files, result.Dirs)
	return 0
}

// humanBytes renders a byte count for the scratch lines: one decimal and
// the biggest of KB/MB/GB (1 KiB units, like formatKB), plain bytes below
// 1 KB.
func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}
