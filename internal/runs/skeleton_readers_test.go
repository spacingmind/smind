package runs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// Reader equivalence for skeleton form (ADR-0053, the #810 discipline). The
// same stream is stored twice as a finished run, once one row per event
// ("full", how every run was stored) and once collapsed by the finalizer
// ("skel"). Each test names what the reader consumes, runs it on both and
// compares. What the skeleton drops is thinking text (read back as a
// placeholder) and the middle of payloads over 8 KiB; everything else must be
// identical.

var bigHeredoc = "cat > f <<'EOF'\n" + strings.Repeat("secret body line\n", 90) + "EOF\necho done" // ~1.5 KB

func skelStream(extra ...Event) []Event {
	in, _ := json.Marshal(map[string]string{"command": bigHeredoc})
	s := []Event{{Type: taskrunner.EventTypeUserMessage, Text: "go"}}
	s = append(s, chunks(think, 4, "t")...)
	s = append(s, chunks(txt, 5, "answer ")...)
	s = append(s, Event{Type: taskrunner.EventTypeToolCall, ToolCallID: "h", ToolName: "Bash", ToolStatus: taskrunner.ToolStatusRunning, ToolInput: in},
		Event{Type: taskrunner.EventTypeToolCall, ToolCallID: "h", ToolName: "Bash", ToolStatus: taskrunner.ToolStatusSuccess, ToolResult: payload(3000)})
	s = append(s, extra...)
	s = append(s, chunks(txt, 4, "final ")...)
	return append(s, Event{Type: taskrunner.EventTypeDone, StopReason: "end_turn"})
}

// canonSkel is canon with thinking rows reduced to a marker: their text is the
// one thing the skeleton does not keep.
func canonSkel(t testing.TB, evs []Event) []canonItem {
	out := canonStream(t, evs)
	for i := range out {
		if out[i].Type == taskrunner.EventTypeThinking {
			out[i].Text = "<thinking>"
		}
	}
	return out
}

type skelPair struct {
	e         *histEnv
	reg       *Registry // fresh: both runs are read from the store
	full, skl string
}

func newSkelPair(t *testing.T, stream []Event, old bool) *skelPair {
	return newSkelPairShrink(t, stream, old, true)
}

func newSkelPairShrink(t *testing.T, stream []Event, old, mustShrink bool) *skelPair {
	t.Helper()
	e := newHistEnv(t)
	started := time.Now().Add(-time.Hour).UTC()
	if old {
		started = time.Now().Add(-60 * 24 * time.Hour).UTC()
		if _, err := e.st.ArchiveTask(e.task.ID); err != nil {
			t.Fatal(err)
		}
	}
	seedFinished(t, e.st, e.task.ID, "full", started, stream)
	seedFinished(t, e.st, e.task.ID, "skel", started, stream)
	if err := Skeletonize(e.st, "skel"); err != nil {
		t.Fatal(err)
	}
	if sk, _ := e.st.GetRunSkeleton("skel"); sk.SkeletonAt == nil {
		t.Fatal("fixture: the run was not converted")
	}
	fr, _ := e.st.ListRunEventsRaw("full")
	sr, _ := e.st.ListRunEventsRaw("skel")
	if mustShrink && len(sr) >= len(fr) {
		t.Fatalf("fixture: %d skeleton rows vs %d full rows", len(sr), len(fr))
	}
	return &skelPair{e: e, reg: newTestRegistry(t, e.st), full: "full", skl: "skel"}
}

// History consumes: every stored row of a finished run, decoded. After: the same
// content; thinking is a placeholder.
func TestSkeletonReader_History(t *testing.T) {
	p := newSkelPair(t, skelStream(), false)
	a, _, _ := p.reg.History(p.full)
	b, _, _ := p.reg.History(p.skl)
	if fmt.Sprint(canonSkel(t, a)) != fmt.Sprint(canonSkel(t, b)) {
		t.Fatalf("History differs\nfull %v\nskel %v", canonSkel(t, a), canonSkel(t, b))
	}
}

