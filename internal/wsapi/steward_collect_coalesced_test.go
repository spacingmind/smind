package wsapi

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/steward"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// seedCoalescedReaderRuns stores the same stream in both formats. Merged
// rows retain the first chunk's seq; tool and done retain their original seqs.
func seedCoalescedReaderRuns(t *testing.T, db *store.Store, taskID int64, finalChunks [3]string) {
	t.Helper()
	chunk := func(kind taskrunner.EventType, text string) map[string]any {
		return map[string]any{"type": int(kind), "text": text}
	}
	stream := []map[string]any{
		chunk(taskrunner.EventTypeThinking, "a"),
		chunk(taskrunner.EventTypeThinking, "b"),
		chunk(taskrunner.EventTypeThinking, "c"),
		chunk(taskrunner.EventTypeText, "Hello "),
		chunk(taskrunner.EventTypeText, "wor"),
		chunk(taskrunner.EventTypeText, "ld"),
		{
			"type": int(taskrunner.EventTypeToolCall), "toolName": "Read", "toolStatus": "success",
			// Over the 512-byte inline cap, so the finalizer moves both to run_excerpts.
			"toolInput":  strings.Repeat("i", 1500),
			"toolResult": strings.Repeat("r", 3000),
		},
		chunk(taskrunner.EventTypeText, finalChunks[0]),
		chunk(taskrunner.EventTypeText, finalChunks[1]),
		chunk(taskrunner.EventTypeText, finalChunks[2]),
		{"type": int(taskrunner.EventTypeDone), "stopReason": "end_turn"},
	}
	for _, runID := range []string{"legacy", "coal", "skel"} {
		if _, err := db.CreateRun(store.Run{ID: runID, TaskID: taskID, Provider: "glm", Status: "done", StartedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		rows := make(map[int]map[string]any, len(stream))
		for seq, event := range stream {
			rows[seq] = event
		}
		if runID == "coal" {
			rows[0] = chunk(taskrunner.EventTypeThinking, "abc")
			rows[3] = chunk(taskrunner.EventTypeText, "Hello world")
			rows[7] = chunk(taskrunner.EventTypeText, finalChunks[0]+finalChunks[1]+finalChunks[2])
			for _, seq := range []int{1, 2, 4, 5, 8, 9} {
				delete(rows, seq)
			}
		}
		for seq := range stream {
			event, ok := rows[seq]
			if !ok {
				continue
			}
			data, err := json.Marshal(event)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.AppendRunEvent(runID, int64(seq), string(data)); err != nil {
				t.Fatal(err)
			}
		}
	}
	// "skel" holds the legacy rows collapsed in place by the finalizer.
	if err := runs.Skeletonize(db, "skel"); err != nil {
		t.Fatalf("skeletonize: %v", err)
	}
	if sk, err := db.GetRunSkeleton("skel"); err != nil || sk.SkeletonAt == nil {
		t.Fatalf("skel run not converted: %+v, %v", sk, err)
	}
	raw, err := db.ListRunEventsRaw("skel")
	if err != nil {
		t.Fatal(err)
	}
	var v2 int
	for _, row := range raw {
		if strings.Contains(row.EventData, `"v":2`) {
			v2++
		}
	}
	if v2 == 0 {
		t.Fatalf("no v2 rows after skeletonize: %+v", raw)
	}
}

func TestFinalRunText_CoalescedEqualsLegacy(t *testing.T) {
	t.Parallel()
	c, _, task := newStewardEnv(t)
	seedCoalescedReaderRuns(t, c.db, task.ID, [3]string{"final ", "answer ", "here"})
	legacy, err := finalRunText(c.db, "legacy")
	if err != nil {
		t.Fatal(err)
	}
	coal, err := finalRunText(c.db, "coal")
	if err != nil {
		t.Fatal(err)
	}
	skel, err := finalRunText(c.db, "skel")
	if err != nil {
		t.Fatal(err)
	}
	if legacy != "final answer here" || coal != legacy || skel != legacy {
		t.Fatalf("final text: legacy=%q, coalesced=%q, skeleton=%q, want all %q", legacy, coal, skel, "final answer here")
	}
}

func TestParseHandoff_CoalescedEqualsLegacy(t *testing.T) {
	t.Parallel()
	c, _, task := newStewardEnv(t)
	// Split the fence and JSON across rows so neither chunk parses alone.
	seedCoalescedReaderRuns(t, c.db, task.ID, [3]string{
		"done, one commit. ```js",
		"on\n" + `{"status":"done","not_verified":["gofmt"],`,
		`"notes":"kept write ownership"}` + "\n```\n",
	})
	var handoffs []steward.HandoffInfo
	for _, runID := range []string{"legacy", "coal", "skel"} {
		text, err := finalRunText(c.db, runID)
		if err != nil {
			t.Fatal(err)
		}
		if text == "" {
			t.Fatalf("%s: empty final text", runID)
		}
		handoff, ok := parseHandoff(text)
		if !ok {
			t.Fatalf("%s: handoff not parsed from %q", runID, text)
		}
		handoffs = append(handoffs, handoff)
	}
	want := steward.HandoffInfo{Status: "done", NotVerified: []string{"gofmt"}, Notes: "kept write ownership"}
	if !reflect.DeepEqual(handoffs[0], want) || !reflect.DeepEqual(handoffs[1], handoffs[0]) || !reflect.DeepEqual(handoffs[2], handoffs[0]) {
		t.Fatalf("handoffs: legacy=%+v, coalesced=%+v, skeleton=%+v, want %+v", handoffs[0], handoffs[1], handoffs[2], want)
	}
}
