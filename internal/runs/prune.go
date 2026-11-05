package runs

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// Run events retention (docs/plans/active/run-events-retention.md): finished
// runs older than the configured number of days lose their event rows but keep
// the run row, usage, failure class and a final text.

// finalTextMax caps the final text kept on a pruned run.
const finalTextMax = 64 << 10

const (
	pruneBatchRows   = 2000
	pruneBatchPause  = 20 * time.Millisecond
	pruneStartDelay  = 2 * time.Minute
	pruneInterval    = 24 * time.Hour
	pruneVacuumPages = 200
)

// ErrPruneRunning is returned when a prune pass is already in progress.
var ErrPruneRunning = errors.New("runs: a prune pass is already running")

// SetRetentionDays tells the registry the retention it reports in the pruned
// marker (0 = unknown or disabled).
func (reg *Registry) SetRetentionDays(days int) {
	reg.mu.Lock()
	reg.retentionDays = days
	reg.mu.Unlock()
}

// RunningCount is how many runs are running right now.
func (reg *Registry) RunningCount() int {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	n := 0
	for _, r := range reg.runs {
		r.mu.Lock()
		if r.status == StatusRunning {
			n++
		}
		r.mu.Unlock()
	}
	return n
}

// markHistoryPruned records that run id's events were pruned: it drops any
// resident copy so every later read serves the summary.
func (reg *Registry) markHistoryPruned(id string, at time.Time) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	r, ok := reg.runs[id]
	if !ok {
		return
	}
	reg.dropHistLocked(r, true)
	r.mu.Lock()
	r.histPrunedAt = &at
	r.history, r.histEvicted = nil, true
	r.mu.Unlock()
}

// prunedSummary is what a pruned run shows in place of its events: a note that
// says what happened (it renders as its own paragraph in every client), the
// kept final text, and the stop reason.
func (reg *Registry) prunedSummary(runID, stopReason string, at time.Time) []Event {
	reg.mu.Lock()
	days := reg.retentionDays
	reg.mu.Unlock()
	note := fmt.Sprintf("detailed history pruned on %s", at.UTC().Format("2006-01-02"))
	if days > 0 {
		note = fmt.Sprintf("detailed history pruned after %d days (on %s)", days, at.UTC().Format("2006-01-02"))
	}
	out := []Event{{Type: taskrunner.EventTypeSessionNote, Text: note}}
	if text, err := reg.st.GetRunFinalText(runID); err == nil && text != "" {
		out = append(out, Event{Type: taskrunner.EventTypeText, Text: text})
	}
	return append(out, Event{Type: taskrunner.EventTypeDone, StopReason: stopReason})
}

// capText cuts s to at most max bytes on a rune boundary.
func capText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max]
}

// PruneReport is the result of a prune pass or a dry run.
type PruneReport struct {
	// Enabled is false when retention is off (0 days): nothing is pruned.
	Enabled      bool      `json:"enabled"`
	Days         int       `json:"days"`
	Cutoff       time.Time `json:"cutoff"`
	DryRun       bool      `json:"dryRun"`
	Runs         int64     `json:"runs"`
	Rows         int64     `json:"rows"`
	Bytes        int64     `json:"bytes"`
	ExcerptRuns  int64     `json:"excerptRuns"`
	ExcerptRows  int64     `json:"excerptRows"`
	ExcerptBytes int64     `json:"excerptBytes"`
	FreedBytes   int64     `json:"freedBytes"`
	ReusableMB   float64   `json:"reusableMB"`
	Duration     string    `json:"duration,omitempty"`
	Incomplete   bool      `json:"incomplete,omitempty"`
}

// Pruner runs retention passes: a daily job and on-demand passes.
type Pruner struct {
	reg  *Registry
	st   *store.Store
	days func() int
	now  func() time.Time

	// batch, pause are the bounded-batch knobs (tests shrink them).
	batch      int
	pause      time.Duration
	startDelay time.Duration

	running sync.Mutex
}

// NewPruner returns a Pruner reading the retention days from days on each pass
// (so a config edit applies without a restart).
func NewPruner(reg *Registry, st *store.Store, days func() int) *Pruner {
	return &Pruner{reg: reg, st: st, days: days, now: time.Now, batch: pruneBatchRows, pause: pruneBatchPause, startDelay: pruneStartDelay}
}

func (p *Pruner) cutoff() (days int, cutoff time.Time) {
	days = p.days()
	return days, p.now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
}

// Preview reports what a pass would prune now, changing nothing.
func (p *Pruner) Preview() (PruneReport, error) {
	days, cutoff := p.cutoff()
	rep := PruneReport{Days: days, Cutoff: cutoff, DryRun: true}
	if days <= 0 {
		return rep, nil
	}
	rep.Enabled = true
	c, err := p.st.PrunePreview(cutoff)
	if err != nil {
		return rep, err
	}
	rep.Runs, rep.Rows, rep.Bytes = c.Runs, c.Rows, c.Bytes
	rep.ExcerptRuns, rep.ExcerptRows, rep.ExcerptBytes = c.ExcerptRuns, c.ExcerptRows, c.ExcerptBytes
	return rep, nil
}

