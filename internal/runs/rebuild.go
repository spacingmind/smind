package runs

import (
	"context"
	"encoding/json"

	"github.com/spacingmind/smind/internal/history/native"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// Rebuilding a finished run's full history from the provider's own session
// (ADR-0053 D4). This is the Go API; the read contract (historySource, detail)
// that calls it from run.logs and the UI comes in the next change.

// Requester says who asked: only a human request rebuilds (H7).
type Requester = native.Requester

const (
	RequesterAgent = native.RequesterAgent
	RequesterHuman = native.RequesterHuman
)

// SetRebuilder wires the session reader. Nil (the default) means no history can
// be rebuilt: every request answers provider_cannot_rebuild.
func (reg *Registry) SetRebuilder(rb *native.Rebuilder) {
	reg.mu.Lock()
	reg.rebuilder = rb
	reg.mu.Unlock()
}

// Rebuilt is the outcome of RebuildHistory: either the turn's events from the
// provider's session, or the reason the stored skeleton must be shown instead.
type Rebuilt struct {
	Events []Event
	// Refs are the heredoc references of the rebuilt tool calls' inputs (by tool
	// call id), what a live read carries next to the elided input.
	Refs   map[string][]taskrunner.HeredocRef
	Reason native.Reason
	Detail string // counts only, never text
}

// OK reports whether the history was rebuilt.
func (r Rebuilt) OK() bool { return r.Reason == native.ReasonNone }

func degraded(reason native.Reason, detail string) Rebuilt {
	return Rebuilt{Reason: reason, Detail: detail}
}

// RebuildHistory reads runID's turn from the provider session named by its
// native_ref. It only rebuilds for a human requester, only for a finished run
// whose chat has no running run (the session must not be read while it is being
// written), and never fails: a refusal is a Reason. The events are in client
// form: heredoc bodies elided, provider secrets scrubbed, exactly as a live read.
func (reg *Registry) RebuildHistory(ctx context.Context, runID string, who Requester) Rebuilt {
	if who != RequesterHuman {
		return degraded(native.ReasonHumanOnly, "")
	}
	reg.mu.Lock()
	rb := reg.rebuilder
	reg.mu.Unlock()
	if rb == nil {
		return degraded(native.ReasonCannotRebuild, "")
	}
	row, err := reg.storedReadable(runID)
	if err != nil {
		return degraded(native.ReasonCannotRebuild, "")
	}
	if row.ChatID != 0 && reg.chatHasRunningRun(row.ChatID) {
		return degraded(native.ReasonSessionInUse, "")
	}
	sk, err := reg.st.GetRunSkeleton(runID)
	if err != nil || sk.NativeRef == "" {
		return degraded(native.ReasonCannotRebuild, "no native_ref recorded")
	}
	var ref native.Ref
	if json.Unmarshal([]byte(sk.NativeRef), &ref) != nil {
		return degraded(native.ReasonCannotRebuild, "native_ref unreadable")
	}
	res := rb.Rebuild(ctx, native.Request{Ref: ref, Requester: who, ExpectedToolCalls: reg.storedToolCalls(runID)})
	if !res.OK() {
		return degraded(res.Reason, res.Detail)
	}
	return Rebuilt{Events: res.Events, Refs: res.Refs, Detail: res.Detail}
}

// chatHasRunningRun reports whether any run of chatID is running.
func (reg *Registry) chatHasRunningRun(chatID int64) bool {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	for _, r := range reg.runs {
		r.mu.Lock()
		busy := r.chatID == chatID && r.status == StatusRunning
		r.mu.Unlock()
		if busy {
			return true
		}
	}
	return false
}

// storedToolCalls is the number of distinct tool calls in the stored rows of
// runID (the skeleton keeps one row per call), or -1 when it cannot be read: the
// drift self-check compares it with the session's count.
func (reg *Registry) storedToolCalls(runID string) int {
	rows, err := reg.st.ListRunEventsRaw(runID)
	if err != nil {
		return -1
	}
	ids := map[string]struct{}{}
	for _, row := range rows {
		var p struct {
			Type       taskrunner.EventType `json:"type"`
			ToolCallID string               `json:"toolCallId"`
		}
		if json.Unmarshal([]byte(row.EventData), &p) == nil && p.Type == taskrunner.EventTypeToolCall && p.ToolCallID != "" {
			ids[p.ToolCallID] = struct{}{}
		}
	}
	return len(ids)
}

// RebuildHeredoc returns heredoc body index of tool call callID as the provider
// session recorded it, for a finished run, and only when its sha256 equals the
// descriptor the skeleton stored for that body (ADR-0053 AC10). Anything else is
// ErrHeredocUnavailable, never "no such heredoc": the call may well have had one.
func (reg *Registry) RebuildHeredoc(ctx context.Context, runID, callID string, index int, who Requester) (HeredocReveal, error) {
	want, ok := reg.storedHeredocSha(runID, callID, index)
	if !ok {
		return HeredocReveal{}, ErrHeredocUnavailable
	}
	if who != RequesterHuman {
		return HeredocReveal{}, ErrHeredocUnavailable
	}
	reg.mu.Lock()
	rb := reg.rebuilder
	reg.mu.Unlock()
	if rb == nil {
		return HeredocReveal{}, ErrHeredocUnavailable
	}
	row, err := reg.storedReadable(runID)
	if err != nil {
		return HeredocReveal{}, err
	}
	sk, err := reg.st.GetRunSkeleton(runID)
	if err != nil || sk.NativeRef == "" {
		return HeredocReveal{}, ErrHeredocUnavailable
	}
	var ref native.Ref
	if json.Unmarshal([]byte(sk.NativeRef), &ref) != nil || (row.ChatID != 0 && reg.chatHasRunningRun(row.ChatID)) {
		return HeredocReveal{}, ErrHeredocUnavailable
	}
	res := rb.Rebuild(ctx, native.Request{Ref: ref, Requester: who, ExpectedToolCalls: reg.storedToolCalls(runID)})
	if !res.OK() {
		return HeredocReveal{}, ErrHeredocUnavailable
	}
	body, ok := res.HeredocBody(callID, index)
	if !ok || sha256Hex(body) != want {
		return HeredocReveal{}, ErrHeredocUnavailable
	}
	return HeredocReveal{RunID: runID, TaskID: row.TaskID, Text: body}, nil
}

// storedHeredocSha reads the skeleton descriptor of body index of callID.
func (reg *Registry) storedHeredocSha(runID, callID string, index int) (string, bool) {
	rows, err := reg.st.ListRunEventsRaw(runID)
	if err != nil {
		return "", false
	}
	for _, row := range rows {
		var p persistedEvent
		if json.Unmarshal([]byte(row.EventData), &p) != nil || p.ToolCallID != callID || p.Skel == nil || p.Skel.In == nil {
			continue
		}
		for _, d := range p.Skel.In.Doc {
			if d.I == index {
				return d.SHA, true
			}
		}
	}
	return "", false
}
