package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const debugUsage = `Usage:
  smind debug memory [--json]    the running daemon's memory: Go heap, process RSS/footprint, run history cache
`

// debugDial is a variable so tests can stub the daemon connection.
var debugDial = func(ctx context.Context) (wsCaller, func(), error) {
	client, err := dialDaemon(ctx)
	if err != nil {
		return nil, nil, err
	}
	return client.Call, func() { _ = client.Close() }, nil
}

// processMemory reads the daemon's resident set (and, on macOS, its
// phys_footprint) from the OS by pid. Best effort: a field is 0 when the probe
// is unavailable. A var so tests do not shell out.
var processMemory = func(pid int) (rss, footprint int64) {
	if runtime.GOOS == "windows" || pid <= 0 {
		return 0, 0
	}
	if out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output(); err == nil {
		if kb, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64); err == nil {
			rss = kb << 10
		}
	}
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("footprint", "-p", strconv.Itoa(pid), "-f", "bytes").Output(); err == nil {
			footprint = parseFootprintBytes(string(out))
		}
	}
	return rss, footprint
}

// parseFootprintBytes finds "Footprint: <n> B" in `footprint -f bytes` output.
func parseFootprintBytes(out string) int64 {
	const key = "Footprint:"
	i := strings.Index(out, key)
	if i < 0 {
		return 0
	}
	f := strings.Fields(out[i+len(key):])
	if len(f) < 2 || f[1] != "B" {
		return 0
	}
	n, err := strconv.ParseInt(f[0], 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func cmdDebug(args []string) int {
	return runDebug(context.Background(), args, osStdout, osStderr)
}

// runDebug is cmdDebug with its environment injected. Exit codes: 0 ok, 1 a
// daemon or call error, 2 usage.
func runDebug(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "memory" {
		fmt.Fprint(stderr, debugUsage)
		return 2
	}
	jsonOut := false
	for _, a := range args[1:] {
		if a != "--json" {
			fmt.Fprint(stderr, debugUsage)
			return 2
		}
		jsonOut = true
	}
	call, closeFn, err := debugDial(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "smind: %v\n", err)
		return 1
	}
	defer closeFn()
	var raw json.RawMessage
	if err := call(ctx, "debug.memory", map[string]any{}, &raw); err != nil {
		fmt.Fprintf(stderr, "smind: %v\n", err)
		return 1
	}
	var res debugMemoryView
	if err := json.Unmarshal(raw, &res); err != nil {
		fmt.Fprintf(stderr, "smind: debug.memory: %v\n", err)
		return 1
	}
	rss, footprint := processMemory(res.PID)
	if jsonOut {
		out := map[string]any{"daemon": json.RawMessage(raw), "rssBytes": rss, "physFootprintBytes": footprint}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Fprintln(stdout, string(b))
		return 0
	}
	printDebugMemory(stdout, res, rss, footprint)
	return 0
}

// debugMemoryView mirrors the debug.memory wire result.
type debugMemoryView struct {
	PID           int    `json:"pid"`
	UptimeSeconds int64  `json:"uptimeSeconds"`
	GoVersion     string `json:"goVersion"`
	NumGoroutine  int    `json:"numGoroutine"`
	Mem           struct {
		HeapAlloc, HeapInuse, HeapIdle, HeapReleased, HeapSys, StackInuse, Sys, NextGC, PauseTotalNs uint64
		NumGC                                                                                        uint32
	} `json:"mem"`
	Runs struct {
		RunsInMemory     int   `json:"runsInMemory"`
		RunningRuns      int   `json:"runningRuns"`
		ResidentRuns     int   `json:"residentRuns"`
		ResidentBytes    int64 `json:"residentBytes"`
		BudgetBytes      int64 `json:"budgetBytes"`
		RunningHistBytes int64 `json:"runningHistoryBytes"`
		Loads            int64 `json:"loads"`
		Evictions        int64 `json:"evictions"`
		StoreReads       int64 `json:"storeReads"`
	} `json:"runs"`
}

func mib(n int64) string { return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20)) }

func printDebugMemory(w io.Writer, r debugMemoryView, rss, footprint int64) {
	fmt.Fprintf(w, "daemon pid %d, up %s, %s, %d goroutines\n\n", r.PID, (time.Duration(r.UptimeSeconds) * time.Second).String(), r.GoVersion, r.NumGoroutine)
	fmt.Fprintln(w, "process")
	if footprint > 0 {
		fmt.Fprintf(w, "  phys_footprint   %s\n", mib(footprint))
	}
	if rss > 0 {
		fmt.Fprintf(w, "  rss              %s\n", mib(rss))
	}
	fmt.Fprintln(w, "go heap")
	fmt.Fprintf(w, "  heap alloc       %s (live objects)\n", mib(int64(r.Mem.HeapAlloc)))
	fmt.Fprintf(w, "  heap in use      %s\n  heap idle        %s (released %s)\n  heap sys         %s\n", mib(int64(r.Mem.HeapInuse)), mib(int64(r.Mem.HeapIdle)), mib(int64(r.Mem.HeapReleased)), mib(int64(r.Mem.HeapSys)))
	fmt.Fprintf(w, "  next gc at       %s\n  gc cycles        %d (pause total %s)\n  stack in use     %s\n  total from os    %s\n", mib(int64(r.Mem.NextGC)), r.Mem.NumGC, time.Duration(r.Mem.PauseTotalNs), mib(int64(r.Mem.StackInuse)), mib(int64(r.Mem.Sys)))
	fmt.Fprintln(w, "run history")
	fmt.Fprintf(w, "  runs in memory   %d (%d running)\n", r.Runs.RunsInMemory, r.Runs.RunningRuns)
	fmt.Fprintf(w, "  resident finished %d runs, %s of %s budget\n", r.Runs.ResidentRuns, mib(r.Runs.ResidentBytes), mib(r.Runs.BudgetBytes))
	fmt.Fprintf(w, "  running runs     %s of history (never evicted)\n", mib(r.Runs.RunningHistBytes))
	fmt.Fprintf(w, "  loads %d, evictions %d, store reads %d\n", r.Runs.Loads, r.Runs.Evictions, r.Runs.StoreReads)
}
