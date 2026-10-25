// Package stall detects a task tree that has gone silent: a seat (a Lead or a
// task with live children) whose subtree has not run or queued anything for
// the configured number of minutes while the tree still has unfinished work.
// It only reports; it never wakes, archives or recovers anything. The rules
// are in docs/plans/active/stalled-tree-alert.md.
package stall

import (
	"context"
	"sort"
	"time"

	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
)

// Episode is one stall: the seat and the instant its subtree last did
// anything. A seat that stalls again after new activity has a new Since.
type Episode struct {
	TaskID int64
	Title  string
	Since  time.Time
	// AlreadyAlerted is true when the persisted marker equals Since.
	AlreadyAlerted bool
}

type agg struct {
	running bool
	// activity is the subtree activity time A (without the seat's own
	// rate-limit reset, which is added per seat).
	activity time.Time
	done     bool // every live node in the subtree is finished
}

// Detect returns the stalled seats among rows (live, kind='task' tasks), in
// task id order. n <= 0 turns detection off.
func Detect(rows []store.StallRow, now time.Time, n time.Duration) []Episode {
	if n <= 0 || len(rows) == 0 {
		return nil
	}
	byID := make(map[int64]*store.StallRow, len(rows))
	for i := range rows {
		byID[rows[i].TaskID] = &rows[i]
	}
	children := make(map[int64][]int64, len(rows))
	for i := range rows {
		r := &rows[i]
		if r.ParentID != nil {
			if _, live := byID[*r.ParentID]; live {
				children[*r.ParentID] = append(children[*r.ParentID], r.TaskID)
			}
		}
	}

	memo := make(map[int64]agg, len(rows))
	visiting := make(map[int64]bool, len(rows))
	var walk func(id int64) agg
	walk = func(id int64) agg {
		if a, ok := memo[id]; ok {
			return a
		}
		if visiting[id] { // parent cycle: do not recurse into it again
			return agg{done: true}
		}
		visiting[id] = true
		r := byID[id]
		a := agg{running: r.Running, activity: r.LastActivity, done: finished(r)}
		if r.QueuedHold != nil && r.QueuedHold.After(a.activity) {
			a.activity = *r.QueuedHold
		}
		for _, c := range children[id] {
			ca := walk(c)
			a.running = a.running || ca.running
			a.done = a.done && ca.done
			if ca.activity.After(a.activity) {
				a.activity = ca.activity
			}
		}
		visiting[id] = false
		memo[id] = a
		return a
	}
	for i := range rows {
		walk(rows[i].TaskID)
	}

	rootOf := func(id int64) int64 {
		seen := map[int64]bool{id: true}
		for {
			p := byID[id].ParentID
			if p == nil {
				return id
			}
			next, live := byID[*p]
			if !live || seen[next.TaskID] {
				return id
			}
			seen[next.TaskID] = true
			id = next.TaskID
		}
	}

	var out []Episode
	for i := range rows {
		r := &rows[i]
		if len(children[r.TaskID]) == 0 && !r.ProfileOrchestration {
			continue // not a seat
		}
		if memo[rootOf(r.TaskID)].done || memo[r.TaskID].running {
			continue
		}
		a := memo[r.TaskID].activity
		if r.LatestFailureKind == "rate_limited" && r.ProfileOnRateLimit == "wait" &&
			r.LatestFailureResetAt != nil && r.LatestFailureResetAt.After(a) {
			a = *r.LatestFailureResetAt
		}
		if now.Sub(a) < n {
			continue
		}
		since := a.UTC().Truncate(time.Second)
		out = append(out, Episode{
			TaskID:         r.TaskID,
			Title:          r.Title,
			Since:          since,
			AlreadyAlerted: r.StallAlertedSince != nil && r.StallAlertedSince.UTC().Truncate(time.Second).Equal(since),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TaskID < out[j].TaskID })
	return out
}

// finished mirrors wsapi.isFinishedNode from the node's own DB facts: the
// latest run reached a terminal state without a needs-continue disposition,
// and no prompt is queued.
func finished(r *store.StallRow) bool {
	if !r.HasRun || r.HasQueued {
		return false
	}
	switch runs.Status(r.LatestStatus) {
	case runs.StatusDone:
		return runs.DispositionFor(runs.StatusDone, r.LatestStopReason) != runs.DispositionNeedsContinue
	case runs.StatusError, runs.StatusStopped, runs.StatusInterrupted:
		return true
	}
	return false
}

// Deps are the detector's collaborators; every one is required.
type Deps struct {
	Snapshot func() ([]store.StallRow, error)
	// Mark persists the alerted episode; it runs before Alert so a crash
	// between them loses at most one alert and never repeats one.
	Mark    func(taskID int64, since time.Time) error
	Alert   func(ep Episode, minutes int)
	Now     func() time.Time
	Minutes func() int
}

// Detector runs Detect over a store snapshot once per Tick.
type Detector struct{ d Deps }

// New builds a Detector.
func New(d Deps) *Detector { return &Detector{d: d} }

func (dt *Detector) episodes() ([]Episode, int) {
	m := dt.d.Minutes()
	if m <= 0 {
		return nil, m
	}
	rows, err := dt.d.Snapshot()
	if err != nil {
		return nil, m
	}
	return Detect(rows, dt.d.Now(), time.Duration(m)*time.Minute), m
}

// Tick runs one detection pass: one snapshot, no per-task queries, and an
// alert for every stalled episode not yet alerted.
func (dt *Detector) Tick() {
	eps, m := dt.episodes()
	for _, ep := range eps {
		if ep.AlreadyAlerted {
			continue
		}
		if err := dt.d.Mark(ep.TaskID, ep.Since); err != nil {
			continue // keep the episode unmarked and retry next tick
		}
		dt.d.Alert(ep, m)
	}
}

// Current maps each currently stalled seat to its stalledSince, for the
// task.tree marker. It uses the same Detect as Tick so the two agree.
func (dt *Detector) Current() map[int64]time.Time {
	eps, _ := dt.episodes()
	if len(eps) == 0 {
		return nil
	}
	out := make(map[int64]time.Time, len(eps))
	for _, ep := range eps {
		out[ep.TaskID] = ep.Since
	}
	return out
}

// Run ticks every period until ctx is done.
func (dt *Detector) Run(ctx context.Context, period time.Duration) {
	t := time.NewTicker(period)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			dt.Tick()
		}
	}
}
