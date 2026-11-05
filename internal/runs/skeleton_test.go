package runs

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// payload is a JSON string literal whose encoded size is exactly n bytes.
func payload(n int) json.RawMessage { return json.RawMessage(`"` + strings.Repeat("x", n-2) + `"`) }

func toolWith(id string, in, res json.RawMessage) Event {
	return Event{Type: taskrunner.EventTypeToolCall, ToolCallID: id, ToolName: "Bash", ToolStatus: taskrunner.ToolStatusSuccess, ToolInput: in, ToolResult: res}
}

// skelEnv is a finished run stored one row per event, and a finalizer to run on it.
type skelEnv struct {
	e   *histEnv
	reg *Registry
}

func newSkelEnv(t *testing.T) *skelEnv {
	e := newHistEnv(t)
	return &skelEnv{e: e, reg: newTestRegistry(t, e.st)}
}

// seed stores evs as finished run id (one row per event, as every run was stored).
func (s *skelEnv) seed(t *testing.T, id string, evs []Event) {
	t.Helper()
	seedFinished(t, s.e.st, s.e.task.ID, id, time.Now().Add(-time.Hour).UTC(), evs)
}

func (s *skelEnv) finalize(t *testing.T, id string) {
	t.Helper()
	if err := Skeletonize(s.e.st, id); err != nil {
		t.Fatal(err)
	}
}

