package runs

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// canon is the canonical form of an event stream for equivalence: consecutive
// plain chunks of one kind are one item (their text concatenated); every other
// event is its persisted encoding. Two streams with the same canon carry the
// same text, thinking and tool content in the same order.
type canonItem struct {
	Type taskrunner.EventType
	Text string // chunk text, or the persisted encoding of a non-chunk event
}

func canon(t testing.TB, evs []Event) []canonItem {
	t.Helper()
	var out []canonItem
	for _, e := range evs {
		if chunkOnly(e) {
			if n := len(out); n > 0 && out[n-1].Type == e.Type && out[n-1].Text != "" && isChunkType(e.Type) {
				out[n-1].Text += e.Text
				continue
			}
			out = append(out, canonItem{Type: e.Type, Text: e.Text})
			continue
		}
		data, err := encodeEvent(e)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, canonItem{Type: -1, Text: data})
	}
	return out
}

func isChunkType(t taskrunner.EventType) bool {
	return t == taskrunner.EventTypeText || t == taskrunner.EventTypeThinking
}

// canonStream is canon with empty chunks dropped first (an empty chunk carries
// no content; coalescing is free to absorb it).
func canonStream(t testing.TB, evs []Event) []canonItem {
	var kept []Event
	for _, e := range evs {
		if chunkOnly(e) && e.Text == "" {
			continue
		}
		kept = append(kept, e)
	}
	return canon(t, kept)
}

func mustJSON(t testing.TB, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// coalesceEnv is one live run recorded through the real record path.
type coalesceEnv struct {
	e    *histEnv
	reg  *Registry
	live *run
}

func newCoalesceEnv(t *testing.T) *coalesceEnv {
	t.Helper()
	e := newHistEnv(t)
	reg := newTestRegistry(t, e.st)
	now := time.Now().UTC()
	if _, err := e.st.CreateRun(store.Run{ID: "live", TaskID: e.task.ID, Provider: "glm", Prompt: "p", Status: "running", StartedAt: now}); err != nil {
		t.Fatal(err)
	}
	live := &run{id: "live", taskID: e.task.ID, status: StatusRunning, startedAt: now, subscribers: map[int]*subQueue{}, pendingPermissions: map[string]chan string{}, closedCh: make(chan struct{})}
	var once sync.Once
	live.cancel = func() { // CloseAll's cleanup stops it
		once.Do(func() {
			live.mu.Lock()
			live.status = StatusStopped
			live.mu.Unlock()
			close(live.closedCh)
		})
	}
	reg.mu.Lock()
	reg.runs[live.id] = live
	reg.mu.Unlock()
	return &coalesceEnv{e: e, reg: reg, live: live}
}

func (c *coalesceEnv) record(evs []Event) {
	for _, e := range evs {
		c.reg.record(c.live, e)
	}
}

func (c *coalesceEnv) rows(t testing.TB) []Event {
	t.Helper()
	rows, err := c.e.st.ListRunEvents("live")
	if err != nil {
		t.Fatal(err)
	}
	var out []Event
	last := int64(-1)
	for _, r := range rows {
		if r.Seq <= last {
			t.Fatalf("seq not strictly increasing: %d after %d", r.Seq, last)
		}
		last = r.Seq
		ev, err := decodeEvent(r.EventData)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, ev)
	}
	return out
}

func txt(s string) Event   { return Event{Type: taskrunner.EventTypeText, Text: s} }
func think(s string) Event { return Event{Type: taskrunner.EventTypeThinking, Text: s} }
func tool(id, status string, in, res string) Event {
	e := Event{Type: taskrunner.EventTypeToolCall, ToolCallID: id, ToolName: "Bash", ToolStatus: status}
	if in != "" {
		e.ToolInput = json.RawMessage(mustJSONStr(in))
	}
	if res != "" {
		e.ToolResult = json.RawMessage(mustJSONStr(res))
	}
	return e
}

func mustJSONStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func chunks(kind func(string) Event, n int, piece string) []Event {
	out := make([]Event, n)
	for i := range out {
		out[i] = kind(fmt.Sprintf("%s%d", piece, i))
	}
	return out
}

