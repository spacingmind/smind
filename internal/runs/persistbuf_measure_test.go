package runs

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// TestMeasureCoalescingReplay replays a stream shaped like the real measurement
// of ADR-0053 (98% of events are small streamed chunks, mostly thinking, in long runs of
// one kind with a few answer bursts; a tool call every ~170 events with a payload
// of 200 B to 4 KB) into
// two stores: one row per event (the old write path) and the real record path.
// It logs rows, file size and write time. Opt in: SMIND_MEASURE_RUNS=100
// go test ./internal/runs -run MeasureCoalescingReplay -v
func TestMeasureCoalescingReplay(t *testing.T) {
	n, _ := strconv.Atoi(os.Getenv("SMIND_MEASURE_RUNS"))
	if n <= 0 {
		t.Skip("SMIND_MEASURE_RUNS not set")
	}
	rng := rand.New(rand.NewSource(1))
	stream := func() []Event {
		var evs []Event
		evs = append(evs, Event{Type: taskrunner.EventTypeUserMessage, Text: "go"})
		for i := 0; i < 5000; i++ {
			switch {
			case i%170 == 169:
				id := fmt.Sprintf("c%d", i)
				evs = append(evs, toolCmd(id, "go test ./..."), tool(id, taskrunner.ToolStatusSuccess, "", strings.Repeat("x", 200+rng.Intn(4000))))
			case i%1000 < 50 && i >= 1000: // a few answer bursts per run, as in the real data
				evs = append(evs, txt("word "))
			default:
				evs = append(evs, think("tok "[:2+rng.Intn(3)]))
			}
		}
		return append(evs, Event{Type: taskrunner.EventTypeDone, StopReason: "end_turn"})
	}

	legacy := newHistEnv(t)
	var legacyEvents int
	t0 := time.Now()
	started := time.Now().UTC()
	for i := 0; i < n; i++ {
		evs := stream()
		legacyEvents += len(evs)
		seedFinished(t, legacy.st, legacy.task.ID, fmt.Sprintf("r%d", i), started, evs)
	}
	legacyDur := time.Since(t0)

	coal := newHistEnv(t)
	reg := newTestRegistry(t, coal.st)
	t0 = time.Now()
	for i := 0; i < n; i++ {
		live := registerLive(t, coal, reg, fmt.Sprintf("r%d", i), started)
		for _, ev := range stream() {
			reg.record(live, ev)
		}
		reg.flushPersist(live)
	}
	coalDur := time.Since(t0)

	// ADR-0053 finalizer on the coalesced runs: time per run and the result.
	fin := time.Now().UTC()
	for i := 0; i < n; i++ {
		if _, err := coal.st.UpdateRunStatus(fmt.Sprintf("r%d", i), "done", &fin, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	t0 = time.Now()
	for i := 0; i < n; i++ {
		if err := Skeletonize(coal.st, fmt.Sprintf("r%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	finalizeDur := time.Since(t0)
	var skelBytes int64
	var skelRows int
	for i := 0; i < n; i++ {
		x, _ := coal.st.ListRunEventsRaw(fmt.Sprintf("r%d", i))
		skelRows += len(x)
		for _, row := range x {
			skelBytes += int64(len(row.EventData))
		}
	}
	var excerptBytes int64
	_ = coal.st.DB().QueryRow(`SELECT COALESCE(SUM(length(head) + length(tail)), 0) FROM run_excerpts`).Scan(&excerptBytes)

	count := func(e *histEnv) (r int, b int64) {
		for i := 0; i < n; i++ {
			x, _ := e.st.ListRunEvents(fmt.Sprintf("r%d", i))
			r += len(x)
			for _, row := range x {
				b += int64(len(row.EventData))
			}
		}
		return
	}
	// 174 B is the per-row table + index overhead measured on the real database.
	const rowOverhead = 174
	lr, lb := count(legacy)
	cr, cb := count(coal)
	est := func(r int, b int64) float64 { return float64(b+int64(r)*rowOverhead) / (1 << 20) }
	t.Logf("runs %d, events %d", n, legacyEvents)
	t.Logf("rows: one-per-event %d -> coalesced %d (%.1f%%)", lr, cr, 100*float64(cr)/float64(lr))
	t.Logf("event_data: %.1f MiB -> %.1f MiB; estimated on disk at 174 B/row: %.1f MiB -> %.1f MiB (%.0f%%)",
		float64(lb)/(1<<20), float64(cb)/(1<<20), est(lr, lb), est(cr, cb), 100*est(cr, cb)/est(lr, lb))
	est2 := func(r int, b int64) float64 { return float64(b+int64(r)*rowOverhead) / (1 << 20) }
	t.Logf("skeleton form: %d rows, row bytes %.1f MiB + excerpts %.1f MiB; estimated on disk (rows at 174 B) %.1f MiB; finalizer %.1f ms/run",
		skelRows, float64(skelBytes)/(1<<20), float64(excerptBytes)/(1<<20), est2(skelRows, skelBytes+excerptBytes), float64(finalizeDur.Microseconds())/1000/float64(n))
	t.Logf("write time: %v -> %v (%.1f us/event -> %.1f us/event, includes fixture cost)", legacyDur.Round(time.Millisecond), coalDur.Round(time.Millisecond),
		float64(legacyDur.Microseconds())/float64(legacyEvents), float64(coalDur.Microseconds())/float64(legacyEvents))
}
