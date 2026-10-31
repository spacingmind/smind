package runs

import (
	"sort"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
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
	return reg.ListWith(RunFilter{TaskID: taskID, ChatID: chatID, Status: status})
}

// RunFilter is ListWith's filter; the zero value matches ListFiltered(0, 0, "").
type RunFilter struct {
	TaskID, ChatID int64
	Status         Status
	// IncludeSystem also returns system runs (purpose != user, ADR-0026),
	// which List/ListFiltered hide.
	IncludeSystem bool
}

// ListWith is ListFiltered with the full RunFilter.
func (reg *Registry) ListWith(f RunFilter) []RunSummary {
	taskID, chatID, status := f.TaskID, f.ChatID, f.Status
	reg.mu.Lock()
	rs := make([]*run, 0, len(reg.runs))
	for _, r := range reg.runs {
		if taskID != 0 && r.taskID != taskID {
			continue
		}
		if chatID != 0 && r.chatID != chatID {
			continue
		}
		if r.purpose != "" && !f.IncludeSystem {
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

// ListTaskHistory is ListFiltered(taskID, 0, "") plus the task's persisted
// runs the Registry no longer holds (evicted past finishedRetentionCap, or
// never rehydrated): read-only history, e.g. of an archived task. A run in
// both is reported once, from memory. Newest first, capped at runListCap.
func (reg *Registry) ListTaskHistory(taskID int64) []RunSummary {
	out := reg.ListFiltered(taskID, 0, "")
	rows, err := reg.st.ListRunsByTask(taskID, runListCap)
	if err != nil {
		return out
	}
	known := make(map[string]bool, len(out))
	for _, s := range out {
		known[s.ID] = true
	}
	var stored []RunSummary
	for _, row := range rows {
		if !known[row.ID] {
			stored = append(stored, reg.summaryFromRow(row))
		}
	}
	if len(stored) == 0 {
		return out
	}
	reg.withStoredUsage(stored)
	out = append(out, stored...)
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	if len(out) > runListCap {
		out = out[:runListCap]
	}
	return out
}

// summaryFromRow is the RunSummary of a persisted run row (a terminal run:
// reconciliation ran at startup), without its event history. Usage is left
// to withStoredUsage.
func (reg *Registry) summaryFromRow(row store.Run) RunSummary {
	rs := RunSummary{
		ID: row.ID, TaskID: row.TaskID, ChatID: row.ChatID,
		Provider: taskrunner.Provider(row.Provider), Prompt: row.Prompt,
		Status: Status(row.Status), StartedAt: row.StartedAt, FinishedAt: row.FinishedAt,
		StopReason: row.StopReason, Disposition: DispositionOf(DispositionFacts{
			Status: Status(row.Status), StopReason: row.StopReason, FailureKind: row.FailureKind, FailureResetAt: row.FailureResetAt,
			HasSession: row.Status == string(StatusError) && resumableKind(row.FailureKind) && chatHasSession(reg.st, row.ChatID), Now: time.Now(),
		}),
		Err: row.ErrMsg, PermissionMode: row.PermissionMode, AutoAccept: row.AutoAccept,
		Model: row.Model, Purpose: rowPurpose(row.Purpose), Origin: row.Origin,
		Attachments:    publicItems(decodeStoredAttachments(row.Attachments)),
		ProfileID:      row.ProfileID,
		FailoverOf:     row.FailoverOf,
		FailureKind:    row.FailureKind,
		FailureWindow:  row.FailureWindow,
		FailureResetAt: cloneTimePtr(row.FailureResetAt), FailureResetInferred: row.FailureResetInferred, FailureResetZone: row.FailureResetZone,
		BaseSHA: cloneStrPtr(row.BaseSHA), HeadSHA: cloneStrPtr(row.HeadSHA), Dirty: cloneBoolPtr(row.Dirty),
	}
	rs.FailureAction = failureActionOf(row.FailureKind, string(rs.Provider), row.Model, row.FailureWindow, row.FailureResetAt, nil, 0)
	return rs
}
