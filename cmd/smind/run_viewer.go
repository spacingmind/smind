package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// viewOptions are the knobs `task send`, `task attach` and `task logs` share
// for how a run's events are rendered.
type viewOptions struct {
	// verbose also prints "raw" events (ACP session updates smind has no typed
	// shape for, e.g. available_commands_update, which can be several KB of
	// JSON each). Off by default: they are protocol noise, not run output.
	verbose bool
	// autoAccept says the run auto-approves its permission prompts, so each
	// one is shown as a single line once resolved instead of the full
	// approve/deny block.
	autoAccept bool
}

// autoAcceptHold is how long a live viewer holds back a permission request it
// expects to be auto-accepted. Auto-accept resolves within milliseconds; if
// nothing resolves it within the hold (auto-accept was switched off
// mid-run, say), the full block is printed so a prompt that needs a human is
// never hidden.
const autoAcceptHold = 2 * time.Second

// permissionReasonAutoAccept is the permission_resolved reason the daemon
// records for a prompt it approved by itself (runs.PermissionResolution).
const permissionReasonAutoAccept = "auto_accept"

// runViewer renders one run's events as text. The same renderer backs the live
// stream (send, attach, logs -f) and the one-shot logs listing, so the two
// cannot drift.
type runViewer struct {
	w     io.Writer
	runID string
	opts  viewOptions
	hold  time.Duration

	mu    sync.Mutex // guards names, held and writes (the hold timer prints off-goroutine)
	names toolCallNames
	held  map[string]*heldPermission
}

// heldPermission is a permission request not yet printed, waiting to see
// whether it resolves as an auto-accept.
type heldPermission struct {
	summary string
	options []permissionOptionParams
	detail  *permissionDetail
	timer   *time.Timer
}

func newRunViewer(runID string, opts viewOptions) *runViewer {
	return &runViewer{w: os.Stdout, runID: runID, opts: opts, hold: autoAcceptHold, names: toolCallNames{}, held: map[string]*heldPermission{}}
}

func (v *runViewer) print(s string) { _, _ = io.WriteString(v.w, s) }

// text prints a chunk, user_message or thinking fragment as it arrived.
func (v *runViewer) text(s string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.print(s)
}

func (v *runViewer) toolCall(p toolCallEventParams) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.print(v.names.render(p))
}

// raw prints a raw event only in verbose mode.
func (v *runViewer) raw(p rawEventParams) {
	if !v.opts.verbose {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.print(renderRaw(p))
}

// permissionRequest prints a request's block, or, for an auto-accept run,
// holds it for permissionResolved to collapse into one line.
func (v *runViewer) permissionRequest(requestID, summary string, options []permissionOptionParams, detail *permissionDetail) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.opts.autoAccept {
		v.print(renderPermissionRequest(v.runID, requestID, summary, options, detail))
		return
	}
	h := &heldPermission{summary: summary, options: options, detail: detail}
	h.timer = time.AfterFunc(v.hold, func() { v.release(requestID) })
	v.held[requestID] = h
}

// release prints a held request's full block (the hold expired or the stream
// ended without a resolution).
func (v *runViewer) release(requestID string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	h, ok := v.held[requestID]
	if !ok {
		return
	}
	delete(v.held, requestID)
	h.timer.Stop()
	v.print(renderPermissionRequest(v.runID, requestID, h.summary, h.options, h.detail))
}

// permissionResolved prints how a request was resolved. A held request
// resolved as auto_accept becomes the single line renderAutoAccepted; any other
// outcome prints the held block first, then the usual resolution line.
func (v *runViewer) permissionResolved(requestID, optionID, reason string) {
	v.mu.Lock()
	h, held := v.held[requestID]
	if held {
		delete(v.held, requestID)
		h.timer.Stop()
	}
	defer v.mu.Unlock()
	switch {
	case held && reason == permissionReasonAutoAccept:
		v.print(renderAutoAccepted(h.summary, h.detail, optionID))
	case held:
		v.print(renderPermissionRequest(v.runID, requestID, h.summary, h.options, h.detail))
		v.print(renderPermissionResolved(requestID, optionID, reason))
	default:
		v.print(renderPermissionResolved(requestID, optionID, reason))
	}
}

// autoAcceptedLine prints the one-line form directly (used by the one-shot
// logs listing, which already knows the outcome).
func (v *runViewer) autoAcceptedLine(summary string, detail *permissionDetail, optionID string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.print(renderAutoAccepted(summary, detail, optionID))
}

// flush prints any request still held, in no particular order: the stream
// ended before it resolved.
func (v *runViewer) flush() {
	v.mu.Lock()
	ids := make([]string, 0, len(v.held))
	for id := range v.held {
		ids = append(ids, id)
	}
	v.mu.Unlock()
	for _, id := range ids {
		v.release(id)
	}
}

// autoAcceptedMaxLen caps the one-line form: an auto-accepted command can be
// a whole script.
const autoAcceptedMaxLen = 160

// renderAutoAccepted is the single line for a prompt the daemon approved by
// itself: what was approved and which option answered.
func renderAutoAccepted(summary string, detail *permissionDetail, optionID string) string {
	what := summary
	if detail != nil && detail.Command != "" && !strings.Contains(summary, detail.Command) {
		what += ": " + detail.Command
	}
	what = strings.Join(strings.Fields(what), " ")
	if r := []rune(what); len(r) > autoAcceptedMaxLen {
		what = string(r[:autoAcceptedMaxLen-1]) + "…"
	}
	return fmt.Sprintf("[permission] auto-accepted %s -> %s\n", what, optionID)
}
