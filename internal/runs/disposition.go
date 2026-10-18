package runs

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

// DispositionFor derives a run's disposition. It is empty for a run that is
// still going, failed, was stopped, or finished normally: only a run that ended
// "done" at a cap needs a continue (an error or a stop already says it did not
// finish, and a cancel is the human's own choice).
func DispositionFor(status Status, stopReason string) string {
	if status == StatusDone && needsContinueStopReasons[stopReason] {
		return DispositionNeedsContinue
	}
	return ""
}