func (s *skelEnv) raw(t *testing.T, id string) []store.RunEvent {
	t.Helper()
	rows, err := s.e.st.ListRunEventsRaw(id)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func (s *skelEnv) excerpts(t *testing.T, id string) int {
	t.Helper()
	ids, err := s.e.st.ListRunEventsRaw(id)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, r := range ids {
		var p persistedEvent
		_ = json.Unmarshal([]byte(r.EventData), &p)
		if p.Skel != nil {
			if p.Skel.In != nil && p.Skel.In.Ex {
				n++
			}
			if p.Skel.Out != nil && p.Skel.Out.Ex {
				n++
			}
		}
	}
	return n
}

// TestSkeletonThresholds: the frozen 512 B / 8 KiB / 4+4 KiB rule on a tool result.
func TestSkeletonThresholds(t *testing.T) {
	for _, tc := range []struct {
		n         int
		excerpt   bool
		wholeTier bool
	}{
		{2, false, false}, {200, false, false}, {512, false, false}, // inline: the row is untouched
		{513, true, true}, {8192, true, true}, // whole in the excerpt tier
		{8193, true, false}, {40000, true, false}, // head 4 KiB + tail 4 KiB
	} {
		t.Run(fmt.Sprint(tc.n), func(t *testing.T) {
			s := newSkelEnv(t)
			res := payload(tc.n)
			s.seed(t, "r", []Event{toolWith("c", nil, res), {Type: taskrunner.EventTypeDone, StopReason: "end_turn"}})
			before := s.raw(t, "r")
			s.finalize(t, "r")
			after := s.raw(t, "r")
			if !tc.excerpt {
				if after[0].EventData != before[0].EventData {
					t.Fatalf("a payload of %d bytes must stay in its row unchanged", tc.n)
				}
				return
			}
			var p persistedEvent
			if err := json.Unmarshal([]byte(after[0].EventData), &p); err != nil || p.V != 2 || p.Skel == nil || p.Skel.Out == nil {
				t.Fatalf("row not in skeleton form: %s", after[0].EventData)
			}
			if len(p.ToolResult) > resultSummaryCap+8 || !json.Valid(p.ToolResult) {
				t.Fatalf("result summary %d bytes (valid=%v), cap %d", len(p.ToolResult), json.Valid(p.ToolResult), resultSummaryCap)
			}
			if p.Skel.Out.N != int64(tc.n) || p.Skel.Out.SHA == "" || p.Skel.Out.SHA != sha256Hex(string(res)) {
				t.Fatalf("descriptor = %+v", p.Skel.Out)
			}
			got, err := s.e.st.ListRunEvents("r")
			if err != nil {
				t.Fatal(err)
			}
			ev, _ := decodeEvent(got[0].EventData)
			if tc.wholeTier {
				if string(ev.ToolResult) != string(res) || got[0].Truncated {
					t.Fatalf("a payload of %d bytes must come back whole", tc.n)
				}
				return
			}
			if !got[0].Truncated {
				t.Fatalf("head+tail payload not marked truncated")
			}
			var text string
			if err := json.Unmarshal(ev.ToolResult, &text); err != nil {
				t.Fatalf("rehydrated head+tail is not a JSON string: %v", err)
			}
			whole := string(res)
			if !strings.HasPrefix(text, whole[:store.ExcerptHalf]) || !strings.HasSuffix(text, whole[len(whole)-store.ExcerptHalf:]) ||
				!strings.Contains(text, "bytes omitted") {
				t.Fatalf("head/tail wrong (%d bytes)", len(text))
			}
		})
	}
}

func TestSkeletonHeadTailRuneBoundary(t *testing.T) {
	s := newSkelEnv(t)
	body := `"` + strings.Repeat("日", 6000) + `"` // 18002 bytes, every rune 3 bytes
	s.seed(t, "r", []Event{toolWith("c", nil, json.RawMessage(body)), {Type: taskrunner.EventTypeDone}})
	s.finalize(t, "r")
	got, _ := s.e.st.ListRunEvents("r")
	ev, _ := decodeEvent(got[0].EventData)
	var text string
	if err := json.Unmarshal(ev.ToolResult, &text); err != nil || !utf8.ValidString(text) || strings.ContainsRune(text, utf8.RuneError) {
		t.Fatalf("a head/tail cut split a character: %v valid=%v", err, utf8.ValidString(text))
	}
}

func TestSkeletonRules(t *testing.T) {
	s := newSkelEnv(t)
	long := strings.Repeat("a", 70000)
	heredoc := "cat > f <<'EOF'\n" + strings.Repeat("secret body line\n", 100) + "EOF\necho done"
	in, _ := json.Marshal(map[string]string{"command": heredoc})
	evs := []Event{
		{Type: taskrunner.EventTypeUserMessage, Text: strings.Repeat("u", 40000)},
		{Type: taskrunner.EventTypeThinking, Text: "thought one"},
		{Type: taskrunner.EventTypeThinking, Text: "thought two"},
		{Type: taskrunner.EventTypeText, Text: "answer "},
		{Type: taskrunner.EventTypeText, Text: "part"},
		{Type: taskrunner.EventTypeText, Text: " two"},
		{Type: taskrunner.EventTypeToolCall, ToolCallID: "h", ToolName: "Bash", ToolStatus: taskrunner.ToolStatusRunning, ToolInput: in, ToolTitle: strings.Repeat("T", 2000)},
		{Type: taskrunner.EventTypePermissionRequest, PermissionRequestID: "p", PermissionSummary: "run it"},
		{Type: taskrunner.EventTypeUsage, Usage: &taskrunner.Usage{}},
		{Type: taskrunner.EventTypeText, Text: long},
		{Type: taskrunner.EventTypeDone, StopReason: "end_turn"},
	}
	s.seed(t, "r", evs)
	s.finalize(t, "r")
	rows := s.raw(t, "r")
	dec := func(i int) persistedEvent {
		var p persistedEvent
		if err := json.Unmarshal([]byte(rows[i].EventData), &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// 11 rows: the two later text chunks merged into the first, so 9 remain.
	if len(rows) != 9 {
		t.Fatalf("%d rows after conversion, want 9 (two text rows merged away)", len(rows))
	}
	if p := dec(0); p.V != 2 || p.Skel.Txt == nil || p.Skel.Txt.N != 40000 || len(p.Text) > store.UserRowCap+64 {
		t.Fatalf("user message not capped: v=%d len=%d skel=%+v", p.V, len(p.Text), p.Skel)
	}
	for _, i := range []int{1, 2} {
		if p := dec(i); p.V != 2 || p.Text != "" || p.Skel.Think == nil || p.Skel.Think.N == 0 {
			t.Fatalf("thinking row %d = %+v", i, p)
		}
	}
	if p := dec(3); p.Text != "answer part two" || p.V != 0 {
		t.Fatalf("merged answer = %q (v=%d), want %q as a plain v1 row", p.Text, p.V, "answer part two")
	}
	tool := dec(4)
	if tool.V != 2 || tool.Skel.In == nil || !tool.Skel.In.Ex || len(tool.Skel.In.Doc) != 1 || tool.Skel.In.Doc[0].N == 0 {
		t.Fatalf("tool row = %+v", tool.Skel)
	}
	if strings.Contains(string(tool.ToolInput), "secret body line") || len(tool.ToolInput) > argsSummaryCap+8 {
		t.Fatalf("args summary leaks the heredoc body or is over the cap: %d bytes: %.120s", len(tool.ToolInput), tool.ToolInput)
	}
	if len(tool.ToolTitle) > titleCap+64 {
		t.Fatalf("title %d bytes", len(tool.ToolTitle))
	}
	if r := rows[5].EventData; strings.Contains(r, `"v":2`) {
		t.Fatalf("a permission row must be untouched: %s", r)
	}
	if p := dec(7); p.V != 2 || p.Skel.Txt == nil || p.Skel.Txt.N != 70000 || len(p.Text) > store.TextRowCap+64 {
		t.Fatalf("long answer not capped: %+v", p.Skel)
	}
	// a human reader still gets the whole input within the excerpt tier
	got, _ := s.e.st.ListRunEvents("r")
	ev, _ := decodeEvent(got[4].EventData)
	if string(ev.ToolInput) != string(in) || got[4].Truncated {
		t.Fatalf("input within 8 KiB must read back whole")
	}
}

// TestSkeletonIdempotentAndMixed: converting a converted run changes nothing, and
// finishing a half-converted run (a crash between two writes, here forced by
// rewriting some rows by hand) gives exactly the rows a clean conversion gives.
func TestSkeletonIdempotentAndMixed(t *testing.T) {
	s := newSkelEnv(t)
	stream := []Event{
		{Type: taskrunner.EventTypeThinking, Text: "t"},
		{Type: taskrunner.EventTypeText, Text: "a"}, {Type: taskrunner.EventTypeText, Text: "b"},
		toolWith("c1", nil, payload(2000)), toolWith("c2", payload(3000), payload(20000)),
		{Type: taskrunner.EventTypeText, Text: "c"}, {Type: taskrunner.EventTypeDone, StopReason: "end_turn"},
	}
	s.seed(t, "clean", stream)
	s.seed(t, "mixed", stream)
	s.finalize(t, "clean")
	cleanRows := s.raw(t, "clean")
	rowsOf := func(rs []store.RunEvent) (out []string) {
		for _, r := range rs {
			out = append(out, fmt.Sprintf("%d:%s", r.Seq, r.EventData))
		}
		return
	}

	// idempotent: the plan over converted rows is empty.
	if plan, err := skeletonRows(cleanRows); err != nil || len(plan) != 0 {
		t.Fatalf("plan over a converted run = %d rows, %v; want none", len(plan), err)
	}

	// mixed: convert only the first tool row by hand (row + excerpt), leave the rest v1.
	mixedRaw := s.raw(t, "mixed")
	plan, err := skeletonRows(mixedRaw)
	if err != nil {
		t.Fatal(err)
	}
	var first store.SkeletonRow
	for _, r := range plan {
		if len(r.Excerpts) > 0 {
			first = r
			break
		}
	}
	if _, err := s.e.st.ApplySkeleton("mixed", []store.SkeletonRow{first}, time.Now()); err != nil {
		t.Fatal(err)
	}
	// ApplySkeleton marked the run: undo the mark, as a crash before the mark would have left it.
	if _, err := s.e.st.ResetSkeletonForTest("mixed"); err != nil {
		t.Fatal(err)
	}
	if mid := s.raw(t, "mixed"); !strings.Contains(strings.Join(rowsOf(mid), "\n"), `"v":2`) {
		t.Fatal("setup: the mixed run has no v2 row")
	}
	s.finalize(t, "mixed")
	if got, want := rowsOf(s.raw(t, "mixed")), rowsOf(cleanRows); !reflect.DeepEqual(got, want) {
		t.Fatalf("finishing a half-converted run differs from a clean conversion\n got %v\nwant %v", got, want)
	}
	// and a mixed run reads correctly before it is finished (every row decodes).
	if _, err := s.e.st.ListRunEvents("mixed"); err != nil {
		t.Fatal(err)
	}
}

func TestFinalizeLeavesRunRowAndUsageByteIdentical(t *testing.T) {
	s := newSkelEnv(t)
	s.seed(t, "r", []Event{{Type: taskrunner.EventTypeText, Text: "x"}, toolWith("c", nil, payload(5000)), {Type: taskrunner.EventTypeDone, StopReason: "end_turn"}})
	cost := 1.5
	if err := s.e.st.UpsertRunUsage(store.RunUsage{RunID: "r", TaskID: s.e.task.ID, Provider: "glm", CostUSD: &cost, Source: "test"}); err != nil {
		t.Fatal(err)
	}
	before, _ := s.e.st.GetRun("r")
	usage, _ := s.e.st.GetRunUsage("r")
	s.finalize(t, "r")
	after, _ := s.e.st.GetRun("r")
	usage2, _ := s.e.st.GetRunUsage("r")
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(usage, usage2) {
		t.Fatalf("the run row or its usage changed:\n%+v\n%+v", before, after)
	}
}

// TestFinishConvertsAndRunningRunIsNeverTouched: a real run is full while it
// runs and skeleton once finish has run; a stopped run too.
func TestFinishConvertsAndRunningRunIsNeverTouched(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang") // one chunk, then blocks
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)
	runID, err := reg.Start(t.Context(), wm, runner, task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatal(err)
	}
	waitForHistoryLen(t, reg, runID, 1, 5*time.Second)
	if ids, _ := st.SkeletonCandidates(10); len(ids) != 0 {
		t.Fatalf("a running run is a skeleton candidate: %v", ids)
	}
	if sk, _ := st.GetRunSkeleton(runID); sk.SkeletonAt != nil {
		t.Fatal("a running run was converted")
	}
	_ = reg.Stop(runID)
	waitForStatus(t, reg, runID, StatusStopped, 10*time.Second)
	reg.FinalizePending(context.Background()) // finish already converted it: a no-op
	sk, err := st.GetRunSkeleton(runID)
	if err != nil || sk.SkeletonAt == nil || sk.Rev != 1 {
		t.Fatalf("the finished run is not in skeleton form: %+v, %v", sk, err)
	}
}

// TestFinalizePendingBootPass: a run that never reached finish (a crash, or
// running at an upgrade) is converted by the boot pass; a run that finished
// before skeleton storage never is.
func TestFinalizePendingBootPass(t *testing.T) {
	s := newSkelEnv(t)
	stream := []Event{{Type: taskrunner.EventTypeThinking, Text: "t"}, toolWith("c", nil, payload(3000)), {Type: taskrunner.EventTypeDone}}
	s.seed(t, "interrupted", stream)
	s.seed(t, "legacy", stream)
	if _, err := s.e.st.ResetSkeletonForTest("legacy"); err != nil {
		t.Fatal(err)
	}
	if err := s.e.st.MarkLegacyForTest("legacy"); err != nil {
		t.Fatal(err)
	}
	if n := s.reg.FinalizePending(context.Background()); n != 1 {
		t.Fatalf("boot pass looked at %d runs, want 1", n)
	}
	if sk, _ := s.e.st.GetRunSkeleton("interrupted"); sk.SkeletonAt == nil {
		t.Fatal("the interrupted run was not converted")
	}
	if sk, _ := s.e.st.GetRunSkeleton("legacy"); sk.SkeletonAt != nil || strings.Contains(s.raw(t, "legacy")[1].EventData, `"v":2`) {
		t.Fatal("a pre-skeleton run was converted")
	}
	// idempotent: a second pass finds nothing
	if n := s.reg.FinalizePending(context.Background()); n != 0 {
		t.Fatalf("second pass looked at %d runs", n)
	}
}
