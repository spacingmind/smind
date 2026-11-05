package delivery

import (
	"encoding/json"
	"testing"
	"time"

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
		{"type": int(taskrunner.EventTypeText), "text": "final "},
		{"type": int(taskrunner.EventTypeText), "text": "answer "},
		{"type": int(taskrunner.EventTypeText), "text": "here"},
		{"type": int(taskrunner.EventTypeDone), "stopReason": "end_turn"},
	}
	type counters struct{ offered, loaded, loads, viaSkill int64 }
	var outcomes []counters
	for _, runID := range []string{"legacy", "coal"} {
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
		before := f.skill(k.ID)
		f.finish(f.finished(runID, "claude-native", []int64{k.ID}, true))
		after := f.skill(k.ID)
		outcomes = append(outcomes, counters{int64(after.OfferedRuns - before.OfferedRuns), int64(after.LoadedRuns - before.LoadedRuns), int64(after.Loads - before.Loads), int64(after.LoadsViaSkill - before.LoadsViaSkill)})
	}
	if outcomes[0] != outcomes[1] || outcomes[0] != (counters{1, 1, 1, 1}) {
		t.Fatalf("legacy/coal outcomes = %+v; want one offer and Skill load each", outcomes)
	}
}
