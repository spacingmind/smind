package runs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// What every reader serves for a skeleton run before and after the 30-day
// excerpt prune (ADR-0053 PR C). The fixture is the readers' pair: two
// 60-day-old runs on an archived task, one stored full and one in skeleton
// form; the stream carries a 3000-byte tool result and a ~1.5 KB heredoc
// input mid-run and ends in a second tool pair so Tail(1) primes its input.
//
//	reader                 | before the prune (excerpts present)      | after the excerpt prune
//	-----------------------+------------------------------------------+----------------------------------------------
//	History                | events whole: h/k results 3000/2500 B,   | same events; results are summaries (< 250 B),
//	                       | inputs the whole heredoc                 | inputs the elided summary
//	Tail(3) + prime        | prime input is the whole heredoc         | prime input is the summary; no error
//	ToolCallHeredoc        | the heredoc body                         | ErrHeredocUnavailable (never ErrNoHeredoc)
//	LastAgentText/Final    | the trailing text rows                   | identical
//	Subscribe              | backfill = History                       | backfill = History
//	ListRunEventsAfterSeq  | full payloads                            | same text, tool results are summaries
//	status                 | HistoryPrunedAt nil                      | HistoryPrunedAt nil, no "pruned" note
//
// The run is never marked pruned: its rows, final text and usage survive.

// pruneReaderStream is skelStream shaped for the tail: the window Tail(3)
// covers the last tool pair, so its prime must fetch the heredoc input from
// before the window, and the run still ends with text (a non-empty trailing
// agent text).
func pruneReaderStream() []Event {
	in, _ := json.Marshal(map[string]string{"command": bigHeredoc})
	s := []Event{{Type: taskrunner.EventTypeUserMessage, Text: "go"}}
	s = append(s, chunks(think, 2, "t")...)
	s = append(s, chunks(txt, 3, "answer ")...)
	s = append(s,
		Event{Type: taskrunner.EventTypeToolCall, ToolCallID: "h", ToolName: "Bash", ToolStatus: taskrunner.ToolStatusRunning, ToolInput: in},
		Event{Type: taskrunner.EventTypeToolCall, ToolCallID: "h", ToolName: "Bash", ToolStatus: taskrunner.ToolStatusSuccess, ToolResult: payload(3000)})
	s = append(s, chunks(txt, 2, "final ")...)
	s = append(s,
		Event{Type: taskrunner.EventTypeToolCall, ToolCallID: "k", ToolName: "Read", ToolStatus: taskrunner.ToolStatusRunning, ToolInput: in},
		Event{Type: taskrunner.EventTypeToolCall, ToolCallID: "k", ToolName: "Read", ToolStatus: taskrunner.ToolStatusSuccess, ToolResult: payload(2500)})
	s = append(s, Event{Type: taskrunner.EventTypeText, Text: "the answer stands"})
	return append(s, Event{Type: taskrunner.EventTypeDone, StopReason: "end_turn"})
}

