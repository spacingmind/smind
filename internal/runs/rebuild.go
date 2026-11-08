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
	// Skipped is set by RebuildOnOpen when it did not try (not cached, too big):
	// not a failure, the caller keeps the skeleton and its "load full" button.
	Skipped bool
}

// OK reports whether the history was rebuilt.
func (r Rebuilt) OK() bool { return r.Reason == native.ReasonNone && !r.Skipped }

func degraded(reason native.Reason, detail string) Rebuilt {
	return Rebuilt{Reason: reason, Detail: detail}
}

// AutoRebuildMaxBytes is the largest session file a human's "auto" open will
// parse on its own (H5); anything bigger waits for the explicit button. The
// bound comes from the measurement in ADR-0053 ("Implemented: the budget cache"):
// at this size a cold rebuild stays far inside the 10 s limit.
const AutoRebuildMaxBytes = 8 << 20

// autoRebuildMax is AutoRebuildMaxBytes as a variable so a test can lower it.
var autoRebuildMax int64 = AutoRebuildMaxBytes

// rebuildTarget checks everything a rebuild of runID needs before the session is
// touched: a human requester, a wired reader, a finished readable run whose chat
// is idle, and a readable native_ref.
func (reg *Registry) rebuildTarget(runID string, who Requester) (*native.Rebuilder, native.Ref, Rebuilt, bool) {
	var ref native.Ref
	if who != RequesterHuman {
		return nil, ref, degraded(native.ReasonHumanOnly, ""), false
	}
	reg.mu.Lock()
	rb := reg.rebuilder
	reg.mu.Unlock()
	if rb == nil {
		return nil, ref, degraded(native.ReasonCannotRebuild, ""), false
	}
	row, err := reg.storedReadable(runID)
	if err != nil {
		return nil, ref, degraded(native.ReasonCannotRebuild, ""), false
	}
	if row.ChatID != 0 && reg.chatHasRunningRun(row.ChatID) {
		return nil, ref, degraded(native.ReasonSessionInUse, ""), false
	}
	sk, err := reg.st.GetRunSkeleton(runID)
	if err != nil || sk.NativeRef == "" {
		return nil, ref, degraded(native.ReasonCannotRebuild, "no native_ref recorded"), false
	}
	if json.Unmarshal([]byte(sk.NativeRef), &ref) != nil {
		return nil, ref, degraded(native.ReasonCannotRebuild, "native_ref unreadable"), false
	}
	return rb, ref, Rebuilt{}, true
}

// RebuildHistory reads runID's turn from the provider session named by its
// native_ref. It only rebuilds for a human requester, only for a finished run
// whose chat has no running run (the session must not be read while it is being
// written), and never fails: a refusal is a Reason. The events are in client
// form: heredoc bodies elided, provider secrets scrubbed, exactly as a live read.
// A rebuild is served from the cache while the session file is unchanged.
func (reg *Registry) RebuildHistory(ctx context.Context, runID string, who Requester) Rebuilt {
	return reg.RebuildHistoryConfirmed(ctx, runID, who, false)
}

// RebuildHistoryConfirmed is RebuildHistory for a caller that may have the human's
// explicit yes to starting an agent: an ACP run's history is the agent's own replay
// of its session, so it is only loaded with confirmStart (else ReasonConfirmRequired,
// nothing started). The claude and codex readers ignore it.
func (reg *Registry) RebuildHistoryConfirmed(ctx context.Context, runID string, who Requester, confirmStart bool) Rebuilt {
	rb, ref, refused, ok := reg.rebuildTarget(runID, who)
	if !ok {
		return refused
	}
	if ref.Harness == native.HarnessACP {
		// No file to key a cache by: every confirmed load starts the agent once.
		res := rb.Rebuild(ctx, native.Request{Ref: ref, Requester: RequesterHuman, ExpectedToolCalls: reg.storedToolCalls(runID), ConfirmStart: confirmStart})
		if !res.OK() {
			return degraded(res.Reason, res.Detail)
		}
		return Rebuilt{Events: res.Events, Refs: res.Refs, Detail: res.Detail}
	}
	return reg.rebuildCached(ctx, rb, ref, runID, rb.Probe(ref))
}

// RebuildOnOpen is the automatic upgrade of a human's open (ADR-0053 H5): it
// rebuilds only when that is cheap, that is when the rebuild is already cached or
// the session file is at most AutoRebuildMaxBytes; otherwise Skipped is true and
// nothing was read. An agent, an orchestrator or an automation never gets here
// (H7): the requester check is the first thing rebuildTarget does.
func (reg *Registry) RebuildOnOpen(ctx context.Context, runID string, who Requester) Rebuilt {
	rb, ref, refused, ok := reg.rebuildTarget(runID, who)
	if !ok {
		return refused
	}
	if ref.Harness == native.HarnessACP {
		return Rebuilt{Skipped: true} // an automatic open never starts an agent
	}
	probe := rb.Probe(ref)
	if !probe.Found {
		return Rebuilt{Skipped: true}
	}
	if got, hit := reg.rcache.get(runID, probe.Size, probe.ModTime); hit {
		return got
	}
	if probe.Size > autoRebuildMax {
		return Rebuilt{Skipped: true}
	}
	return reg.rebuildCached(ctx, rb, ref, runID, probe)
}

// rebuildCached serves runID's rebuild from the cache when probe's file identity
// matches, else rebuilds and caches the result. A hit reads nothing.
func (reg *Registry) rebuildCached(ctx context.Context, rb *native.Rebuilder, ref native.Ref, runID string, probe native.Probe) Rebuilt {
	if probe.Found {
		if got, hit := reg.rcache.get(runID, probe.Size, probe.ModTime); hit {
			return got
		}
	}
	gen := reg.rcache.generation()
	res := rb.Rebuild(ctx, native.Request{Ref: ref, Requester: RequesterHuman, ExpectedToolCalls: reg.storedToolCalls(runID)})
	if !res.OK() {
		return degraded(res.Reason, res.Detail)
	}
	out := Rebuilt{Events: res.Events, Refs: res.Refs, Detail: res.Detail}
	// Client form only: res.raw (the unelided heredoc bodies) never enters the cache.
	reg.rcache.put(runID, gen, res.FileSize, res.FileModTime, out)
	return out
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
