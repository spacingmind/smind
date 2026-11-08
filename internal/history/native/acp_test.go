package native

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// ACP histories: the replay of session/load sliced to the run's turn.

func userMsg(t string) taskrunner.Event {
	return taskrunner.Event{Type: taskrunner.EventTypeUserMessage, Text: t}
}
func agentText(t string) taskrunner.Event {
	return taskrunner.Event{Type: taskrunner.EventTypeText, Text: t}
}
func thought(t string) taskrunner.Event {
	return taskrunner.Event{Type: taskrunner.EventTypeThinking, Text: t}
}
func toolCall(id, cmd string) taskrunner.Event {
	in, _ := json.Marshal(map[string]any{"command": cmd})
	return taskrunner.Event{Type: taskrunner.EventTypeToolCall, ToolCallID: id, ToolName: "execute", ToolTitle: "run", ToolStatus: "success", ToolInput: in}
}

// two turns: "first" (with a tool call) and "second".
func twoTurnReplay() []taskrunner.Event {
	return []taskrunner.Event{
		userMsg("first"), thought("hmm "), thought("ok"), agentText("a1 "), agentText("a2"), toolCall("t1", "ls"),
		userMsg("second"), agentText("b1"),
	}
}

func TestSliceACPTurn(t *testing.T) {
	all := twoTurnReplay()
	turn, n := SliceACPTurn(all, sha("first"))
	if n != 1 || len(turn) != 5 || turn[0].Text != "hmm " || turn[4].ToolCallID != "t1" {
		t.Fatalf("first: %d matches, %d events", n, len(turn))
	}
	turn, n = SliceACPTurn(all, sha("second"))
	if n != 1 || len(turn) != 1 || turn[0].Text != "b1" {
		t.Fatalf("second: %d matches, %+v", n, turn)
	}
	if _, n = SliceACPTurn(all, sha("third")); n != 0 {
		t.Fatalf("no such turn: %d matches", n)
	}
	// the same prompt twice is ambiguous and never guessed
	dup := append(append([]taskrunner.Event(nil), all...), userMsg("first"), agentText("c"))
	if _, n = SliceACPTurn(dup, sha("first")); n != 2 {
		t.Fatalf("duplicate prompt: %d matches, want 2", n)
	}
	// a user text split into chunks matches as a whole, and a role preface sent as an
	// earlier chunk matches by the last chunk (the user's own block)
	split := []taskrunner.Event{userMsg("fi"), userMsg("rst"), agentText("x")}
	if _, n = SliceACPTurn(split, sha("first")); n != 1 {
		t.Fatalf("split chunks: %d", n)
	}
	pref := []taskrunner.Event{userMsg("ROLE TEXT"), userMsg("first"), agentText("x")}
	if _, n = SliceACPTurn(pref, sha("first")); n != 1 {
		t.Fatalf("prefaced prompt: %d", n)
	}
	// replayed non-transcript events (raw, usage) are not part of the turn
	withRaw := []taskrunner.Event{userMsg("first"), {Type: taskrunner.EventTypeRaw, RawKind: "plan"}, agentText("x")}
	if turn, _ = SliceACPTurn(withRaw, sha("first")); len(turn) != 1 {
		t.Fatalf("raw leaked into the turn: %+v", turn)
	}
}

type acpEnv struct {
	rb     *Rebuilder
	calls  atomic.Int32
	events []taskrunner.Event
	err    error
	hold   chan struct{}
}