// Tail consumes: the last n rows, plus the earlier rows that carry the input of
// the tool calls in that window (the heredoc priming). Windows count rows and
// the finalizer merges answer rows, so n events cover different text; what must
// match is a window that ends in a tool result (and its primed input) and the
// whole run.
func TestSkeletonReader_TailAndPriming(t *testing.T) {
	in, _ := json.Marshal(map[string]string{"command": bigHeredoc})
	endsInTool := append(skelStream()[:9:9], Event{Type: taskrunner.EventTypeToolCall, ToolCallID: "k", ToolName: "Bash", ToolStatus: taskrunner.ToolStatusRunning, ToolInput: in},
		Event{Type: taskrunner.EventTypeToolCall, ToolCallID: "k", ToolName: "Bash", ToolStatus: taskrunner.ToolStatusSuccess, ToolResult: payload(2500)})
	p := newSkelPair(t, endsInTool, false)
	enc := func(evs []Event) (out []string) {
		for _, e := range evs {
			d, _ := encodeEvent(e)
			out = append(out, d)
		}
		return
	}
	a, err := p.reg.Tail(p.full, 1)
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.reg.Tail(p.skl, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Prime) == 0 || fmt.Sprint(enc(a.Events)) != fmt.Sprint(enc(b.Events)) || fmt.Sprint(enc(a.Prime)) != fmt.Sprint(enc(b.Prime)) {
		t.Fatalf("Tail(1) differs\nfull %v | prime %v\nskel %v | prime %v", enc(a.Events), enc(a.Prime), enc(b.Events), enc(b.Prime))
	}
	all := skelStream()
	q := newSkelPair(t, all, false)
	x, _ := q.reg.Tail(q.full, 1000)
	y, _ := q.reg.Tail(q.skl, 1000)
	if fmt.Sprint(canonSkel(t, x.Events)) != fmt.Sprint(canonSkel(t, y.Events)) {
		t.Fatal("Tail(all) differs")
	}
}

