package store_test

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

func TestLearningCollect_CoalescedRunWatermarkAndGaps(t *testing.T) {
	t.Parallel()
	// External package allows using the persisted event constants without an import cycle.
	st, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Error(err)
		}
	})
	ws, err := st.CreateWorkspace(store.Workspace{Path: "/repo", Title: "w", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := st.CreateTask(store.Task{WorkspaceID: ws.ID, Title: "t", Status: "created"})
	if err != nil {
		t.Fatal(err)
	}
	chat, err := st.CreateChat(task.ID, "c")
	if err != nil {
		t.Fatal(err)
	}
	stream := []map[string]any{
		{"type": int(taskrunner.EventTypeThinking), "text": "a"},
		{"type": int(taskrunner.EventTypeThinking), "text": "b"},
		{"type": int(taskrunner.EventTypeThinking), "text": "c"},
		{"type": int(taskrunner.EventTypeText), "text": "Hello "},
		{"type": int(taskrunner.EventTypeText), "text": "wor"},
		{"type": int(taskrunner.EventTypeText), "text": "ld"},
		{"type": int(taskrunner.EventTypeToolCall), "toolCallId": "load", "toolName": "Skill", "toolInput": map[string]any{"skill": "learned-a"}},
		{"type": int(taskrunner.EventTypeText), "text": "final "},
		{"type": int(taskrunner.EventTypeText), "text": "answer "},
		{"type": int(taskrunner.EventTypeText), "text": "here"},
		{"type": int(taskrunner.EventTypeDone), "stopReason": "end_turn"},
	}

	var expected []store.RunEvent
	for i, runID := range []string{"legacy", "coal"} {
		start := time.Now().UTC().Add(time.Duration(i) * time.Minute)
		end := start.Add(time.Second)
		if _, err := st.CreateRun(store.Run{ID: runID, TaskID: task.ID, ChatID: chat.ID, Provider: "claude-native", Status: "done", Purpose: "user", StartedAt: start, FinishedAt: &end}); err != nil {
			t.Fatal(err)
		}
		for seq := 0; seq < len(stream); {
			row := make(map[string]any)
			for key, value := range stream[seq] {
				row[key] = value
			}
			next := seq + 1
			if runID == "coal" && (row["type"] == int(taskrunner.EventTypeText) || row["type"] == int(taskrunner.EventTypeThinking)) {
				for next < len(stream) && stream[next]["type"] == row["type"] {
					row["text"] = row["text"].(string) + stream[next]["text"].(string)
					next++
				}
			}
			data, err := json.Marshal(row)
			if err != nil {
				t.Fatal(err)
			}
			event, err := st.AppendRunEvent(runID, int64(seq), string(data))
			if err != nil {
				t.Fatal(err)
			}
			expected = append(expected, event)
			seq = next
		}

	}
	all, err := st.ListChatRunsWithEventsAfter(chat.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].Run.ID != "legacy" || all[1].Run.ID != "coal" || !all[0].PromptInWindow || !all[1].PromptInWindow {
		t.Fatalf("full window = %+v", all)
	}
	var normalized [][]taskrunner.Event
	for i, window := range all {
		var seqs []int64
		var events []taskrunner.Event
		var previousID int64
		for _, row := range window.Events {
			if row.ID <= previousID {
				t.Fatalf("ids not strictly increasing: %d after %d", row.ID, previousID)
			}
			previousID = row.ID
			seqs = append(seqs, row.Seq)
			ev, err := runs.DecodeStoredEvent(row.EventData)
			if err != nil {
				t.Fatal(err)
			}
			n := len(events)
			if n > 0 && events[n-1].Type == ev.Type && (ev.Type == taskrunner.EventTypeText || ev.Type == taskrunner.EventTypeThinking) {
				events[n-1].Text += ev.Text
			} else {
				events = append(events, ev)
			}
		}
		wantSeqs := []int64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
		if i == 1 {
			wantSeqs = []int64{0, 3, 6, 7, 10}
		}
		if !reflect.DeepEqual(seqs, wantSeqs) {
			t.Fatalf("%s seqs = %v, want %v", window.Run.ID, seqs, wantSeqs)
		}
		normalized = append(normalized, events)
	}
	if !reflect.DeepEqual(normalized[0], normalized[1]) || len(normalized[0]) != 5 || taskrunner.FinalText(normalized[0]) != "final answer here" {
		t.Fatalf("normalized legacy/coal events = %+v", normalized)
	}
	// Check every possible row boundary, including inside the coalesced run.
	// Comparing the exact suffix proves no row is duplicated or skipped.
	for boundary := 0; boundary <= len(expected); boundary++ {
		var watermark int64
		if boundary > 0 {
			watermark = expected[boundary-1].ID
		}
		windows, err := st.ListChatRunsWithEventsAfter(chat.ID, watermark)
		if err != nil {
			t.Fatal(err)
		}
		var got []store.RunEvent
		for _, window := range windows {
			got = append(got, window.Events...)
		}
		if len(got) != len(expected)-boundary {
			t.Fatalf("after id %d: got %d rows, want %d", watermark, len(got), len(expected)-boundary)
		}
		previousID := watermark
		for i, row := range got {
			want := expected[boundary+i]
			if row.ID <= previousID || row.ID != want.ID || row.RunID != want.RunID || row.Seq != want.Seq || row.EventData != want.EventData {
				t.Fatalf("after id %d: row %d = %+v, want %+v", watermark, i, row, want)
			}
			previousID = row.ID
		}
	}
	pastLast := expected[len(expected)-1].ID + 1
	if err := st.SetChatLearnState(chat.ID, pastLast, 0); err != nil {
		t.Fatal(err)
	}
	saved, err := st.GetChat(chat.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.LearnWatermark != pastLast {
		t.Fatalf("saved watermark = %d, want %d", saved.LearnWatermark, pastLast)
	}
	remaining, err := st.ListChatRunsWithEventsAfter(chat.ID, saved.LearnWatermark)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("past last id: runs = %+v, error = %v", remaining, err)
	}
}

func TestLearningCollect_SkeletonRunWatermarkAndGaps(t *testing.T) {
	t.Parallel()
	st, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Error(err)
		}
	})
	ws, err := st.CreateWorkspace(store.Workspace{Path: "/repo", Title: "w", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := st.CreateTask(store.Task{WorkspaceID: ws.ID, Title: "t", Status: "created"})
	if err != nil {
		t.Fatal(err)
	}
	chat, err := st.CreateChat(task.ID, "c")
	if err != nil {
		t.Fatal(err)
	}
	bigResult := strings.Repeat("--- FAIL: TestSomething (0.00s) file_test.go:12: boom\n", 60)
	stream := []map[string]any{
		{"type": int(taskrunner.EventTypeThinking), "text": "a"},
		{"type": int(taskrunner.EventTypeThinking), "text": "b"},
		{"type": int(taskrunner.EventTypeThinking), "text": "c"},
		{"type": int(taskrunner.EventTypeText), "text": "Hello "},
		{"type": int(taskrunner.EventTypeText), "text": "wor"},
		{"type": int(taskrunner.EventTypeText), "text": "ld"},
		{"type": int(taskrunner.EventTypeToolCall), "toolCallId": "big", "toolName": "Bash", "toolInput": map[string]any{"command": "go test " + strings.Repeat("-race ", 250)}, "toolResult": bigResult},
		{"type": int(taskrunner.EventTypeText), "text": "final "},
		{"type": int(taskrunner.EventTypeText), "text": "answer "},
		{"type": int(taskrunner.EventTypeText), "text": "here"},
		{"type": int(taskrunner.EventTypeDone), "stopReason": "end_turn"},
	}
	start := time.Now().UTC()
	end := start.Add(time.Second)
	if _, err := st.CreateRun(store.Run{ID: "skel", TaskID: task.ID, ChatID: chat.ID, Provider: "claude-native", Status: "done", Purpose: "user", StartedAt: start, FinishedAt: &end}); err != nil {
		t.Fatal(err)
	}
	var original []store.RunEvent
	for seq, row := range stream {
		data, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		event, err := st.AppendRunEvent("skel", int64(seq), string(data))
		if err != nil {
			t.Fatal(err)
		}
		original = append(original, event)
	}
	// What the collector consumed before conversion: every possible boundary.
	before := make([][]int64, len(original)+1)
	for boundary := range before {
		before[boundary] = collectIDsAfter(t, st, chat.ID, watermarkAt(original, boundary))
	}

	if err := runs.Skeletonize(st, "skel"); err != nil {
		t.Fatal(err)
	}
	raw, err := st.ListRunEventsRaw("skel")
	if err != nil {
		t.Fatal(err)
	}
	moved := false
	surviving := make(map[int64]bool)
	for _, e := range raw {
		moved = moved || strings.Contains(e.EventData, `"v":2`)
		surviving[e.ID] = true
	}
	if sk, err := st.GetRunSkeleton("skel"); err != nil || sk.SkeletonAt == nil || !moved {
		t.Fatalf("run not converted: err=%v skeleton=%+v moved=%v", err, sk, moved)
	}
	if len(raw) >= len(original) {
		t.Fatalf("no rows merged away: %d raw rows of %d", len(raw), len(original))
	}

	for boundary := 0; boundary <= len(original); boundary++ {
		watermark := watermarkAt(original, boundary)
		var want []int64
		for _, e := range original {
			if e.ID > watermark && surviving[e.ID] {
				want = append(want, e.ID)
			}
		}
		got := collectIDsAfter(t, st, chat.ID, watermark)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("after id %d: ids = %v, want %v", watermark, got, want)
		}
		// Rows seen before conversion and rows seen after it never overlap and
		// leave no hole: the surviving rows past the watermark were all in the
		// pre-conversion tail.
		inBefore := make(map[int64]bool)
		for _, id := range before[boundary] {
			inBefore[id] = true
		}
		for _, id := range got {
			if !inBefore[id] {
				t.Fatalf("after id %d: row %d appeared only after conversion", watermark, id)
			}
		}
	}

	windows, err := st.ListChatRunsWithEventsAfter(chat.ID, 0)
	if err != nil || len(windows) != 1 {
		t.Fatalf("full window = %+v, error = %v", windows, err)
	}
	var events []taskrunner.Event
	for _, row := range windows[0].Events {
		ev, err := runs.DecodeStoredEvent(row.EventData)
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, ev)
	}
	var tool *taskrunner.Event
	for i := range events {
		if events[i].Type == taskrunner.EventTypeToolCall {
			tool = &events[i]
		}
	}
	if tool == nil || string(tool.ToolResult) != mustJSON(t, bigResult) {
		t.Fatalf("tool result not rehydrated whole: %+v", tool)
	}
	if got := taskrunner.FinalText(events); got != "final answer here" {
		t.Fatalf("final text = %q", got)
	}
}

func watermarkAt(rows []store.RunEvent, boundary int) int64 {
	if boundary == 0 {
		return 0
	}
	return rows[boundary-1].ID
}

func collectIDsAfter(t *testing.T, st *store.Store, chatID int64, watermark int64) []int64 {
	t.Helper()
	windows, err := st.ListChatRunsWithEventsAfter(chatID, watermark)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, w := range windows {
		for _, row := range w.Events {
			if row.ID <= watermark {
				t.Fatalf("row %d not after watermark %d", row.ID, watermark)
			}
			ids = append(ids, row.ID)
		}
	}
	return ids
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
