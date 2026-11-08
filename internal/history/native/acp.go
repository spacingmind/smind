package native

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// ACP histories (ADR-0053 D4): the agent itself replays its session on
// session/load. Unlike the claude and codex readers this starts a process, so
// it only runs for a human who confirmed it (Request.ConfirmStart), under the
// same slot pool, with its own timeout, and the session is sliced to the run's
// turn by the prompt hash.

var (
	// ErrNoLoadSession is returned by Config.ACPReplay when the agent did not
	// advertise the loadSession capability (the protocol forbids calling it).
	ErrNoLoadSession = errors.New("native: the agent does not advertise loadSession")
	// ErrSessionUnknown is returned by Config.ACPReplay when the agent does not
	// know the session id any more.
	ErrSessionUnknown = errors.New("native: the agent does not know the session")
)

// ErrReplayTooLarge is returned by Config.ACPReplay when the replay passed the
// byte bound it was given.
var ErrReplayTooLarge = errTooLarge

// errNotIdle: BeforeStart said the chat is no longer idle.
var errNotIdle = errors.New("native: the chat started a run")

// SupportsACP reports whether ACP histories can be rebuilt at all.
func (rb *Rebuilder) SupportsACP() bool { return rb.cfg.ACPReplay != nil }

// ACPLoadUnsupported reports whether provider's agent was found not to advertise
// loadSession (learned from an earlier refusal, memory only): the button is no
// longer offered for it.
func (rb *Rebuilder) ACPLoadUnsupported(provider string) bool {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.acpNoLoad[provider]
}

func (rb *Rebuilder) rebuildACP(ctx context.Context, req Request) Result {
	ref := req.Ref
	if rb.cfg.ACPReplay == nil || ref.Provider == "" {
		return refuse(ReasonCannotRebuild, "")
	}
	if ref.SessionID == "" || ref.PromptSha256 == "" {
		return refuse(ReasonCannotRebuild, "no session or prompt hash recorded")
	}
	if !req.ConfirmStart {
		return refuse(ReasonConfirmRequired, "")
	}
	if rb.ACPLoadUnsupported(ref.Provider) {
		return refuse(ReasonCannotRebuild, "the agent does not support loading sessions")
	}
	if st, err := os.Stat(ref.Cwd); ref.Cwd == "" || !filepath.IsAbs(ref.Cwd) || err != nil || !st.IsDir() {
		return refuse(ReasonSessionMissing, "the task worktree is gone")
	}
	key := ref.Harness + "|" + ref.Provider
	if rb.breakerOpen(key) {
		return refuse(ReasonFormatDrift, "too many consecutive format failures for this agent; retrying later")
	}
	select {
	case rb.sem <- struct{}{}:
	default:
		return refuse(ReasonBusy, "")
	}
	ctx, cancel := context.WithTimeout(ctx, rb.cfg.ACPTimeout)
	defer cancel()
	type replayed struct {
		events []taskrunner.Event
		err    error
	}
	done := make(chan replayed, 1)
	go func() {
		// The slot stays held until the agent process is really gone.
		defer func() { <-rb.sem }()
		if req.BeforeStart != nil && !req.BeforeStart() {
			done <- replayed{err: errNotIdle}
			return
		}
		ev, err := rb.cfg.ACPReplay(ctx, ref, rb.cfg.MaxOutput)
		done <- replayed{ev, err}
	}()
	var r replayed
	select {
	case r = <-done:
	case <-ctx.Done():
		return refuse(ReasonFailed, "timed out")
	}
	switch {
	case errors.Is(r.err, errNotIdle):
		return refuse(ReasonSessionInUse, "")
	case errors.Is(r.err, ErrNoLoadSession):
		rb.mu.Lock()
		rb.acpNoLoad[ref.Provider] = true
		rb.mu.Unlock()
		return refuse(ReasonCannotRebuild, "the agent does not support loading sessions")
	case errors.Is(r.err, ErrSessionUnknown):
		return refuse(ReasonSessionMissing, "")
	case errors.Is(r.err, errTooLarge):
		return refuse(ReasonFailed, "the replay is over the size bound")
	case r.err != nil:
		return refuse(ReasonFailed, "the agent could not load the session")
	}
	turn, matches := SliceACPTurn(r.events, ref.PromptSha256)
	if matches != 1 {
		return refuse(ReasonSliceNotFound, fmt.Sprintf("%d matching turns", matches))
	}
	turn = coalesceACP(turn)
	if why := acpSelfCheck(turn, req.ExpectedToolCalls); why != "" {
		rb.recordDrift(key, true)
		return refuse(ReasonFormatDrift, why)
	}
	rb.recordDrift(key, false)
	events, raw, refs := rb.deliver(ref.Provider, turn)
	return Result{Events: events, raw: raw, Refs: refs, Detail: fmt.Sprintf("%d events from %d replayed", len(events), len(r.events))}
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// SliceACPTurn returns the assistant side (text, thinking, tool calls) of the
// turn whose user message is the run's prompt, and how many turns matched. A turn
// starts at a group of consecutive user_message events: it matches when the sha256
// of the whole user text, or of its last chunk (smind sends role and skill text
// as an earlier block of a first prompt), equals promptSha.
func SliceACPTurn(all []taskrunner.Event, promptSha string) ([]taskrunner.Event, int) {
	var (
		found   []taskrunner.Event
		matches int
		inTurn  bool
		hit     bool
		chunks  []string
		cur     []taskrunner.Event
	)
	closeTurn := func() {
		if inTurn && hit {
			matches++
			found = cur
		}
		inTurn, hit, chunks, cur = false, false, nil, nil
	}
	for _, e := range all {
		if e.Type == taskrunner.EventTypeUserMessage {
			if len(cur) > 0 { // assistant events since the last user group: a new turn starts
				closeTurn()
			}
			inTurn = true
			chunks = append(chunks, e.Text)
			hit = sha256Hex(strings.Join(chunks, "")) == promptSha || sha256Hex(chunks[len(chunks)-1]) == promptSha
			continue
		}
		if !inTurn {
			continue
		}
		switch e.Type {
		case taskrunner.EventTypeText, taskrunner.EventTypeThinking, taskrunner.EventTypeToolCall:
			e.Raw = nil
			cur = append(cur, e)
		}
	}
	closeTurn()
	return found, matches
}

// coalesceACP joins adjacent text (or thinking) events: an agent may replay a
// message as many small chunks.
func coalesceACP(in []taskrunner.Event) []taskrunner.Event {
	out := make([]taskrunner.Event, 0, len(in))
	for _, e := range in {
		if n := len(out); n > 0 && out[n-1].Type == e.Type && (e.Type == taskrunner.EventTypeText || e.Type == taskrunner.EventTypeThinking) {
			out[n-1].Text += e.Text
			continue
		}
		out = append(out, e)
	}
	return out
}

// acpSelfCheck is the drift test for an ACP turn: the call count is within 10%
// (at least one) of the stored skeleton's. "" = pass.
func acpSelfCheck(turn []taskrunner.Event, expectedCalls int) string {
	if expectedCalls < 0 {
		return ""
	}
	ids := map[string]struct{}{}
	for _, e := range turn {
		if e.Type == taskrunner.EventTypeToolCall && e.ToolCallID != "" {
			ids[e.ToolCallID] = struct{}{}
		}
	}
	allow := expectedCalls / 10
	if allow < 1 {
		allow = 1
	}
	diff := len(ids) - expectedCalls
	if diff < 0 {
		diff = -diff
	}
	if diff > allow {
		return fmt.Sprintf("%d tool calls replayed, %d in the stored run", len(ids), expectedCalls)
	}
	return ""
}