// ToolCallHeredoc consumes: the call's stored input. A finished run's input is
// whole in the excerpt tier up to 8 KiB, so the reveal is identical; over 8 KiB
// or after the excerpt is gone it is the typed ErrHeredocUnavailable, never
// "no heredoc".
func TestSkeletonReader_HeredocReveal(t *testing.T) {
	p := newSkelPair(t, skelStream(), false)
	a, err := p.reg.ToolCallHeredoc(p.full, "h", 0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.reg.ToolCallHeredoc(p.skl, "h", 0)
	if err != nil {
		t.Fatalf("reveal on a skeleton run: %v", err)
	}
	if a.Text != b.Text || !strings.Contains(b.Text, "secret body line") {
		t.Fatalf("reveal full %q skel %q", a.Text, b.Text)
	}

	// the excerpt pruned away: the body is unavailable, and says so
	if _, err := p.e.st.DB().Exec(`DELETE FROM run_excerpts WHERE run_id = 'skel'`); err != nil {
		t.Fatal(err)
	}
	if _, err := newTestRegistry(t, p.e.st).ToolCallHeredoc(p.skl, "h", 0); !errors.Is(err, ErrHeredocUnavailable) {
		t.Fatalf("pruned excerpt: err = %v, want ErrHeredocUnavailable", err)
	}
}

func TestSkeletonReader_HeredocOverEightKiB(t *testing.T) {
	huge := "cat > f <<'EOF'\n" + strings.Repeat("0123456789abcdef\n", 700) + "EOF" // > 8 KiB
	in, _ := json.Marshal(map[string]string{"command": huge})
	p := newSkelPairShrink(t, []Event{{Type: taskrunner.EventTypeToolCall, ToolCallID: "h", ToolName: "Bash", ToolStatus: taskrunner.ToolStatusRunning, ToolInput: in}, {Type: taskrunner.EventTypeDone}}, false, false)
	if _, err := p.reg.ToolCallHeredoc(p.full, "h", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := p.reg.ToolCallHeredoc(p.skl, "h", 0); !errors.Is(err, ErrHeredocUnavailable) {
		t.Fatalf("err = %v, want ErrHeredocUnavailable (head and tail only)", err)
	}
}

// LastAgentText/FinalAgentText consume: the trailing text rows of the run.
func TestSkeletonReader_LastAndFinalAgentText(t *testing.T) {
	p := newSkelPair(t, skelStream(), false)
	for name, f := range map[string]func(string) (string, error){"Last": p.reg.LastAgentText, "Final": p.reg.FinalAgentText} {
		a, _ := f(p.full)
		b, err := f(p.skl)
		if err != nil || a != b || !strings.HasPrefix(a, "final 0final 1") {
			t.Fatalf("%s: full %q skel %q err %v", name, a, b, err)
		}
	}
}

// Subscribe consumes: the stored rows of a finished run (backfill, then close).
func TestSkeletonReader_Subscribe(t *testing.T) {
	p := newSkelPair(t, skelStream(), false)
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
	if fmt.Sprint(canonSkel(t, read(p.full))) != fmt.Sprint(canonSkel(t, read(p.skl))) {
		t.Fatal("Subscribe backfill differs")
	}
}

// The 30-day prune consumes (PR C): a full run's rows, final text and
// excerpts; a skeleton run's excerpt tier only -- its rows, final text and
// usage stay, so readers keep serving the skeleton.
func TestSkeletonReader_RetentionPrune(t *testing.T) {
	p := newSkelPair(t, skelStream(), true)
	pruner := NewPruner(p.reg, p.e.st, func() int { return 30 })
	pruner.pause, pruner.batch = 0, 50
	rowsBefore := eventCount(t, p.e.st, p.skl)
	rep, err := pruner.Run(context.Background())
	if err != nil || rep.Runs != 1 || rep.ExcerptRuns != 1 || rep.ExcerptRows == 0 {
		t.Fatalf("prune = %+v, %v; want the full run's rows and the skeleton's excerpts", rep, err)
	}
	if txt, _ := p.e.st.GetRunFinalText(p.full); !strings.HasPrefix(txt, "final 0final 1") {
		t.Fatalf("full run final text = %q", txt)
	}
	if n := eventCount(t, p.e.st, p.full); n != 0 {
		t.Fatalf("%s: %d rows left", p.full, n)
	}
	if n := eventCount(t, p.e.st, p.skl); n != rowsBefore {
		t.Fatalf("%s: %d rows left, want the %d it had", p.skl, n, rowsBefore)
	}
	if txt, _ := p.e.st.GetRunFinalText(p.skl); txt != "" {
		t.Fatalf("%s: final text = %q, want none (its rows keep the text)", p.skl, txt)
	}
	if row, _ := p.e.st.GetRun(p.skl); row.HistoryPrunedAt != nil {
		t.Fatalf("%s was marked history-pruned", p.skl)
	}
	var left int
	if err := p.e.st.DB().QueryRow(`SELECT COUNT(*) FROM run_excerpts`).Scan(&left); err != nil || left != 0 {
		t.Fatalf("%d excerpts left after the prune (%v)", left, err)
	}
}

// Store paging (fleet, steward, learning) consumes: ListRunEventsAfterSeq rows.
func TestSkeletonReader_StorePaging(t *testing.T) {
	p := newSkelPair(t, skelStream(), false)
	read := func(id string, limit int) (text string, tools []string) {
		cursor, last := int64(-1), int64(-1)
		for {
			rows, err := p.e.st.ListRunEventsAfterSeq(id, cursor, limit)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) == 0 {
				return
			}
			for _, r := range rows {
				if r.Seq <= last {
					t.Fatalf("seq %d after %d", r.Seq, last)
				}
				last, cursor = r.Seq, r.Seq
				ev, _ := decodeEvent(r.EventData)
				switch ev.Type {
				case taskrunner.EventTypeText:
					text += ev.Text
				case taskrunner.EventTypeToolCall:
					tools = append(tools, fmt.Sprintf("%s/%s/%d/%d", ev.ToolName, ev.ToolStatus, len(ev.ToolInput), len(ev.ToolResult)))
				}
			}
		}
	}
	for _, limit := range []int{1, 2, 100} {
		ta, ka := read(p.full, limit)
		tb, kb := read(p.skl, limit)
		if ta != tb || fmt.Sprint(ka) != fmt.Sprint(kb) {
			t.Fatalf("limit %d: full %q %v skel %q %v", limit, ta, ka, tb, kb)
		}
	}
}
