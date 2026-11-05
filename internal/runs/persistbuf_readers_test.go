package runs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// Reader audit for write-time coalescing (ADR-0053 H3): every reader in this
// package gives, on a run whose chunk rows were coalesced, the same answer as on
// the same run stored one row per chunk (the legacy form, which stays in every
// existing database). Readers in other packages have their own tests.

const readerHeredoc = "cat > f <<'EOF'\nsecret body line\nEOF\necho done"

// readerStream ends in a heredoc tool call and its result, so Tail(1) must prime
// the call's input; the first part has chunk runs of both kinds.
func readerStream() []Event {
	s := []Event{{Type: taskrunner.EventTypeUserMessage, Text: "go"}}
	s = append(s, chunks(think, 6, "t")...)
	s = append(s, chunks(txt, 8, "answer ")...)
	s = append(s, toolCmd("c1", readerHeredoc), tool("c1", taskrunner.ToolStatusSuccess, "", "ok"))
	return s
}

// trailing ends in agent text, for LastAgentText / FinalAgentText.
func trailingStream() []Event {
	s := []Event{{Type: taskrunner.EventTypeUserMessage, Text: "go"}}
	s = append(s, chunks(think, 4, "t")...)
	s = append(s, tool("c1", taskrunner.ToolStatusRunning, "ls", ""), tool("c1", taskrunner.ToolStatusSuccess, "", "a b"))
	s = append(s, chunks(txt, 9, "final ")...)
	return append(s, Event{Type: taskrunner.EventTypeDone, StopReason: "end_turn"})
}

type pair struct {
	e              *histEnv
	reg            *Registry // a fresh registry: both runs read from the store
	legacy, coaled string
}

// newPair stores stream twice: one row per event (legacy) and through the real
// record path (coalesced), both finished, both 60 days old on an archived task
// when old is set (for the prune reader).
func newPair(t *testing.T, stream []Event, old bool) *pair {
	t.Helper()
	e := newHistEnv(t)
	started := time.Now().Add(-time.Hour).UTC()
	if old {
		started = time.Now().Add(-60 * 24 * time.Hour).UTC()
		if _, err := e.st.ArchiveTask(e.task.ID); err != nil {
			t.Fatal(err)
		}
	}
	seedFinished(t, e.st, e.task.ID, "legacy", started, stream)

	writer := newTestRegistry(t, e.st)
	live := registerLive(t, e, writer, "coalesced", started)
	for _, ev := range stream {
		writer.record(live, ev)
	}
	writer.flushPersist(live)
	fin := started.Add(time.Second)
	if _, err := e.st.UpdateRunStatus("coalesced", "done", &fin, "", ""); err != nil {
		t.Fatal(err)
	}
	legacyRows, _ := e.st.ListRunEvents("legacy")
	coalRows, _ := e.st.ListRunEvents("coalesced")
	if len(coalRows) >= len(legacyRows) {
		t.Fatalf("fixture is not coalesced: %d rows vs %d legacy", len(coalRows), len(legacyRows))
	}
	return &pair{e: e, reg: newTestRegistry(t, e.st), legacy: "legacy", coaled: "coalesced"}
}

func TestReaders_History(t *testing.T) {
	p := newPair(t, readerStream(), false)
	a, _, err := p.reg.History(p.legacy)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := p.reg.History(p.coaled)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(canon(t, a)) != fmt.Sprint(canon(t, b)) {
		t.Fatalf("History differs\nlegacy    %v\ncoalesced %v", canon(t, a), canon(t, b))
	}
}

// TestReaders_TailAndHeredocPriming: the last event is a tool result whose input
// (with the heredoc) is outside a Tail(1) window; priming brings it, identically.
func TestReaders_TailAndHeredocPriming(t *testing.T) {
	p := newPair(t, readerStream(), false)
	for _, n := range []int{1, 2, 1000} {
		a, err := p.reg.Tail(p.legacy, n)
		if err != nil {
			t.Fatal(err)
		}
		b, err := p.reg.Tail(p.coaled, n)
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprint(canon(t, a.Events)) != fmt.Sprint(canon(t, b.Events)) || fmt.Sprint(canon(t, a.Prime)) != fmt.Sprint(canon(t, b.Prime)) {
			t.Fatalf("Tail(%d) differs\nlegacy    %v | prime %v\ncoalesced %v | prime %v", n, canon(t, a.Events), canon(t, a.Prime), canon(t, b.Events), canon(t, b.Prime))
		}
		if n == 1 && len(b.Prime) == 0 {
			t.Fatalf("Tail(1) on the coalesced run primed nothing: the heredoc input is lost")
		}
	}
}

