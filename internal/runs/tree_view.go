package runs

import (
	"sort"

	"github.com/spacingmind/smind/internal/tasktree"
)

// TaskRunFact is one task's run-derived facts for the task.tree read
// model: the latest run's state (tasktree's state vocabulary), the ids of
// the task's own running runs (oldest-started first), and how many
// permission requests are pending across the task's runs.
type TaskRunFact struct {
	State              tasktree.State
	RunningRunIDs      []string
	PendingPermissions int
}

// taskRunFactSnapshot is one run's contribution to TaskRunFacts, read
// under that run's own mu.
type taskRunFactSnapshot struct {
	id        string
	taskID    int64
	startedAt int64 // unix nanos, for oldest-first ordering
	status    Status
	pending   int
}

// TaskRunFacts summarizes the registry's in-memory runs per task id in
// ONE pass over reg.runs, matching ListFiltered's lock pattern: reg.mu to
// snapshot the run set, then each r.mu briefly, never nested. The latest
// run is the newest-started; a still-StatusRunning run outranks finished
// ones (and reports waiting_permission while any of them has a pending
// request). Tasks absent from the result are idle as far as the caller is
// concerned; runs absent from memory (persisted only, or evicted) are
// deliberately not reconstructed from the store.
func (reg *Registry) TaskRunFacts() map[int64]TaskRunFact {
	reg.mu.Lock()
	snaps := make([]taskRunFactSnapshot, 0, len(reg.runs))
	for _, r := range reg.runs {
		r.mu.Lock()
		snaps = append(snaps, taskRunFactSnapshot{
			id:        r.id,
			taskID:    r.taskID,
			startedAt: r.startedAt.UnixNano(),
			status:    r.status,
			pending:   len(r.pendingPermissions),
		})
		r.mu.Unlock()
	}
	reg.mu.Unlock()

	type acc struct {
		latest     taskRunFactSnapshot
		hasLatest  bool
		running    []taskRunFactSnapshot
		pending    int
		hasRunning bool
	}
	byTask := make(map[int64]*acc, len(snaps))
	for _, s := range snaps {
		a := byTask[s.taskID]
		if a == nil {
			a = &acc{}
			byTask[s.taskID] = a
		}
		a.pending += s.pending
		if s.status == StatusRunning {
			a.hasRunning = true
			a.running = append(a.running, s)
			continue
		}
		if !a.hasLatest || s.startedAt > a.latest.startedAt {
			a.latest = s
			a.hasLatest = true
		}
	}

	out := make(map[int64]TaskRunFact, len(byTask))
	for taskID, a := range byTask {
		f := TaskRunFact{PendingPermissions: a.pending, RunningRunIDs: []string{}}
		if a.hasRunning {
			// A running run is the task's live state; waiting_permission
			// wins while any run has a pending request (§4.1).
			f.State = tasktree.StateRunning
			if a.pending > 0 {
				f.State = tasktree.StateWaitingPermission
			}
			sort.Slice(a.running, func(i, j int) bool { return a.running[i].startedAt < a.running[j].startedAt })
			ids := make([]string, 0, len(a.running))
			for _, s := range a.running {
				ids = append(ids, s.id)
			}
			f.RunningRunIDs = ids
		} else if a.hasLatest {
			f.State = mapStatusToState(a.latest.status)
		} else {
			f.State = tasktree.StateIdle
		}
		out[taskID] = f
	}
	return out
}

// mapStatusToState maps a run status to tasktree's state vocabulary;
// interrupted (fate unknown after a restart) reports as error.
func mapStatusToState(s Status) tasktree.State {
	switch s {
	case StatusRunning:
		return tasktree.StateRunning
	case StatusDone:
		return tasktree.StateDone
	case StatusStopped:
		return tasktree.StateStopped
	case StatusError, StatusInterrupted:
		return tasktree.StateError
	default:
		return tasktree.StateIdle
	}
}
