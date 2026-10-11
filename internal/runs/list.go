package runs

import (
	"sort"
)

// runListCap bounds how many runs ListFiltered returns: newest first, so
// the cap drops the oldest -- a read-only discovery listing (run.list with
// taskId/status filters, and the MCP run_list tool over it) must not grow
// with daemon uptime. Registry's own finishedRetentionCap already bounds
// what's knowable in memory; this is the wire-side backstop.
const runListCap = 200

// ListFiltered is List with the additive discovery filters of ADR-0016's
// later run.list extensions: taskID (nonzero) restricts to that task's
// runs across all of its chats, chatID (nonzero) to that chat's runs
// (both set: the intersection), and status (non-empty) to runs in exactly
// that status ("running", "done", ...). Everything else matches List:
// newest started first, usage filled in from the store for finished runs.
// The result is capped at runListCap entries, newest first.
func (reg *Registry) ListFiltered(taskID, chatID int64, status Status) []RunSummary {
	reg.mu.Lock()
	rs := make([]*run, 0, len(reg.runs))
	for _, r := range reg.runs {
		if taskID != 0 && r.taskID != taskID {
			continue
		}
		if chatID != 0 && r.chatID != chatID {
			continue
		}
		rs = append(rs, r)
	}
	reg.mu.Unlock()

	out := make([]RunSummary, 0, len(rs))
	for _, r := range rs {
		r.mu.Lock()
		s := r.statusLocked()
		r.mu.Unlock()
		if status != "" && s.Status != status {
			continue
		}
		out = append(out, s)
	}
	reg.withStoredUsage(out)
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	if len(out) > runListCap {
		out = out[:runListCap]
	}
	return out
}

// ValidRunListStatus reports whether s is one of the five Run lifecycle
// statuses run.list's status filter accepts -- everything except the
// zero value. Exported for wsapi's param validation.
func ValidRunListStatus(s Status) bool {
	switch s {
	case StatusRunning, StatusDone, StatusError, StatusStopped, StatusInterrupted:
		return true
	}
	return false
}