func TestReaders_HeredocReveal(t *testing.T) {
	p := newPair(t, readerStream(), false)
	a, err := p.reg.ToolCallHeredoc(p.legacy, "c1", 0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.reg.ToolCallHeredoc(p.coaled, "c1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if a.Text != b.Text || !strings.Contains(b.Text, "secret body line") {
		t.Fatalf("reveal legacy %q, coalesced %q", a.Text, b.Text)
	}
}

func TestReaders_LastAndFinalAgentText(t *testing.T) {
	p := newPair(t, trailingStream(), false)
	for name, f := range map[string]func(string) (string, error){"LastAgentText": p.reg.LastAgentText, "FinalAgentText": p.reg.FinalAgentText} {
		a, err := f(p.legacy)
		if err != nil {
			t.Fatal(err)
		}
		b, err := f(p.coaled)
		if err != nil {
			t.Fatal(err)
		}
		if a != b || !strings.HasPrefix(a, "final 0final 1") {
			t.Fatalf("%s legacy %q, coalesced %q", name, a, b)
		}
	}
}

// TestReaders_Subscribe: a finished run backfills (and closes) with its stored events.
func TestReaders_Subscribe(t *testing.T) {
	p := newPair(t, readerStream(), false)
	read := func(id string) []Event {
		ch, unsub, err := p.reg.Subscribe(id)
		if err != nil {
			t.Fatal(err)
		}
		defer unsub()
		var out []Event
		for e := range ch {
			out = append(out, e)
		}
		return out
	}
	if fmt.Sprint(canon(t, read(p.legacy))) != fmt.Sprint(canon(t, read(p.coaled))) {
		t.Fatal("Subscribe backfill differs")
	}
}

// TestReaders_RetentionPrune: the 30-day prune keeps the same final text and
// summary for a coalesced run as for a legacy one, and deletes all of its rows.
func TestReaders_RetentionPrune(t *testing.T) {
	p := newPair(t, trailingStream(), true)
	pruner := NewPruner(p.reg, p.e.st, func() int { return 30 })
	pruner.pause, pruner.batch = 0, 50
	rep, err := pruner.Run(context.Background())
	if err != nil || rep.Runs != 2 {
		t.Fatalf("prune = %+v, %v; want both runs pruned", rep, err)
	}
	for _, id := range []string{p.legacy, p.coaled} {
		if n := eventCount(t, p.e.st, id); n != 0 {
			t.Fatalf("%s: %d rows left", id, n)
		}
	}
	a, _ := p.e.st.GetRunFinalText(p.legacy)
	b, _ := p.e.st.GetRunFinalText(p.coaled)
	if a != b || !strings.HasPrefix(a, "final 0final 1") {
		t.Fatalf("final text legacy %q, coalesced %q", a, b)
	}
	ha, _, _ := p.reg.History(p.legacy)
	hb, _, _ := p.reg.History(p.coaled)
	if fmt.Sprint(canon(t, ha)) != fmt.Sprint(canon(t, hb)) {
		t.Fatal("pruned summary differs")
	}
}

// TestReaders_StoreLevel: rows of both forms through the store reads other
// packages use (fleet, learning, steward), as raw text per run.
func TestReaders_StoreLevel(t *testing.T) {
	p := newPair(t, readerStream(), false)
	text := func(id string, after int64) (s string, maxSeq int64) {
		rows, err := p.e.st.ListRunEventsAfterSeq(id, after, 1000)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			ev, _ := decodeEvent(r.EventData)
			if chunkOnly(ev) {
				s += fmt.Sprintf("[%d]%s", ev.Type, ev.Text)
			}
			maxSeq = r.Seq
		}
		return
	}
	a, _ := text(p.legacy, -1)
	b, _ := text(p.coaled, -1)
	// same chunk text per kind, in order (kind markers collapse when merged)
	strip := func(s string) (th, an string) {
		for _, part := range strings.Split(s, "[")[1:] {
			if k, rest, ok := strings.Cut(part, "]"); ok {
				if k == fmt.Sprint(int(taskrunner.EventTypeThinking)) {
					th += rest
				} else {
					an += rest
				}
			}
		}
		return
	}
	at, aa := strip(a)
	bt, ba := strip(b)
	if at != bt || aa != ba {
		t.Fatalf("fleet-style read differs: thinking %q vs %q, text %q vs %q", at, bt, aa, ba)
	}
	// A reader that resumes after the last seq it saw gets nothing new, and one
	// that resumes mid-way never sees a row twice.
	_, last := text(p.coaled, -1)
	if rest, _ := text(p.coaled, last); rest != "" {
		t.Fatalf("rows after the last seq: %q", rest)
	}
}

// toolCmd is a running execute call whose input is {"command": cmd}.
func toolCmd(id, cmd string) Event {
	return Event{Type: taskrunner.EventTypeToolCall, ToolCallID: id, ToolName: "Bash", ToolStatus: taskrunner.ToolStatusRunning,
		ToolInput: json.RawMessage(mustJSON2(map[string]string{"command": cmd}))}
}

func mustJSON2(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