func streamTable() map[string][]Event {
	big := strings.Repeat("0123456789", 5400) // 54 KB answer
	var bigChunks []Event
	for i := 0; i < len(big); i += 1000 {
		bigChunks = append(bigChunks, txt(big[i:i+1000]))
	}
	var interleaved []Event
	interleaved = append(interleaved, Event{Type: taskrunner.EventTypeUserMessage, Text: "go"})
	interleaved = append(interleaved, chunks(think, 5, "t")...)
	interleaved = append(interleaved, chunks(txt, 4, "a")...)
	interleaved = append(interleaved, tool("c1", taskrunner.ToolStatusRunning, "echo 1", ""), tool("c1", taskrunner.ToolStatusSuccess, "", "out 1"))
	interleaved = append(interleaved, chunks(txt, 3, "b")...)
	interleaved = append(interleaved, think("x"), txt("y"), think("z"), txt("w"))
	interleaved = append(interleaved, tool("c2", taskrunner.ToolStatusRunning, "cat <<'EOF'\nbody\nEOF", ""))
	interleaved = append(interleaved, chunks(txt, 2, "c")...)
	interleaved = append(interleaved, Event{Type: taskrunner.EventTypeDone, StopReason: "end_turn"})
	return map[string][]Event{
		"text only":       append(chunks(txt, 300, "w"), Event{Type: taskrunner.EventTypeDone, StopReason: "end_turn"}),
		"thinking only":   chunks(think, 300, "th"),
		"interleaved":     interleaved,
		"multibyte":       {txt("日本"), txt("語のテ"), txt("スト 🎉"), txt("é"), txt("é"), think("✓")},
		"54 KB answer":    bigChunks,
		"one 54 KB chunk": {txt(big)},
		"empty chunks":    {txt(""), txt("a"), txt(""), think(""), txt("b"), txt(""), tool("c", taskrunner.ToolStatusSuccess, "", "r"), txt("")},
		"over the cap":    {txt(strings.Repeat("a", 3000)), txt(strings.Repeat("b", 3000)), txt(strings.Repeat("c", 5000)), txt("d")},
	}
}

// TestCoalescedRowsEquivalentToStream is the contract: for every stream, the
// text, thinking and tool content of the PERSISTED (coalesced) rows is, in
// order, identical to what the uncoalesced stream carries, and the live
// history and every subscriber still get each chunk as its own event.
func TestCoalescedRowsEquivalentToStream(t *testing.T) {
	for name, stream := range streamTable() {
		t.Run(name, func(t *testing.T) {
			c := newCoalesceEnv(t)
			sub, _, err := c.reg.Subscribe("live")
			if err != nil {
				t.Fatal(err)
			}
			c.record(stream)
			c.reg.flushPersist(c.live)

			rows := c.rows(t)
			if got, want := canonStream(t, rows), canonStream(t, stream); fmt.Sprint(got) != fmt.Sprint(want) {
				t.Fatalf("coalesced rows differ from the stream\n got %v\nwant %v", got, want)
			}
			if len(rows) > len(stream) {
				t.Errorf("coalescing produced more rows (%d) than events (%d)", len(rows), len(stream))
			}
			for i, r := range rows {
				if !chunkOnly(r) || len(r.Text) <= persistChunkMax {
					continue
				}
				single := false // only one oversized chunk may exceed the cap, on its own
				for _, e := range stream {
					single = single || (e.Type == r.Type && e.Text == r.Text)
				}
				if !single {
					t.Errorf("row %d is %d bytes, over the %d cap and not a single chunk", i, len(r.Text), persistChunkMax)
				}
			}
			// live side unchanged: memory history has one event per chunk.
			hist, _, err := c.reg.History("live")
			if err != nil {
				t.Fatal(err)
			}
			if len(hist) != len(stream) {
				t.Fatalf("live history has %d events, want %d (one per chunk)", len(hist), len(stream))
			}
			for i := range stream {
				if mustJSON(t, hist[i]) != mustJSON(t, stream[i]) {
					t.Fatalf("live history[%d] changed: %+v want %+v", i, hist[i], stream[i])
				}
			}
			// every subscriber chunk arrives individually.
			got := 0
			for got < len(stream) {
				select {
				case ev := <-sub:
					if mustJSON(t, ev) != mustJSON(t, stream[got]) {
						t.Fatalf("subscriber event %d = %+v, want %+v", got, ev, stream[got])
					}
					got++
				case <-time.After(3 * time.Second):
					t.Fatalf("subscriber got %d of %d events", got, len(stream))
				}
			}
		})
	}
}

