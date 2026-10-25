package runs

import (
	"time"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// disposition.go -- the derived "what should happen next" of a finished run
// (ADR-0027 section 4, decision D13 of docs/plans/active/orchestration-parity-
// paseo.md). It exists because a run that stopped at a budget cap ends with
// Status done: the agent process finished cleanly, so every consumer that only
// reads the status (a driver's task_wait, a queue's "finished" trigger, a
// glance at the task list) takes the work for complete. The cap is in StopReason,
// where only a reader who knows to look will find it.
//
// Disposition is computed from the status and the stop reason, never stored,
// and is not a status: Status keeps its five values, and nothing that waits
// for a terminal status changes behaviour. It is additive on every surface that
// reports StopReason, so a client that has not heard of it loses nothing.

// DispositionNeedsContinue marks a run that ended at a turn, token or budget
// cap: it is not finished, and a follow-up prompt ("continue") resumes it.
const DispositionNeedsContinue = "needs-continue"

// needsContinueStopReasons are the stop reasons that mean "cut off by a cap".
// budget_exceeded is the ADR-0027 budget stop; no code emits it yet, but a run
// that does must read as needing a continue (a budget raise, then "continue")
// without a change here.
var needsContinueStopReasons = map[string]bool{
	"max_turn_requests": true,
	"max_tokens":        true,
	"budget_exceeded":   true,
}

// StopReasonDaemonRestart is the stop reason of a run the daemon ended by going
// down (a graceful shutdown stops it; after a crash the startup reconciliation
// marks it interrupted), recorded only when its chat has a stored agent session
// to resume. Such a run is not finished and "continue" picks it back up.
const StopReasonDaemonRestart = "daemon_restart"

// StopReasonDaemonRestartFresh is the same interruption for a chat with no
// stored session (the run died before the agent reported one): it says why the
// run ended, but there is nothing to continue from.
const StopReasonDaemonRestartFresh = "daemon_restart_fresh"

// DispositionFor derives a run's disposition. It is empty for a run that is
// still going, failed, was stopped, or finished normally: a run that ended
// "done" at a cap needs a continue, and so does one the daemon's restart cut off
// (an error or a stop by the user already says it did not finish, and a cancel
// is the human's own choice).
func DispositionFor(status Status, stopReason string) string {
	switch {
	case status == StatusDone && needsContinueStopReasons[stopReason]:
		return DispositionNeedsContinue
	case (status == StatusStopped || status == StatusInterrupted) && stopReason == StopReasonDaemonRestart:
		return DispositionNeedsContinue
	}
	return ""
}

// DispositionFacts is what DispositionOf derives a disposition from: the run's
// terminal status and typed failure, and whether its chat has a stored agent
// session to resume.
type DispositionFacts struct {
	Status         Status
	StopReason     string
	FailureKind    string
	FailureResetAt *time.Time
	// HasSession: the run's chat has a stored agent session (and provider), the
	// thing a "continue" resumes. An error run without one has nothing to resume.
	HasSession bool
	Now        time.Time
}

// DispositionOf is DispositionFor plus the resumable failures (J1): an error run
// whose CLI died without a result (cli_exited) can be resumed from its stored
// session, and so can a rate-limited one once its quota has reset. Every other
// failure (permission denial or timeout, auth/config/provider errors, an
// unclassified error) stays without a disposition: resuming would repeat it.
// A rate limit with an unknown reset time stays with the existing retry and
// wait machinery (the daemon never guesses the time). Without a stored session
// no error run is resumable, same as daemon_restart_fresh.
func DispositionOf(f DispositionFacts) string {
	if d := DispositionFor(f.Status, f.StopReason); d != "" {
		return d
	}
	if f.Status != StatusError || !f.HasSession {
		return ""
	}
	switch f.FailureKind {
	case taskrunner.FailureKindCLIExited:
		return DispositionNeedsContinue
	case "rate_limited":
		if f.FailureResetAt != nil && !f.Now.Before(*f.FailureResetAt) {
			return DispositionNeedsContinue
		}
	}
	return ""
}
