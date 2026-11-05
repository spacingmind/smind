package wsapi

import (
	"context"
	"encoding/json"
	"os"
	"runtime"
	"time"

	"github.com/spacingmind/smind/internal/runs"
)

// debug.memory is the daemon's own memory report for `smind debug memory`: Go
// runtime stats plus the run registry's history residency. Like pprof it is
// human-local only: a run-scoped agent connection and a relay session are
// refused (it is on no method list for either), and a direct connection must be
// loopback.

// debugMemoryMethods lists the diagnostics RPCs; a test walks it.
var debugMemoryMethods = []string{"debug.memory"}

// startedAt is when this process started, for the uptime line.
var startedAt = time.Now()

type debugMemoryResult struct {
	PID           int               `json:"pid"`
	UptimeSeconds int64             `json:"uptimeSeconds"`
	GoVersion     string            `json:"goVersion"`
	NumGoroutine  int               `json:"numGoroutine"`
	Mem           debugGoMem        `json:"mem"`
	Runs          runs.HistoryStats `json:"runs"`
}

// debugGoMem is the useful subset of runtime.MemStats, in bytes.
type debugGoMem struct {
	HeapAlloc    uint64 `json:"heapAlloc"`
	HeapInuse    uint64 `json:"heapInuse"`
	HeapIdle     uint64 `json:"heapIdle"`
	HeapReleased uint64 `json:"heapReleased"`
	HeapSys      uint64 `json:"heapSys"`
	StackInuse   uint64 `json:"stackInuse"`
	Sys          uint64 `json:"sys"`
	NumGC        uint32 `json:"numGC"`
	PauseTotalNs uint64 `json:"pauseTotalNs"`
	NextGC       uint64 `json:"nextGC"`
}

func handleDebugMemory(reg *runs.Registry) handlerFunc {
	return func(_ context.Context, rc *requestContext, _ json.RawMessage) (any, error) {
		switch {
		case rc == nil || rc.runScoped:
			return nil, forbidden("debug.memory is not available to agents")
		case !rc.local:
			return nil, forbidden("debug.memory is only available on a direct connection, not over the relay")
		case rc.channel != ChannelLoopback:
			return nil, forbidden("debug.memory needs a local (loopback) connection")
		}
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return debugMemoryResult{
			PID:           os.Getpid(),
			UptimeSeconds: int64(time.Since(startedAt).Seconds()),
			GoVersion:     runtime.Version(),
			NumGoroutine:  runtime.NumGoroutine(),
			Mem: debugGoMem{HeapAlloc: m.HeapAlloc, HeapInuse: m.HeapInuse, HeapIdle: m.HeapIdle, HeapReleased: m.HeapReleased,
				HeapSys: m.HeapSys, StackInuse: m.StackInuse, Sys: m.Sys, NumGC: m.NumGC, PauseTotalNs: m.PauseTotalNs, NextGC: m.NextGC},
			Runs: reg.HistoryStats(),
		}, nil
	}
}