func newACPEnv(t *testing.T, mut func(*Config)) *acpEnv {
	t.Helper()
	e := &acpEnv{events: twoTurnReplay()}
	cfg := Config{
		ACPReplay: func(ctx context.Context, ref Ref, max int64) ([]taskrunner.Event, error) {
			e.calls.Add(1)
			if e.hold != nil {
				select {
				case <-e.hold:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			return e.events, e.err
		},
		Scrub: func(_, s string) string { return strings.ReplaceAll(s, "SECRET", "[redacted]") },
	}
	if mut != nil {
		mut(&cfg)
	}
	e.rb = New(cfg)
	return e
}

func (e *acpEnv) req(t *testing.T, prompt string, confirm bool) Request {
	return Request{Ref: Ref{Provider: "glm", Harness: HarnessACP, SessionID: "s1", Cwd: t.TempDir(), PromptSha256: sha(prompt)}, Requester: RequesterHuman, ExpectedToolCalls: -1, ConfirmStart: confirm}
}

// Nothing starts without the human's confirmation or for a non-human.
func TestRebuildACPNeedsAHumanAndAConfirmation(t *testing.T) {
	e := newACPEnv(t, nil)
	ctx := context.Background()
	if r := e.rb.Rebuild(ctx, e.req(t, "first", false)); r.Reason != ReasonConfirmRequired || e.calls.Load() != 0 {
		t.Fatalf("no confirm: %q, %d starts", r.Reason, e.calls.Load())
	}
	req := e.req(t, "first", true)
	req.Requester = RequesterAgent
	if r := e.rb.Rebuild(ctx, req); r.Reason != ReasonHumanOnly || e.calls.Load() != 0 {
		t.Fatalf("agent: %q, %d starts", r.Reason, e.calls.Load())
	}
	none := New(Config{})
	if r := none.Rebuild(ctx, e.req(t, "first", true)); r.Reason != ReasonCannotRebuild {
		t.Fatalf("no replayer wired: %q", r.Reason)
	}
}

func TestRebuildACPRebuildsTheTurn(t *testing.T) {
	e := newACPEnv(t, nil)
	e.events[5] = toolCall("t1", "cat > f <<'EOF'\nSECRET body\nEOF")
	e.events[3] = agentText("a1 SECRET")
	res := e.rb.Rebuild(context.Background(), e.req(t, "first", true))
	if !res.OK() {
		t.Fatalf("refused: %q (%s)", res.Reason, res.Detail)
	}
	if len(res.Events) != 3 { // thinking coalesced, text coalesced, tool call
		t.Fatalf("%d events: %+v", len(res.Events), res.Events)
	}
	b, _ := json.Marshal(res.Events)
	if strings.Contains(string(b), "SECRET") {
		t.Fatalf("a secret or heredoc body reached a rebuilt event: %s", b)
	}
	if body, ok := res.HeredocBody("t1", 0); !ok || !strings.Contains(body, "[redacted] body") {
		t.Fatalf("reveal body = %q, %v", body, ok)
	}
}

func TestRebuildACPRefusals(t *testing.T) {
	ctx := context.Background()
	e := newACPEnv(t, nil)
	e.err = ErrNoLoadSession
	if r := e.rb.Rebuild(ctx, e.req(t, "first", true)); r.Reason != ReasonCannotRebuild || !e.rb.ACPLoadUnsupported("glm") {
		t.Fatalf("no loadSession: %q", r.Reason)
	}
	before := e.calls.Load()
	if r := e.rb.Rebuild(ctx, e.req(t, "first", true)); r.Reason != ReasonCannotRebuild || e.calls.Load() != before {
		t.Fatalf("a provider known not to load must not be started again: %q, %d", r.Reason, e.calls.Load()-before)
	}
	e = newACPEnv(t, nil)
	e.err = ErrSessionUnknown
	if r := e.rb.Rebuild(ctx, e.req(t, "first", true)); r.Reason != ReasonSessionMissing {
		t.Fatalf("unknown session: %q", r.Reason)
	}
	e = newACPEnv(t, nil)
	e.err = fmt.Errorf("agent exploded: %w", errors.New("boom"))
	if r := e.rb.Rebuild(ctx, e.req(t, "first", true)); r.Reason != ReasonFailed || strings.Contains(r.Detail, "boom") {
		t.Fatalf("agent error: %q %q", r.Reason, r.Detail)
	}
	e = newACPEnv(t, nil)
	if r := e.rb.Rebuild(ctx, e.req(t, "nope", true)); r.Reason != ReasonSliceNotFound {
		t.Fatalf("no matching turn: %q", r.Reason)
	}
	e.events = append(e.events, userMsg("first"), agentText("again"))
	if r := e.rb.Rebuild(ctx, e.req(t, "first", true)); r.Reason != ReasonSliceNotFound {
		t.Fatalf("the same prompt twice must never be guessed: %q", r.Reason)
	}
	e.events = twoTurnReplay()
	req := e.req(t, "first", true)
	req.Ref.Cwd = req.Ref.Cwd + "/gone"
	if r := e.rb.Rebuild(ctx, req); r.Reason != ReasonSessionMissing {
		t.Fatalf("worktree gone: %q", r.Reason)
	}
	req = e.req(t, "first", true)
	req.ExpectedToolCalls = 10
	if r := e.rb.Rebuild(ctx, req); r.Reason != ReasonFormatDrift {
		t.Fatalf("tool count drift: %q", r.Reason)
	}
}

// Bounds: the slot pool, the timeout, and the slot staying held until the agent
// is gone.
func TestRebuildACPBounds(t *testing.T) {
	ctx := context.Background()
	e := newACPEnv(t, func(c *Config) { c.MaxConcurrent = 1; c.ACPTimeout = 200 * time.Millisecond })
	e.hold = make(chan struct{})
	started := make(chan struct{})
	e.rb.cfg.ACPReplay = func(ctx context.Context, ref Ref, max int64) ([]taskrunner.Event, error) {
		close(started)
		<-e.hold
		return nil, nil
	}
	done := make(chan Result, 1)
	go func() { done <- e.rb.Rebuild(ctx, e.req(t, "first", true)) }()
	<-started
	if r := e.rb.Rebuild(ctx, e.req(t, "first", true)); r.Reason != ReasonBusy {
		t.Fatalf("second load while the slot is held: %q", r.Reason)
	}
	if r := <-done; r.Reason != ReasonFailed || r.Detail != "timed out" {
		t.Fatalf("timeout: %q %q", r.Reason, r.Detail)
	}
	// the replayer has not returned yet: the slot is still held
	if r := e.rb.Rebuild(ctx, e.req(t, "first", true)); r.Reason != ReasonBusy {
		t.Fatalf("slot released before the agent returned: %q", r.Reason)
	}
	close(e.hold)
	deadline := time.Now().Add(2 * time.Second)
	for len(e.rb.sem) != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if len(e.rb.sem) != 0 {
		t.Fatal("the slot was never released")
	}
}

func TestRebuildACPOverSizeIsRefused(t *testing.T) {
	e := newACPEnv(t, nil)
	e.err = errTooLarge
	if r := e.rb.Rebuild(context.Background(), e.req(t, "first", true)); r.Reason != ReasonFailed || !strings.Contains(r.Detail, "size") {
		t.Fatalf("over size: %q %q", r.Reason, r.Detail)
	}
}
