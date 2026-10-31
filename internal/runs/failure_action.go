package runs

import (
	"time"

	"github.com/spacingmind/smind/internal/runs/failure"
)

// failureActionOf builds a failed run's next action from its stored facts. The
// fallback profile and the allowed models are not known here (wire layers that
// know them rebuild the action with failure.NextAction); a queued transient
// retry is passed in as (attempt, nextAt).
func failureActionOf(kind, provider, model, window string, resetAt, nextAt *time.Time, attempt int) *failure.Action {
	return failure.NextAction(kind, failure.ActionContext{
		Provider: provider, Model: model, Window: window, ResetAt: cloneTimePtr(resetAt),
		Attempt: attempt, Max: len(transientRetryBackoffs), NextAt: cloneTimePtr(nextAt), Now: time.Now(),
	})
}

// failureAction is the run's own next action (r.mu held by the caller).
func (r *run) failureAction() *failure.Action {
	if r.status != StatusError {
		return nil
	}
	return failureActionOf(r.failureKind, string(r.provider), r.model, r.failureWindow, r.failureResetAt, r.retryNextAt, r.retryAttempt)
}