func TestChunkRowsActuallyCoalesce(t *testing.T) {
	c := newCoalesceEnv(t)
	c.record(append(chunks(txt, 300, "w"), Event{Type: taskrunner.EventTypeDone, StopReason: "end_turn"}))
	rows := c.rows(t)
	if len(rows) != 2+len(rows)-2 || len(rows) > 6 {
		t.Fatalf("300 small chunks + done wrote %d rows, want a handful", len(rows))
	}
	if rows[len(rows)-1].Type != taskrunner.EventTypeDone {
		t.Fatalf("last row = %+v, want done", rows[len(rows)-1])
	}
}

func TestCoalescingBoundaries(t *testing.T) {
	c := newCoalesceEnv(t)
	c.record([]Event{
		txt("a"), txt("b"), think("c"), think("d"), txt("e"),
		tool("t", taskrunner.ToolStatusRunning, "x", ""), txt("f"), txt("g"),
		txt(strings.Repeat("h", 4000)), txt(strings.Repeat("i", 200)),
	})
	c.reg.flushPersist(c.live)
	var got []string
	for _, r := range c.rows(t) {
		got = append(got, fmt.Sprintf("%d:%d", r.Type, len(r.Text)))
	}
	want := []string{"0:2", "5:2", "0:1", "6:0", "0:4002", "0:200"} // text ab | thinking cd | text e | tool | text fg+h*4000 | i*200 (4 KiB cap)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
}

func TestCoalescedRowKeepsFirstSeqAndLeavesGaps(t *testing.T) {
	c := newCoalesceEnv(t)
	c.record([]Event{txt("a"), txt("b"), txt("c"), tool("t", taskrunner.ToolStatusSuccess, "", "r"), txt("d")})
	c.reg.flushPersist(c.live)
	rows, err := c.e.st.ListRunEvents("live")
	if err != nil {
		t.Fatal(err)
	}
	var seqs []int64
	for _, r := range rows {
		seqs = append(seqs, r.Seq)
	}
	if fmt.Sprint(seqs) != "[0 3 4]" {
		t.Fatalf("seqs = %v, want [0 3 4] (first chunk's seq, then gaps)", seqs)
	}
}

// TestChunksWithOtherFieldsAreNotMerged: a chunk that carries anything beside
// its text is a boundary, never folded into a neighbour.
func TestChunksWithOtherFieldsAreNotMerged(t *testing.T) {
	c := newCoalesceEnv(t)
	odd := txt("odd")
	odd.RawKind = "plan"
	c.record([]Event{txt("a"), odd, txt("b")})
	c.reg.flushPersist(c.live)
	rows := c.rows(t)
	if len(rows) != 3 || rows[1].RawKind != "plan" || rows[0].Text != "a" || rows[2].Text != "b" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestChunkOnlyCoversPersistedFields(t *testing.T) {
	if persistedEventFields != persistedFieldsCovered {
		t.Fatalf("persistedEvent has %d fields, chunkOnly was written for %d: decide what the new field means for coalescing", persistedEventFields, persistedFieldsCovered)
	}
}

// TestSplitRuneIsLosslessWhenCoalesced: a multi-byte character split across two
// chunks was stored as two U+FFFD before (each chunk is JSON-encoded alone);
// merged first, the persisted text is the original.
func TestSplitRuneIsLosslessWhenCoalesced(t *testing.T) {
	c := newCoalesceEnv(t)
	c.record([]Event{txt("ok \xe4\xb8"), txt("\xad ok")})
	c.reg.flushPersist(c.live)
	rows := c.rows(t)
	if len(rows) != 1 || rows[0].Text != "ok 中 ok" {
		t.Fatalf("rows = %+v, want the rune rejoined", rows)
	}
}

// TestCrashLosesOnlyTheBufferedTail: rows written before the kill are exactly
// the stream up to the last boundary; what is lost is the open buffer, at most
// persistChunkMax bytes of text of one kind.
func TestCrashLosesOnlyTheBufferedTail(t *testing.T) {
	c := newCoalesceEnv(t)
	stream := []Event{txt("a"), txt("b"), tool("t", taskrunner.ToolStatusSuccess, "", "r"), think("c"), txt("tail1"), txt("tail2")}
	c.record(stream)
	// No flush: the process is killed here.
	rows := c.rows(t)
	// persisted: "ab", the tool, "c" (flushed by the text that followed it); "tail1tail2" is the lost buffer
	if got, want := canonStream(t, rows), canonStream(t, stream[:4]); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("rows after a crash = %v, want the stream up to the last boundary %v", got, want)
	}
}

