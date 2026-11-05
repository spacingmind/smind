package runstats

import (
	"fmt"
	"io"
	"sort"
)

func mib(n int64) string { return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20)) }

func distRow(label string, d Dist) string {
	return fmt.Sprintf("  %-26s n=%-8d sum=%-11s p50=%-7d p90=%-7d p99=%-8d max=%d", label, d.N, mib(d.Sum), d.P50, d.P90, d.P99, d.Max)
}

// Render writes the human table of r: the same content as the JSON form,
// counts and sizes only.
func Render(w io.Writer, r *Report) {
	p := func(f string, a ...any) { fmt.Fprintf(w, f+"\n", a...) }
	p("run-stats (counts and sizes only)")
	p("file %s, freelist %s; run_events table %s + indexes %s; other tables %s",
		mib(r.DB.FileBytes), mib(r.DB.FreelistBytes), mib(r.DB.RunEventsTableB), mib(r.DB.RunEventsIndexB), mib(r.DB.OtherBytes))
	p("runs %d, rows %d, event_data %s, per-row overhead %d B (%s)", r.Runs, r.Rows, mib(r.EventBytes), r.DB.RowOverheadBytes, r.DB.RowOverheadSource)
	p("\nrows and bytes per run (event_data bytes)")
	p("%s", distRow("rows per run", r.RowsPerRun))
	p("%s", distRow("bytes per run", r.BytesPerRun))
	p("\nby event type (event_data bytes per row)")
	for _, t := range r.ByType {
		p("%s", distRow(t.Type, t.Size))
	}
	p("\nby provider")
	for _, v := range r.ByProvider {
		p("  %s: runs %d, rows %d, %s", v.Provider, v.Runs, v.Rows, mib(v.Bytes))
		p("%s", distRow("rows per run", v.RowsPerRun))
		p("%s", distRow("text/thinking chunk bytes", v.TextChunk))
		p("%s", distRow("tool input bytes", v.ToolInput))
		p("%s", distRow("tool result bytes", v.ToolResult))
	}
	p("\nby run age (started)")
	var ages []string
	for k := range r.ByAge {
		ages = append(ages, k)
	}
	sort.Strings(ages)
	for _, k := range ages {
		p("  %-8s rows %-9d %s", k, r.ByAge[k].Rows, mib(r.ByAge[k].Bytes))
	}
	p("\nby tool (%d distinct calls)", r.ToolCalls)
	for _, t := range r.ByTool {
		p("  %s: calls %d", t.Tool, t.Calls)
		p("%s", distRow("input bytes", t.Input))
		p("%s", distRow("result bytes", t.Result))
	}
	p("%s", distRow("\nall payloads (in+result)", r.Payload))
	c := r.Coalescing
	p("\nchunk coalescing (PR1 model: flush on boundary, %d B, %s idle)", ChunkFlushBytes, ChunkIdle)
	p("  chunk rows now %d (%s) -> boundary+4KiB %d (%s) -> +250ms idle %d (%s); saved incl. row overhead %s",
		c.ChunkRowsNow, mib(c.ChunkBytesNow), c.RowsBoundaryAnd4K, mib(c.BytesBoundaryAnd4K), c.RowsWithIdle, mib(c.BytesWithIdle), mib(c.SavedBytesWithIdle))
	p("%s", distRow("contiguous answer run", c.AnswerRun))
	p("\nthreshold grid (payload bytes only; whole cap = head+tail above it)")
	p("  %-7s %-7s %-9s %-12s %-12s %-12s %s", "inline", "whole", "whole%", "after-prune", "with-excerpt", "excerpt-tier", "saved%")
	for _, t := range r.Thresholds {
		p("  %-7d %-7d %-9.1f %-12s %-12s %-12s %.1f", t.InlineCap, t.WholeCap, t.PayloadsWhole, mib(t.SkeletonOnlyBytes), mib(t.WithExcerptBytes), mib(t.ExcerptTierBytes), t.SavedPct)
	}
	e := r.Estimate
	p("\nwhole-database estimate (rows incl. overhead)")
	p("  now %s | after coalescing %s | skeleton + excerpts %s | skeleton only %s | thinking now %s", mib(e.CurrentBytes), mib(e.AfterCoalescingBytes), mib(e.SkeletonWithExcerptsBytes), mib(e.SkeletonOnlyBytes), mib(e.ThinkingBytesNow))
	p("  assumptions: %s", e.Assumptions)
}