func TestPrune_ReadersAfterAnExcerptPrune(t *testing.T) {
	p := newSkelPair(t, pruneReaderStream(), true)
	reg := p.reg
	page := func() (text string, tools []string) {
		cursor, last := int64(-1), int64(-1)
		for {
			rows, err := p.e.st.ListRunEventsAfterSeq(p.skl, cursor, 2)
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
	byID := func(h []Event, id string) (input, result Event) {
		for _, e := range h {
			if e.Type != taskrunner.EventTypeToolCall || e.ToolCallID != id {
				continue
			}
			if len(e.ToolInput) > 0 {
				input = e
			}
			if e.ToolStatus == taskrunner.ToolStatusSuccess {
				result = e
			}
		}
		return
	}

	// before
	histBefore, statusBefore, err := reg.History(p.skl)
	if err != nil {
		t.Fatal(err)
	}
	tailBefore, err := reg.Tail(p.skl, 3)
	if err != nil {
		t.Fatal(err)
	}
	revBefore, err := reg.ToolCallHeredoc(p.skl, "k", 0)
	if err != nil || !strings.Contains(revBefore.Text, "secret body line") {
		t.Fatalf("heredoc before the prune = %q, %v; want the body", revBefore.Text, err)
	}
	lastBefore, _ := reg.LastAgentText(p.skl)
	finalBefore, _ := reg.FinalAgentText(p.skl)
	textBefore, toolsBefore := page()

	// the prune: the skeleton run's excerpts go, its rows stay
	rep, err := p.pruner().Run(context.Background())
	if err != nil || rep.Runs != 1 || rep.ExcerptRuns != 1 || rep.ExcerptRows == 0 {
		t.Fatalf("prune = %+v, %v", rep, err)
	}
	if sk, _ := p.e.st.GetRunSkeleton(p.skl); sk.ExcerptsPrunedAt == nil {
		t.Fatal("fixture: the excerpt prune did not run")
	}

	// (1) History: same events, the big payloads now come back as summaries.
	histAfter, statusAfter, err := reg.History(p.skl)
	if err != nil || len(histAfter) != len(histBefore) {
		t.Fatalf("History after = %d events (%d before), %v", len(histAfter), len(histBefore), err)
	}
	for i := range histAfter {
		a, b := histAfter[i], histBefore[i]
		if a.Type != b.Type || a.Text != b.Text || a.ToolCallID != b.ToolCallID || a.ToolName != b.ToolName || a.ToolStatus != b.ToolStatus {
			t.Fatalf("event %d changed:\n got %+v\nwant %+v", i, a, b)
		}
	}
	for _, id := range []string{"h", "k"} {
		inB, resB := byID(histBefore, id)
		inA, resA := byID(histAfter, id)
		whole := map[string]int{"h": 3000, "k": 2500}[id]
		if len(resB.ToolResult) != whole || len(resA.ToolResult) >= 250 {
			t.Fatalf("%s: tool result before %d bytes, after %d; want %d then a summary under 250", id, len(resB.ToolResult), len(resA.ToolResult), whole)
		}
		if !strings.Contains(string(inB.ToolInput), "secret body line") || strings.Contains(string(inA.ToolInput), "secret body line") || len(inA.ToolInput) > 600 {
			t.Fatalf("%s: input before %d bytes, after %d bytes; want the whole heredoc then its summary", id, len(inB.ToolInput), len(inA.ToolInput))
		}
	}

	// (2) Tail(3): the primed input is the summary, with no error.
	tailAfter, err := reg.Tail(p.skl, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(tailAfter.Events) != len(tailBefore.Events) || len(tailAfter.Prime) != len(tailBefore.Prime) || len(tailAfter.Prime) == 0 {
		t.Fatalf("Tail(3) shape changed: %d/%d events, %d/%d prime", len(tailAfter.Events), len(tailBefore.Events), len(tailAfter.Prime), len(tailBefore.Prime))
	}
	if in := string(tailAfter.Prime[0].ToolInput); strings.Contains(in, "secret body line") || len(in) > 600 {
		t.Fatalf("Tail(3) prime input = %.100s (%d bytes); want the summary", in, len(in))
	}

	// (3) ToolCallHeredoc: the body is unavailable, and says so. Read through a
	// registry that loaded the run from the store (as after a restart): a
	// resident copy of the history carries no truncation flags.
	if _, err := newTestRegistry(t, p.e.st).ToolCallHeredoc(p.skl, "k", 0); !errors.Is(err, ErrHeredocUnavailable) {
		t.Fatalf("ToolCallHeredoc after = %v, want ErrHeredocUnavailable", err)
	}

	// (4) LastAgentText / FinalAgentText.
	if last, _ := reg.LastAgentText(p.skl); last != lastBefore || last == "" {
		t.Fatalf("LastAgentText = %q, before %q", last, lastBefore)
	}
	if final, _ := reg.FinalAgentText(p.skl); final != finalBefore {
		t.Fatalf("FinalAgentText = %q, before %q", final, finalBefore)
	}

	// (5) Subscribe backfills the same content as History.
	ch, unsub, err := reg.Subscribe(p.skl)
	if err != nil {
		t.Fatal(err)
	}
	if got := drain(t, ch); fmt.Sprint(canonSkel(t, got)) != fmt.Sprint(canonSkel(t, histAfter)) {
		t.Fatal("Subscribe backfill differs from History after the prune")
	}
	unsub()

	// (6) Store paging: same text, tool results are summaries.
	textAfter, toolsAfter := page()
	if textAfter != textBefore {
		t.Fatalf("paged text changed:\n got %q\nwant %q", textAfter, textBefore)
	}
	if len(toolsAfter) != len(toolsBefore) || fmt.Sprint(toolsBefore) == fmt.Sprint(toolsAfter) {
		t.Fatalf("paged tool rows before %v after %v; same count, different payloads", toolsBefore, toolsAfter)
	}

	// (7) The run is not reported as pruned.
	for _, e := range histAfter {
		if e.Type == taskrunner.EventTypeSessionNote {
			t.Fatalf("a session note reached the history: %+v", e)
		}
	}
	if statusBefore.HistoryPrunedAt != nil || statusAfter.HistoryPrunedAt != nil {
		t.Fatalf("HistoryPrunedAt set: before %v after %v", statusBefore.HistoryPrunedAt, statusAfter.HistoryPrunedAt)
	}
}