// Run does one pass. A second concurrent call returns ErrPruneRunning.
func (p *Pruner) Run(ctx context.Context) (PruneReport, error) {
	if !p.running.TryLock() {
		return PruneReport{}, ErrPruneRunning
	}
	defer p.running.Unlock()
	start := time.Now()
	days, cutoff := p.cutoff()
	rep := PruneReport{Days: days, Cutoff: cutoff}
	if days <= 0 {
		return rep, nil
	}
	rep.Enabled = true
	p.reg.SetRetentionDays(days)
	pre, err := p.st.PrunePreview(cutoff)
	if err != nil {
		return rep, err
	}
	rep.Bytes = pre.Bytes
	rep.ExcerptBytes = pre.ExcerptBytes

	seen := map[string]bool{}
	for ctx.Err() == nil {
		ids, err := p.st.PruneCandidates(cutoff, 50)
		if err != nil {
			return rep, err
		}
		progressed := false
		for _, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			progressed = true
			rows, excerpts, err := p.pruneRun(ctx, id, cutoff)
			if err != nil {
				return rep, err
			}
			if excerpts {
				if rows > 0 {
					rep.ExcerptRuns++
					rep.ExcerptRows += rows
				}
			} else if rows > 0 {
				rep.Runs++
				rep.Rows += rows
			}
			if ctx.Err() != nil {
				break
			}
		}
		if !progressed {
			break
		}
	}
	rep.Incomplete = ctx.Err() != nil
	if rep.Rows > 0 {
		rep.FreedBytes = p.reclaim(ctx)
	}
	if pages, size, err := p.st.FreePages(); err == nil {
		rep.ReusableMB = float64(pages*size) / (1 << 20)
	}
	rep.Duration = time.Since(start).Round(time.Millisecond).String()
	log.Printf("runs: retention pass: %d runs, %d event rows pruned, %d excerpt rows of %d skeleton runs, %d bytes returned to the file system (cutoff %s)", rep.Runs, rep.Rows, rep.ExcerptRows, rep.ExcerptRuns, rep.FreedBytes, cutoff.Format(time.RFC3339))
	return rep, nil
}

// pruneRun prunes one run and returns the rows deleted and whether they were
// excerpt rows. A run in skeleton form keeps its rows: only its excerpt tier
// goes, so every reader keeps serving the skeleton (a payload whose excerpt is
// gone reads back as its summary). A run in full form is pruned in the two
// phases as before.
func (p *Pruner) pruneRun(ctx context.Context, id string, cutoff time.Time) (rows int64, excerpts bool, err error) {
	sk, err := p.st.GetRunSkeleton(id)
	if err != nil {
		return 0, false, err
	}
	if sk.SkeletonAt != nil {
		var total int64
		for ctx.Err() == nil {
			n, err := p.st.PruneExcerptsBatch(id, p.batch, p.now().UTC(), cutoff)
			if err != nil {
				return total, true, err
			}
			total += n
			if n == 0 {
				break
			}
			time.Sleep(p.pause)
		}
		p.reg.dropResident(id)
		return total, true, nil
	}
	row, err := p.st.GetRun(id)
	if err != nil {
		return 0, false, err
	}
	if row.HistoryPrunedAt == nil {
		text, _ := p.reg.LastAgentText(id)
		now := p.now().UTC()
		ok, err := p.st.MarkRunPruned(id, capText(text, finalTextMax), now, cutoff)
		if err != nil {
			return 0, false, err
		}
		if !ok {
			return 0, false, nil // no longer eligible: left untouched
		}
		p.reg.markHistoryPruned(id, now)
	} else {
		p.reg.markHistoryPruned(id, *row.HistoryPrunedAt)
	}
	var total int64
	for ctx.Err() == nil {
		n, err := p.st.DeleteRunEventsBatch(id, p.batch)
		if err != nil {
			return total, false, err
		}
		total += n
		if n == 0 {
			break
		}
		time.Sleep(p.pause)
	}
	return total, false, nil
}

// dropResident drops a run's resident history without marking it pruned: the
// run is not "pruned" (its skeleton rows are still in the store), so the next
// read loads them again instead of serving the prune summary.
func (reg *Registry) dropResident(id string) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	r, ok := reg.runs[id]
	if !ok {
		return
	}
	r.mu.Lock()
	running := r.status == StatusRunning
	r.mu.Unlock()
	if running {
		return
	}
	reg.dropHistLocked(r, true)
	r.mu.Lock()
	r.history, r.histEvicted = nil, true
	r.mu.Unlock()
}

// reclaim returns freed pages to the OS when the file allows it and truncates
// the WAL. It never runs a full VACUUM (that is `smind db compact`).
func (p *Pruner) reclaim(ctx context.Context) int64 {
	var freed int64
	for ctx.Err() == nil {
		n, err := p.st.IncrementalVacuum(pruneVacuumPages)
		if err != nil || n <= 0 {
			break
		}
		freed += n
		time.Sleep(p.pause)
	}
	_ = p.st.Checkpoint()
	return freed
}

// Start runs the daily job until ctx ends: the first pass shortly after start,
// then every 24 hours. Passes never overlap.
func (p *Pruner) Start(ctx context.Context) {
	go func() {
		timer := time.NewTimer(p.startDelay)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
			if _, err := p.Run(ctx); err != nil && !errors.Is(err, ErrPruneRunning) {
				log.Printf("runs: retention pass failed: %v", err)
			}
			timer.Reset(pruneInterval)
		}
	}()
}
