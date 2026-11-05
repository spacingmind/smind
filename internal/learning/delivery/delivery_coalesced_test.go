package delivery

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

func TestDelivery_Events_CoalescedEqualsLegacy(t *testing.T) {
	t.Parallel()
	f := newFixture(t, store.LearningModePropose)
	k := f.land("a")
	stream := []map[string]any{
		{"type": int(taskrunner.EventTypeThinking), "text": "a"},
		{"type": int(taskrunner.EventTypeThinking), "text": "b"},
		{"type": int(taskrunner.EventTypeThinking), "text": "c"},
		{"type": int(taskrunner.EventTypeText), "text": "Hello "},
		{"type": int(taskrunner.EventTypeText), "text": "wor"},
		{"type": int(taskrunner.EventTypeText), "text": "ld"},
		{"type": int(taskrunner.EventTypeToolCall), "toolCallId": "load", "toolName": "Skill", "toolInput": map[string]any{"skill": k.Name}},
		{"type": int(taskrunner.EventTypeToolCall), "toolCallId": "big", "toolName": "Bash", "toolInput": map[string]any{"command": "go test ./... " + strings.Repeat("-v ", 500)}, "toolResult": strings.Repeat("FAIL: TestSomething line of failing log\n", 75)},
		{"type": int(taskrunner.EventTypeText), "text": "final "},
		{"type": int(taskrunner.EventTypeText), "text": "answer "},
		{"type": int(taskrunner.EventTypeText), "text": "here"},
		{"type": int(taskrunner.EventTypeDone), "stopReason": "end_turn"},
	}
	type counters struct{ offered, loaded, loads, viaSkill int64 }
	var outcomes []counters
	for _, runID := range []string{"legacy", "coal", "skel"} {
		now := time.Now().UTC()
		if _, err := f.st.CreateRun(store.Run{ID: runID, TaskID: f.task.ID, Provider: "claude-native", Status: "done", StartedAt: now, FinishedAt: &now}); err != nil {
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
			if _, err := f.st.AppendRunEvent(runID, int64(seq), string(data)); err != nil {
				t.Fatal(err)
			}
			seq = next
		}
		if runID == "skel" {
			if err := runs.Skeletonize(f.st, runID); err != nil {
				t.Fatal(err)
			}
			raw, err := f.st.ListRunEventsRaw(runID)
			if err != nil {
				t.Fatal(err)
			}
			moved := false
			for _, e := range raw {
				moved = moved || strings.Contains(e.EventData, `"v":2`)
			}
			if sk, err := f.st.GetRunSkeleton(runID); err != nil || sk.SkeletonAt == nil || !moved {
				t.Fatalf("skel run not converted: err=%v skeleton=%+v moved=%v", err, sk, moved)
			}
		}
		before := f.skill(k.ID)
		f.finish(f.finished(runID, "claude-native", []int64{k.ID}, true))
		after := f.skill(k.ID)
		outcomes = append(outcomes, counters{int64(after.OfferedRuns - before.OfferedRuns), int64(after.LoadedRuns - before.LoadedRuns), int64(after.Loads - before.Loads), int64(after.LoadsViaSkill - before.LoadsViaSkill)})
	}
	if outcomes[0] != outcomes[1] || outcomes[0] != outcomes[2] || outcomes[0] != (counters{1, 1, 1, 1}) {
		t.Fatalf("legacy/coal/skel outcomes = %+v; want one offer and Skill load each", outcomes)
	}
}
