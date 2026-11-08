package wsapi

import (
	"context"
	"fmt"
	"strings"

	"github.com/spacingmind/smind/internal/history/native"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// The history read contract (ADR-0053 section 5). Everything here is additive:
// a caller that sends no `detail` gets today's events, byte for byte, and the
// label fields below appear only on a run stored in skeleton form (or when a
// `detail` was asked for).
//
//	detail      who may ask           what it does
//	(omitted)   anyone                the stored rows
//	"skeleton"  anyone                the stored rows, never a rebuild
//	"auto"      a human, for the open  the stored rows, upgraded to the rebuilt history only when that is
//	                                  cheap (cached, or a session file of at most runs.AutoRebuildMaxBytes);
//	                                  anyone else gets the stored rows
//	"full"      a human only          rebuild the turn from the provider's session and merge it with the
//	                                  stored non-transcript rows; any failure serves the stored rows
//	                                  with a skeletonReason, never an error
//
// An agent, an orchestrator (task_logs sends no detail) or an automation never
// triggers a rebuild (H7): a run-scoped caller asking for "full" is answered
// with the stored rows and skeletonReason "rebuild_not_requested".

// Values of the `detail` parameter.
const (
	detailAuto     = "auto"
	detailSkeleton = "skeleton"
	detailFull     = "full"
)

// historyFields is embedded in the results that carry a label; all omitempty.
type historyFields struct {
	// HistorySource is "full" (every stored row is the original), "skeleton"
	// (the run was collapsed to its skeleton) or "rebuilt" (read from the
	// provider's own session just now).
	HistorySource string `json:"historySource,omitempty"`
	// SkeletonReason says why a skeleton is shown instead of the full history
	// ("rebuild_not_requested", "provider_cannot_rebuild", "native_session_missing",
	// "slice_not_found", "format_drift", "rebuild_failed", ...).
	SkeletonReason string `json:"skeletonReason,omitempty"`
	// CanLoadFull is true when a human may ask for the rebuilt history.
	CanLoadFull bool `json:"canLoadFull,omitempty"`
}

// validDetail reports whether d is an accepted value ("" = omitted).
func validDetail(d string) bool {
	return d == "" || d == detailAuto || d == detailSkeleton || d == detailFull
}

// isHuman reports whether the caller is a person's surface (web, desktop,
// mobile, CLI) and not an agent run: the only callers that may rebuild.
func isHuman(rc *requestContext) bool { return rc != nil && !rc.runScoped }

// historyLabel is the label of runID's stored history for a caller. detail is
// the request's value ("" = omitted).
func historyLabel(reg *runs.Registry, runID, detail string, human bool) historyFields {
	info := reg.HistoryInfo(runID)
	switch info.Source {
	case "skeleton":
		return historyFields{HistorySource: "skeleton", SkeletonReason: info.Reason, CanLoadFull: info.CanLoadFull && human}
	default:
		if detail == "" {
			return historyFields{} // today's bytes: no label on a run that is not in skeleton form
		}
		return historyFields{HistorySource: "full"}
	}
}

// rebuiltEvents is the merged history of runID for a human: the stored rows with
// the transcript replaced by the provider's own, or the reason it could not be.
// Events are in client form (already elided). auto is the open-time upgrade: it
// rebuilds only when that is cheap, and reports skipped (no events, no reason,
// nothing read) when it is not.
func rebuiltEvents(ctx context.Context, reg *runs.Registry, runID string, auto bool) (events []taskrunner.Event, refs map[string][]taskrunner.HeredocRef, reason native.Reason, skipped bool) {
	var got runs.Rebuilt
	if auto {
		got = reg.RebuildOnOpen(ctx, runID, runs.RequesterHuman)
	} else {
		got = reg.RebuildHistory(ctx, runID, runs.RequesterHuman)
	}
	if got.Skipped {
		return nil, nil, native.ReasonNone, true
	}
	if !got.OK() {
		return nil, nil, got.Reason, false
	}
	stored, _, err := reg.History(runID)
	if err != nil {
		return nil, nil, native.ReasonFailed, false
	}
	return runs.MergeRebuilt(stored, got.Events), got.Refs, native.ReasonNone, false
}

// quietOpenRefusal reports a reason an automatic open should not announce: the
// concurrency bound being full or the chat being busy says nothing about the
// session, and the button is still the way to ask later.
func quietOpenRefusal(reason native.Reason) bool {
	return reason == native.ReasonBusy || reason == native.ReasonSessionInUse
}

// logEventsClientForm converts events that are already in client form (a
// rebuilt history) to their wire shape; unlike runLogEventsPrimed it never
// elides again.
func logEventsClientForm(history []taskrunner.Event, refs map[string][]taskrunner.HeredocRef, tail int) []runLogEvent {
	el := taskrunner.NewClientFormElider(refs)
	events := make([]runLogEvent, len(history))
	for i, e := range history {
		events[i] = toRunLogEvent(el, e)
	}
	if tail > 0 && len(events) > tail {
		events = events[len(events)-tail:]
	}
	return events
}

func detailError(method, d string) error {
	return &rpcCodeError{code: codeInvalid, msg: fmt.Sprintf("%s: detail must be one of auto, skeleton, full, not %q", method, strings.TrimSpace(d))}
}
