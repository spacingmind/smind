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

	// StatusInterrupted is a Run whose persisted row was still "running"
	// when the daemon started up -- meaning the process driving it is
	// definitely gone (nothing ties a run.start-originated subprocess's
	// lifetime to the daemon's; see Registry.CloseAll's doc comment), but
	// unlike StatusStopped (a deliberate, successful cancellation) or
	// StatusError (a reported backend failure), its actual fate is unknown.
	// Only ever assigned by Registry's startup reconciliation, never by a
	// live run.
	StatusInterrupted Status = "interrupted"
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

	// ApprovalPolicy is this run's current taskrunner.ApprovalPolicy --
	// live (may differ from what Start was called with, see
	// Registry.SetApprovalPolicy) for a running run, and whatever it was
	// last switched to (or started with) for a finished one. Lets a caller
	// (internal/wsapi, and from there the web UI) know whether a live
	// manual<->auto-safe switch control applies to this run at all.
	ApprovalPolicy taskrunner.ApprovalPolicy

	// ThinkingLevel is this run's taskrunner.ThinkingLevel as passed to
	// Start -- immutable thereafter (see the run struct's own field doc
	// comment), Claude-only, and not persisted across a daemon restart (a
	// rehydrated run always reports taskrunner.ThinkingLevelUnspecified
	// here). Lets a caller determine whether a failed Claude-native run has
	// a higher tier left to retry at (docs/plans/active/mid-run-approval-
	// and-retry-effort.md's Item B).
	ThinkingLevel taskrunner.ThinkingLevel
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