func TestIdleFlushWritesBufferWithoutAnotherEvent(t *testing.T) {
	c := newCoalesceEnv(t)
	c.record([]Event{txt("hello "), txt("world")})
	if n := len(c.rows(t)); n != 0 {
		t.Fatalf("%d rows written before the idle window", n)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if rows := c.rows(t); len(rows) == 1 {
			if rows[0].Text != "hello world" {
				t.Fatalf("row = %q", rows[0].Text)
			}
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("the buffer was not flushed after the idle window")
}

func TestIdleFlushWaitsForTheLastChunk(t *testing.T) {
	c := newCoalesceEnv(t)
	// A chunk every 100 ms for 600 ms keeps the buffer open (idle is 250 ms since the LAST chunk).
	for i := 0; i < 6; i++ {
		c.record([]Event{txt("x")})
		time.Sleep(100 * time.Millisecond)
		if n := len(c.rows(t)); n != 0 {
			t.Fatalf("flushed after %d chunks although chunks kept arriving", i+1)
		}
	}
}

// TestStopFlushesBufferedChunks: a run stopped mid-stream (what daemon shutdown
// does) keeps its last chunk: finish writes the buffer before the run ends.
func TestStopFlushesBufferedChunks(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang") // one chunk "before hang", then blocks
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)
	runID, err := reg.Start(t.Context(), wm, runner, task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatal(err)
	}
	waitForHistoryLen(t, reg, runID, 1, 5*time.Second)
	if err := reg.Stop(runID); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, reg, runID, StatusStopped, 10*time.Second)
	rows, err := st.ListRunEvents(runID)
	if err != nil {
		t.Fatal(err)
	}
	var text string
	for _, r := range rows {
		if ev, err := decodeEvent(r.EventData); err == nil && ev.Type == taskrunner.EventTypeText {
			text += ev.Text
		}
	}
	if text != "before hang" {
		t.Fatalf("persisted text after a stop = %q, want %q", text, "before hang")
	}
}

// TestRestartHistoryIsCoalescedAndEquivalent: a restart reads the coalesced rows;
// the text it shows equals the live run's.
func TestRestartHistoryIsCoalescedAndEquivalent(t *testing.T) {
	c := newCoalesceEnv(t)
	stream := streamTable()["interleaved"]
	c.record(stream)
	c.reg.flushPersist(c.live)
	c.live.mu.Lock()
	c.live.status = StatusDone
	c.live.mu.Unlock()
	fin := time.Now().UTC()
	if _, err := c.e.st.UpdateRunStatus("live", "done", &fin, "", ""); err != nil {
		t.Fatal(err)
	}
	reg2 := newTestRegistry(t, c.e.st)
	hist, _, err := reg2.History("live")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(canonStream(t, hist)) != fmt.Sprint(canonStream(t, stream)) {
		t.Fatalf("restarted history differs from the stream\n got %v\nwant %v", canonStream(t, hist), canonStream(t, stream))
	}
	if len(hist) >= len(stream) {
		t.Errorf("restarted history has %d events for %d streamed: not coalesced", len(hist), len(stream))
	}
}
