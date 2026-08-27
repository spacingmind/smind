// Package runs tracks a task.prompt turn as a Run: a server-side object
// with a lifetime independent of whichever WebSocket connection started it.
// A Registry owns each Run's background goroutine (driving
// taskrunner.Runner.RunPrompt to completion regardless of whether anyone
// is still watching) plus its accumulated event history, so a later
// connection can list runs, attach to a still-running one (backfilled with
// everything emitted so far, then the live tail), fetch its full history
// without live-following, or stop it -- all independent of which
// connection, if any, originally started it.
package runs

import (
	"errors"
	"time"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// Event is the unit of a Run's history and live stream. It's exactly
// taskrunner.Event: Registry doesn't need to add anything to it, since
// Run-level bookkeeping (status, timestamps, stop reason) lives on
// RunStatus instead.
type Event = taskrunner.Event

// Status is a Run's lifecycle state.
type Status string

const (
	// StatusRunning is a Run's state from Start until its RunPrompt call
	// returns.
	StatusRunning Status = "running"

	// StatusDone is a Run that completed its turn normally.
	StatusDone Status = "done"

	// StatusError is a Run whose RunPrompt call returned an error not
	// caused by Stop (a backend failure, a bad task, etc.).
	StatusError Status = "error"

	// StatusStopped is a Run that ended because Stop was called on it,
	// distinguished from StatusError so callers can tell a deliberate stop
	// from an actual backend failure.
	StatusStopped Status = "stopped"
)

// RunStatus is a point-in-time snapshot of a Run's identity and lifecycle
// state, returned by History and (as RunSummary) by List.
type RunStatus struct {
	ID         string
	TaskID     int64
	Provider   taskrunner.Provider
	Prompt     string
	Status     Status
	StartedAt  time.Time
	FinishedAt *time.Time

	// StopReason is populated once Status is StatusDone, carrying the
	// taskrunner.EventTypeDone event's StopReason.
	StopReason string

	// Err is populated once Status is StatusError, carrying the error
	// RunPrompt returned.
	Err string
}

// RunSummary is the shape List returns; it's the same information as
// RunStatus, named separately because a run list and a single run's status
// lookup are different call sites even though they carry identical data
// today.
type RunSummary = RunStatus

// ErrNotFound is returned by Subscribe, History, and Stop when no Run with
// the given ID is known to the Registry -- either it never existed, or it
// finished long enough ago to have been evicted (see Registry's doc
// comment on retention).
var ErrNotFound = errors.New("runs: run not found")
